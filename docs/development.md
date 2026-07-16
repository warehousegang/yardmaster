# Yardmaster Development

This guide covers the normal workflow for changing and testing Yardmaster.

## Prerequisites

- Go 1.24 or newer
- Docker
- `kubectl`
- `kind` for local cluster development
- access to a Kubernetes cluster through kubeconfig

`kubectl apply -k` provides the Kustomize behavior used by this repository.

## First Setup

```bash
go mod download
make test
make build
```

`make build` creates:

```text
bin/yardmaster
bin/kubectl-yardmaster
bin/yardmaster-dashboard
```

The `bin/` directory is ignored by Git because these files are build outputs.

## Repository Layout

```text
api/                  Kubernetes API definitions
cmd/                  Executable entrypoints
config/               Kubernetes manifests and Kustomize files
docs/                 Developer and operator guides
internal/analyzer/    Deterministic analysis logic
internal/controller/  Reconciliation and Kubernetes API behavior
internal/dashboard/   Dashboard server, finding views, page, and assets
internal/presentation/Shared formatting
```

## Common Commands

| Command | Purpose |
| --- | --- |
| `make fmt` | Format Go files. |
| `make test` | Run all Go tests. |
| `make build` | Build all three executables. |
| `make generate` | Regenerate DeepCopy methods. |
| `make manifests` | Regenerate the CRD manifest. |
| `make docker-build` | Build the container image. |
| `make run` | Run the operator against the current kubeconfig context. |
| `make report` | Print findings through the CLI. |
| `make dashboard` | Run the dashboard locally. |
| `make demo-kind` | Build and deploy a complete local demo. |
| `make smoke-kind` | Run a local controller smoke test. |

## Local Kind Workflow

Create or select the protected Yardmaster development cluster:

```bash
make kind-context
```

Install the CRD and RBAC:

```bash
make install
```

Run the operator locally:

```bash
make run
```

In another terminal, create sample problems and inspect findings:

```bash
make sample
make report
```

Run the dashboard:

```bash
make dashboard
```

Then open `http://localhost:8088`.

The `sample`, `smoke-kind`, and `demo-kind` targets verify that the current
context is `kind-yardmaster` before creating sample Pods or labeling nodes.

## Full Local Demo

```bash
make demo-kind
```

This target:

1. creates or selects the `yardmaster` kind cluster
2. builds the container image
3. loads the image into kind
4. installs and deploys Yardmaster
5. waits for operator and dashboard rollouts
6. creates sample workloads
7. prints the CLI report

## Testing Strategy

The current test suite covers:

- pending Pod explanations
- request coverage detection
- Track grouping and accounting
- workload owner resolution
- reconciler create, update, resolution, source deletion, and stale cleanup
- Karpenter NodePool and NodeClaim reading
- dashboard sorting, counts, HTTP handlers, and Kubernetes read failures
- presentation formatting

Run one package:

```bash
go test ./internal/analyzer
```

Run one test:

```bash
go test ./internal/analyzer -run TestAnalyzePodExplainsMissingNodeSelector
```

Run everything:

```bash
go test ./...
```

The reconciler tests use controller-runtime's fake client. `envtest` integration
tests with a real API server and full end-to-end tests in kind are still future
coverage areas.

## Changing The API

When editing `api/v1alpha1`:

1. Change the Go structs and Kubebuilder markers.
2. Run `make generate`.
3. Run `make manifests`.
4. Inspect the generated CRD diff.
5. Run `go test ./...`.
6. Render the install configuration with `kubectl kustomize config/default`.

Generated files should be committed, but not manually edited.

## Adding Or Changing Analysis

For a rule that can be decided from supplied objects:

1. Implement the decision in `internal/analyzer`.
2. Return a draft `DispatchFindingSpec`.
3. Add table-driven unit tests for positive, negative, and edge cases.
4. Keep API calls out of the analyzer.
5. Let the controller own object retrieval and persistence.

## Adding A Controller

1. Create the reconciler in `internal/controller`.
2. Define its primary resource and secondary watches.
3. Make finding names deterministic.
4. Handle creation, updates, resolution, and source deletion.
5. Register it in `cmd/yardmaster/main.go`.
6. Add only the required RBAC permissions.
7. Add analyzer, controller, and end-to-end coverage appropriate to the risk.
8. Update [Controllers](controllers.md).

## Before Committing

```bash
make fmt
make generate
make manifests
go test ./...
go build ./cmd/...
kubectl kustomize config/default >/dev/null
git diff --check
```

Also inspect `git diff` for accidental generated-file drift, binary files, local
IDE files, credentials, and unrelated changes.
