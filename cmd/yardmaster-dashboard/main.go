package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	yardcontroller "github.com/warehousegang/yardmaster/internal/controller"
	"github.com/warehousegang/yardmaster/internal/dashboard"
)

func main() {
	var addr string
	var kubeconfig string
	var findingNamespace string

	flags := flag.NewFlagSet("yardmaster-dashboard", flag.ExitOnError)
	flags.StringVar(&addr, "addr", ":8088", "Address where the dashboard listens.")
	flags.StringVar(&kubeconfig, "kubeconfig", defaultKubeconfig(), "Path to the kubeconfig file.")
	flags.StringVar(&findingNamespace, "finding-namespace", yardcontroller.DefaultFindingNamespace, "Namespace where DispatchFinding resources are stored.")
	_ = flags.Parse(os.Args[1:])

	server, err := dashboard.New(kubeconfig, findingNamespace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Yardmaster dashboard listening on http://localhost%s\n", addr)
	if err := dashboard.ListenAndServe(ctx, addr, server); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
	path := filepath.Join(home, ".kube", "config")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
