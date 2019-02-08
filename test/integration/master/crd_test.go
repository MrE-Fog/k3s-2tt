/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package master

import (
	"encoding/json"
	"testing"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/test/integration/etcd"
	"k8s.io/kubernetes/test/integration/framework"
)

func TestCRDShadowGroup(t *testing.T) {
	result := kubeapiservertesting.StartTestServerOrDie(t, nil, nil, framework.SharedEtcd())
	defer result.TearDownFn()

	kubeclient, err := kubernetes.NewForConfig(result.ClientConfig)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	apiextensionsclient, err := apiextensionsclientset.NewForConfig(result.ClientConfig)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	t.Logf("Creating a NetworkPolicy")
	nwPolicy, err := kubeclient.NetworkingV1().NetworkPolicies("default").Create(&networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "abc", Namespace: metav1.NamespaceDefault},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
			Ingress:     []networkingv1.NetworkPolicyIngressRule{},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create NetworkPolicy: %v", err)
	}

	t.Logf("Trying to shadow networking group")
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foos." + networkingv1.GroupName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   networkingv1.GroupName,
			Version: networkingv1.SchemeGroupVersion.Version,
			Scope:   apiextensionsv1beta1.ClusterScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: "foos",
				Kind:   "Foo",
			},
		},
	}
	etcd.CreateTestCRDs(t, apiextensionsclient, true, crd)

	// wait to give aggregator time to update
	time.Sleep(2 * time.Second)

	t.Logf("Checking that we still see the NetworkPolicy")
	_, err = kubeclient.NetworkingV1().NetworkPolicies(nwPolicy.Namespace).Get(nwPolicy.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get NetworkPolocy: %v", err)
	}

	t.Logf("Checking that crd resource does not show up in networking group")
	if etcd.CrdExistsInDiscovery(apiextensionsclient, crd) {
		t.Errorf("CRD resource shows up in discovery, but shouldn't.")
	}
}

func TestCRD(t *testing.T) {
	result := kubeapiservertesting.StartTestServerOrDie(t, nil, nil, framework.SharedEtcd())
	defer result.TearDownFn()

	apiextensionsclient, err := apiextensionsclientset.NewForConfig(result.ClientConfig)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	t.Logf("Trying to create a custom resource without conflict")
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foos.cr.bar.com",
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   "cr.bar.com",
			Version: "v1",
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: "foos",
				Kind:   "Foo",
			},
		},
	}
	etcd.CreateTestCRDs(t, apiextensionsclient, false, crd)

	t.Logf("Trying to access foos.cr.bar.com with dynamic client")
	dynamicClient, err := dynamic.NewForConfig(result.ClientConfig)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	fooResource := schema.GroupVersionResource{Group: "cr.bar.com", Version: "v1", Resource: "foos"}
	_, err = dynamicClient.Resource(fooResource).Namespace("default").List(metav1.ListOptions{})
	if err != nil {
		t.Errorf("Failed to list foos.cr.bar.com instances: %v", err)
	}
}

type Foo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
}

func unstructuredFoo(foo *Foo) (*unstructured.Unstructured, error) {
	bs, err := json.Marshal(foo)
	if err != nil {
		return nil, err
	}
	ret := &unstructured.Unstructured{}
	if err = ret.UnmarshalJSON(bs); err != nil {
		return nil, err
	}
	return ret, nil
}