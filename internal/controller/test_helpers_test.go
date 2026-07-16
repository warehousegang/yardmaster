package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

func testScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	utilruntime.Must(yardv1alpha1.AddToScheme(scheme))

	for _, version := range karpenterVersions {
		groupVersion := schema.GroupVersion{Group: "karpenter.sh", Version: version}
		scheme.AddKnownTypeWithName(groupVersion.WithKind("NodePool"), &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(groupVersion.WithKind("NodePoolList"), &unstructured.UnstructuredList{})
		scheme.AddKnownTypeWithName(groupVersion.WithKind("NodeClaim"), &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(groupVersion.WithKind("NodeClaimList"), &unstructured.UnstructuredList{})
		metav1.AddToGroupVersion(scheme, groupVersion)
	}

	return scheme
}
