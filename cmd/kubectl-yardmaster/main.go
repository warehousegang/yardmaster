package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
	yardcontroller "github.com/warehousegang/yardmaster/internal/controller"
	yardpresentation "github.com/warehousegang/yardmaster/internal/presentation"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var kubeconfig string
	var findingNamespace string

	cmd := &cobra.Command{
		Use:          "kubectl-yardmaster",
		Short:        "Inspect Yardmaster capacity findings",
		SilenceUsage: true,
	}

	cmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", defaultKubeconfig(), "Path to the kubeconfig file.")
	cmd.PersistentFlags().StringVar(&findingNamespace, "finding-namespace", yardcontroller.DefaultFindingNamespace, "Namespace where DispatchFinding resources are stored.")

	cmd.AddCommand(&cobra.Command{
		Use:   "report",
		Short: "Print a readable capacity findings report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReport(cmd.Context(), kubeconfig, findingNamespace)
		},
	})

	return cmd
}

func runReport(ctx context.Context, kubeconfig, namespace string) error {
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return err
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(yardv1alpha1.AddToScheme(scheme))

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}

	var findings yardv1alpha1.DispatchFindingList
	if err := k8sClient.List(ctx, &findings, client.InNamespace(namespace)); err != nil {
		return err
	}

	printReport(namespace, findings.Items)
	return nil
}

func printReport(namespace string, findings []yardv1alpha1.DispatchFinding) {
	fmt.Printf("Yardmaster report from namespace %s\n\n", namespace)
	if len(findings) == 0 {
		fmt.Println("No active findings.")
		return
	}

	sort.Slice(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]
		if left.Spec.Category != right.Spec.Category {
			return categoryRank(left.Spec.Category) < categoryRank(right.Spec.Category)
		}
		if left.Spec.Severity != right.Spec.Severity {
			return severityRank(left.Spec.Severity) > severityRank(right.Spec.Severity)
		}
		return subjectLabel(left) < subjectLabel(right)
	})

	currentCategory := ""
	for _, finding := range findings {
		if finding.Spec.Category != currentCategory {
			currentCategory = finding.Spec.Category
			fmt.Println(categoryTitle(currentCategory))
		}

		fmt.Printf("  %-8s %s\n", finding.Spec.Severity, subjectLabel(finding))
		fmt.Printf("           %s\n", finding.Spec.Summary)
		if finding.Spec.Detail != "" {
			fmt.Printf("           Reason: %s\n", finding.Spec.Detail)
		}
		for _, related := range finding.Spec.Related {
			fmt.Printf("           Related: %s\n", yardpresentation.SubjectLabel(related))
		}
		for _, recommendation := range finding.Spec.Recommendations {
			fmt.Printf("           Recommendation: %s\n", recommendation)
		}
		fmt.Println()
	}
}

func subjectLabel(finding yardv1alpha1.DispatchFinding) string {
	return yardpresentation.SubjectLabel(finding.Spec.Subject)
}

func categoryTitle(category string) string {
	switch category {
	case "scheduling":
		return "Scheduling"
	case "requests":
		return "Requests"
	case "tracks":
		return "Node Pools"
	case "":
		return "General"
	default:
		return strings.ToUpper(category[:1]) + category[1:]
	}
}

func categoryRank(category string) int {
	switch category {
	case "scheduling":
		return 10
	case "requests":
		return 20
	case "tracks":
		return 30
	default:
		return 100
	}
}

func severityRank(severity yardv1alpha1.FindingSeverity) int {
	switch severity {
	case yardv1alpha1.FindingSeverityCritical:
		return 3
	case yardv1alpha1.FindingSeverityWarning:
		return 2
	default:
		return 1
	}
}

func defaultKubeconfig() string {
	if value := os.Getenv("KUBECONFIG"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
}
