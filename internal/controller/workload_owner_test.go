package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

func TestWorkloadSubjectForPodResolvesDeploymentThroughReplicaSet(t *testing.T) {
	scheme := testScheme()
	controller := true
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-6f775cf4f6-x87rv",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       "api-6f775cf4f6",
				Controller: &controller,
			}},
		},
	}
	replicaSet := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-6f775cf4f6",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "api",
				Controller: &controller,
			}},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&replicaSet).Build()

	subject := workloadSubjectForPod(context.Background(), k8sClient, &pod)

	assertSubject(t, subject, yardv1alpha1.DispatchFindingSubject{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Namespace:  "default",
		Name:       "api",
	})
}

func TestWorkloadSubjectForPodResolvesCronJobThroughJob(t *testing.T) {
	scheme := testScheme()
	controller := true
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nightly-29172051-h82pn",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1",
				Kind:       "Job",
				Name:       "nightly-29172051",
				Controller: &controller,
			}},
		},
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nightly-29172051",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1",
				Kind:       "CronJob",
				Name:       "nightly",
				Controller: &controller,
			}},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&job).Build()

	subject := workloadSubjectForPod(context.Background(), k8sClient, &pod)

	assertSubject(t, subject, yardv1alpha1.DispatchFindingSubject{
		APIVersion: "batch/v1",
		Kind:       "CronJob",
		Namespace:  "default",
		Name:       "nightly",
	})
}

func TestWorkloadSubjectForPodUsesDirectWorkloadOwner(t *testing.T) {
	scheme := testScheme()
	controller := true
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "database-0",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "StatefulSet",
				Name:       "database",
				Controller: &controller,
			}},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	subject := workloadSubjectForPod(context.Background(), k8sClient, &pod)

	assertSubject(t, subject, yardv1alpha1.DispatchFindingSubject{
		APIVersion: "apps/v1",
		Kind:       "StatefulSet",
		Namespace:  "default",
		Name:       "database",
	})
}

func testScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	return scheme
}

func assertSubject(t *testing.T, got, want yardv1alpha1.DispatchFindingSubject) {
	t.Helper()
	if got != want {
		t.Fatalf("expected subject %#v, got %#v", want, got)
	}
}
