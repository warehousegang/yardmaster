package model

import corev1 "k8s.io/api/core/v1"

type ClusterSnapshot struct {
	Pods   []corev1.Pod
	Nodes  []corev1.Node
	Events []corev1.Event
}
