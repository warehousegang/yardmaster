# Yardmaster Architecture

Yardmaster is a read-mostly Kubernetes operator that converts cluster scheduling
and capacity state into `DispatchFinding` custom resources. The findings are then
consumed by a CLI and dashboard.

## System Flow

```text
Kubernetes API
    |
    | Pods, Nodes, Events, workload owners,
    | Karpenter NodePools and NodeClaims
    v
Yardmaster controller manager
    |
    +-- PendingPodReconciler
    +-- RequestCoverageReconciler
    +-- TrackSummaryReconciler
    |
    v
DispatchFinding resources in yardmaster-system
    |
    +-- kubectl-yardmaster report
    +-- Yardmaster dashboard
```

The operator reads cluster state and writes only Yardmaster-owned
`DispatchFinding` resources. It does not mutate workloads or provision nodes.

## Executables

The repository builds three programs from `cmd/`:

| Program | Source | Responsibility |
| --- | --- | --- |
| `yardmaster` | `cmd/yardmaster` | Starts the controller-runtime manager and registers controllers. |
| `kubectl-yardmaster` | `cmd/kubectl-yardmaster` | Reads findings and prints a terminal report. |
| `yardmaster-dashboard` | `cmd/yardmaster-dashboard` | Reads findings and serves the web dashboard. |

The `yardmaster` binary is the operator. The CLI and dashboard are separate
consumers of the API objects it produces.

## Package Boundaries

```text
api/v1alpha1/          DispatchFinding Kubernetes API definitions
cmd/                   Program entrypoints and assembly
internal/analyzer/     Pure analysis and finding construction
internal/controller/   Watches, reconciliation, Kubernetes reads and writes
internal/dashboard/    Dashboard HTTP server and presentation
internal/model/        Shared internal data structures
internal/presentation/ Shared human-readable formatting
config/                CRD, RBAC, Deployment, Service, and sample manifests
docs/                  Developer and operator documentation
```

The intended dependency direction is:

```text
cmd
 |
 +--> internal/controller --> internal/analyzer --> api
 +--> internal/dashboard -----------------------> api
 +--> internal/presentation --------------------> api
```

The API package defines data contracts. It should not contain CLI, dashboard, or
other presentation behavior.

## Controller And Analyzer Split

Controllers own Kubernetes behavior:

- receive reconcile requests
- read objects through the controller-runtime client
- call analysis code
- create, update, or delete findings
- update status
- arrange watches and requeues

Analyzers own decision logic:

- accept Kubernetes objects as input
- determine whether a problem or summary exists
- produce a draft `DispatchFindingSpec`
- avoid direct Kubernetes API calls

This split keeps most decision logic easy to unit test without a live cluster.

## API Ownership

Yardmaster currently owns one custom resource:

```text
Group:    yardmaster.dev
Version:  v1alpha1
Kind:     DispatchFinding
Scope:    Namespaced
```

The Go types under `api/v1alpha1` are the source of truth. `controller-gen`
creates:

- `api/v1alpha1/zz_generated.deepcopy.go`
- `config/crd/yardmaster.dev_dispatchfindings.yaml`

See [DispatchFinding API](dispatchfinding.md) for the schema and generation
workflow.

## Kubernetes Identities

The deployed components use separate ServiceAccounts:

| ServiceAccount | Scope |
| --- | --- |
| `yardmaster` | Reads cluster resources and manages `DispatchFinding` objects. |
| `yardmaster-dashboard` | Lists `DispatchFinding` objects only in `yardmaster-system`. |

The CLI normally uses the permissions from the developer's kubeconfig rather
than an in-cluster ServiceAccount.

## Optional Karpenter Integration

The Track controller attempts to read `karpenter.sh` `NodePool` and `NodeClaim`
objects using unstructured clients. It supports API versions `v1` and `v1beta1`.
If the Karpenter CRDs are not installed, Yardmaster skips that part of the
analysis and continues operating.

## Runtime Configuration

The operator supports:

- metrics bind address
- health probe bind address
- finding namespace
- namespaces ignored by request coverage
- leader election

The deployed default uses one operator replica and does not enable leader
election. See [Operations](operations.md) before changing replica count.
