package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

const (
	findingCategoryLabel = "yardmaster.dev/category"
	findingSubjectLabel  = "yardmaster.dev/subject"
)

func upsertDispatchFinding(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
	category string,
	spec yardv1alpha1.DispatchFindingSpec,
) error {
	key := types.NamespacedName{Namespace: namespace, Name: name}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		now := metav1.Now()
		var current yardv1alpha1.DispatchFinding
		err := k8sClient.Get(ctx, key, &current)
		if apierrors.IsNotFound(err) {
			finding := yardv1alpha1.DispatchFinding{
				TypeMeta: metav1.TypeMeta{
					APIVersion: yardv1alpha1.GroupVersion.String(),
					Kind:       "DispatchFinding",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Labels:    mergeFindingLabels(nil, category, spec.Subject.Kind),
				},
				Spec: spec,
			}
			if err := k8sClient.Create(ctx, &finding); err != nil {
				return err
			}
			finding.Status.FirstSeen = now
			finding.Status.LastSeen = now
			return k8sClient.Status().Update(ctx, &finding)
		}
		if err != nil {
			return err
		}

		current.Spec = spec
		current.Labels = mergeFindingLabels(current.Labels, category, spec.Subject.Kind)
		if err := k8sClient.Update(ctx, &current); err != nil {
			return err
		}
		current.Status.LastSeen = now
		if current.Status.FirstSeen.IsZero() {
			current.Status.FirstSeen = now
		}
		return k8sClient.Status().Update(ctx, &current)
	})
}

func deleteDispatchFinding(ctx context.Context, k8sClient client.Client, key types.NamespacedName) error {
	var finding yardv1alpha1.DispatchFinding
	if err := k8sClient.Get(ctx, key, &finding); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return k8sClient.Delete(ctx, &finding)
}

func mergeFindingLabels(labels map[string]string, category, subjectKind string) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[findingCategoryLabel] = category
	labels[findingSubjectLabel] = sanitizeDNSLabel(subjectKind)
	return labels
}
