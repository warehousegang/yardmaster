package controller

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKarpenterTrackFindingsReadsNodePoolAndNodeClaims(t *testing.T) {
	nodePool := karpenterObject("v1", "NodePool", "general")
	nodePool.Object["spec"] = map[string]any{
		"limits": map[string]any{
			"cpu":    "100",
			"memory": "400Gi",
			"nodes":  int64(20),
		},
		"template": map[string]any{
			"spec": map[string]any{
				"nodeClassRef": map[string]any{
					"kind": "EC2NodeClass",
					"name": "general",
				},
				"requirements": []any{
					map[string]any{
						"key":      "kubernetes.io/arch",
						"operator": "In",
						"values":   []any{"arm64"},
					},
				},
				"taints": []any{
					map[string]any{
						"key":    "workload",
						"value":  "general",
						"effect": "NoSchedule",
					},
				},
			},
		},
		"disruption": map[string]any{
			"consolidationPolicy": "WhenEmptyOrUnderutilized",
			"consolidateAfter":    "1m",
		},
	}
	nodePool.Object["status"] = map[string]any{
		"nodes": int64(3),
		"resources": map[string]any{
			"cpu":    "12",
			"memory": "48Gi",
		},
		"conditions": []any{
			map[string]any{"type": "Ready", "status": "True"},
		},
	}
	claimOne := karpenterObject("v1", "NodeClaim", "claim-1")
	claimOne.SetLabels(map[string]string{"karpenter.sh/nodepool": "general"})
	claimTwo := karpenterObject("v1", "NodeClaim", "claim-2")
	claimTwo.SetLabels(map[string]string{"karpenter.sh/nodepool-name": "general"})

	reconciler := &TrackSummaryReconciler{
		Client: newControllerTestClient(nodePool, claimOne, claimTwo),
	}
	findings, err := reconciler.karpenterTrackFindings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %d", len(findings))
	}
	finding := findings[0]
	if finding.TrackName != "karpenter-policy/general" {
		t.Fatalf("unexpected track name %q", finding.TrackName)
	}
	for _, want := range []string{
		"2 NodeClaim(s)",
		"node limit 20",
		"EC2NodeClass/general",
		"kubernetes.io/arch In [arm64]",
		"workload=general:NoSchedule",
		"WhenEmptyOrUnderutilized after 1m",
	} {
		if !strings.Contains(finding.Spec.Summary+" "+finding.Spec.Detail, want) {
			t.Fatalf("expected finding to contain %q, got summary=%q detail=%q", want, finding.Spec.Summary, finding.Spec.Detail)
		}
	}
}

func TestKarpenterTrackFindingsAllowsMissingNodeClaimType(t *testing.T) {
	nodePool := karpenterObject("v1", "NodePool", "general")
	baseClient := newControllerTestClient(nodePool)
	reconciler := &TrackSummaryReconciler{
		Client: noNodeClaimClient{Client: baseClient},
	}

	findings, err := reconciler.karpenterTrackFindings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %d", len(findings))
	}
	if !strings.Contains(findings[0].Spec.Summary, "0 NodeClaim(s)") {
		t.Fatalf("unexpected summary %q", findings[0].Spec.Summary)
	}
}

type noNodeClaimClient struct {
	client.Client
}

func (c noNodeClaimClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if objectList, ok := list.(*unstructured.UnstructuredList); ok && objectList.GetKind() == "NodeClaimList" {
		return &meta.NoKindMatchError{
			GroupKind:        schema.GroupKind{Group: "karpenter.sh", Kind: "NodeClaim"},
			SearchedVersions: []string{"v1", "v1beta1"},
		}
	}
	return c.Client.List(ctx, list, opts...)
}

func karpenterObject(version, kind, name string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "karpenter.sh/" + version,
			"kind":       kind,
			"metadata": map[string]any{
				"name": name,
			},
		},
	}
	object.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "karpenter.sh",
		Version: version,
		Kind:    kind,
	})
	object.SetCreationTimestamp(metav1.Now())
	return object
}
