package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

func workloadSubjectForPod(ctx context.Context, k8sClient client.Client, pod *corev1.Pod) yardv1alpha1.DispatchFindingSubject {
	if pod == nil {
		return yardv1alpha1.DispatchFindingSubject{}
	}

	podSubject := yardv1alpha1.SubjectFromPod(pod)
	owner := controllerOwnerRef(pod.OwnerReferences)
	if owner == nil {
		return podSubject
	}

	switch owner.Kind {
	case "ReplicaSet":
		return deploymentSubjectForReplicaSet(ctx, k8sClient, pod.Namespace, owner.Name, podSubject)
	case "Job":
		return cronJobSubjectForJob(ctx, k8sClient, pod.Namespace, owner.Name, podSubject)
	case "Deployment", "StatefulSet", "DaemonSet", "CronJob":
		return subjectFromOwnerRef(pod.Namespace, *owner)
	default:
		return subjectFromOwnerRef(pod.Namespace, *owner)
	}
}

func deploymentSubjectForReplicaSet(ctx context.Context, k8sClient client.Client, namespace, name string, fallback yardv1alpha1.DispatchFindingSubject) yardv1alpha1.DispatchFindingSubject {
	var replicaSet appsv1.ReplicaSet
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &replicaSet); err != nil {
		if apierrors.IsNotFound(err) {
			return subjectForKnownWorkload("apps/v1", "ReplicaSet", namespace, name)
		}
		return fallback
	}

	owner := controllerOwnerRef(replicaSet.OwnerReferences)
	if owner == nil {
		return subjectForKnownWorkload("apps/v1", "ReplicaSet", namespace, name)
	}
	if owner.Kind == "Deployment" {
		return subjectFromOwnerRef(namespace, *owner)
	}
	return subjectFromOwnerRef(namespace, *owner)
}

func cronJobSubjectForJob(ctx context.Context, k8sClient client.Client, namespace, name string, fallback yardv1alpha1.DispatchFindingSubject) yardv1alpha1.DispatchFindingSubject {
	var job batchv1.Job
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &job); err != nil {
		if apierrors.IsNotFound(err) {
			return subjectForKnownWorkload("batch/v1", "Job", namespace, name)
		}
		return fallback
	}

	owner := controllerOwnerRef(job.OwnerReferences)
	if owner == nil {
		return subjectForKnownWorkload("batch/v1", "Job", namespace, name)
	}
	if owner.Kind == "CronJob" {
		return subjectFromOwnerRef(namespace, *owner)
	}
	return subjectFromOwnerRef(namespace, *owner)
}

func controllerOwnerRef(refs []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			return &refs[i]
		}
	}
	if len(refs) == 0 {
		return nil
	}
	return &refs[0]
}

func subjectFromOwnerRef(namespace string, owner metav1.OwnerReference) yardv1alpha1.DispatchFindingSubject {
	return subjectForKnownWorkload(owner.APIVersion, owner.Kind, namespace, owner.Name)
}

func subjectForKnownWorkload(apiVersion, kind, namespace, name string) yardv1alpha1.DispatchFindingSubject {
	return yardv1alpha1.DispatchFindingSubject{
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
	}
}

func promoteFindingToWorkload(ctx context.Context, k8sClient client.Client, pod *corev1.Pod, draftSpec *yardv1alpha1.DispatchFindingSpec) {
	if pod == nil || draftSpec == nil {
		return
	}

	podSubject := yardv1alpha1.SubjectFromPod(pod)
	workloadSubject := workloadSubjectForPod(ctx, k8sClient, pod)
	draftSpec.Subject = workloadSubject
	if workloadSubject != podSubject {
		draftSpec.Related = appendUniqueSubject(draftSpec.Related, podSubject)
	}
}

func appendUniqueSubject(subjects []yardv1alpha1.DispatchFindingSubject, subject yardv1alpha1.DispatchFindingSubject) []yardv1alpha1.DispatchFindingSubject {
	for _, existing := range subjects {
		if existing == subject {
			return subjects
		}
	}
	return append(subjects, subject)
}
