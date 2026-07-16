# Yardmaster Controllers

Yardmaster uses `controller-runtime`. The controller manager is created in
`cmd/yardmaster/main.go`, where each reconciler is registered with
`SetupWithManager`.

## Reconciliation Model

A controller does not run a one-time script. It repeatedly compares current
cluster state with Yardmaster's expected findings.

For each reconcile request, a Yardmaster controller generally:

1. Reads the triggering Kubernetes object.
2. Reads any supporting cluster state.
3. Calls an analyzer.
4. Creates or updates the expected `DispatchFinding`.
5. Deletes the finding when the condition no longer exists.
6. Returns an error for controller-runtime to retry, or schedules a later
   reconciliation.

The code must be idempotent: running the same reconciliation multiple times
should converge on the same cluster state.

## Current Controllers

| Controller | Watches | Additional reads | Output | Periodic requeue |
| --- | --- | --- | --- | --- |
| Pending Pod | Pods, Nodes, Events | Nodes, namespace Events, workload owners | `scheduling` finding per pending Pod | 5 minutes |
| Request Coverage | Pods | Workload owners | `requests` finding per active Pod with missing requests | 15 minutes |
| Track Summary | Nodes, Pods | All Nodes and Pods, optional Karpenter NodePools and NodeClaims | `tracks` finding per capacity Track | 5 minutes |

## Pending Pod Controller

Source:

- `internal/controller/pending_pod_controller.go`
- `internal/analyzer/pending_pods.go`

The controller primarily watches Pods. Node changes enqueue all currently
pending, unscheduled Pods, and scheduling Events enqueue the involved Pod.

The analyzer currently checks:

- no ready schedulable nodes
- unmatched `nodeSelector` terms
- untolerated `NoSchedule` or `NoExecute` taints
- CPU or memory requests that cannot fit on a ready node
- scheduler condition and `FailedScheduling` Event messages

When a Pod is no longer pending, is scheduled, or is deleted, its scheduling
finding is removed.

## Request Coverage Controller

Source:

- `internal/controller/request_coverage_controller.go`
- `internal/analyzer/request_coverage.go`

This controller watches Pods and checks regular and init containers for missing
or zero CPU and memory requests.

Terminal Pods are ignored. The following namespaces are ignored by default:

```text
kube-node-lease
kube-public
kube-system
local-path-storage
yardmaster-system
```

The list is configurable with `--ignored-request-namespaces`.

## Track Summary Controller

Source:

- `internal/controller/track_summary_controller.go`
- `internal/analyzer/track_summary.go`

This is a cluster-summary controller. Any watched Node or Pod event enqueues the
same synthetic `cluster` reconcile request.

Ready nodes are grouped into Tracks using the first recognized label:

1. `karpenter.sh/nodepool`
2. `eks.amazonaws.com/nodegroup`
3. `cloud.google.com/gke-nodepool`
4. `kubernetes.azure.com/agentpool`
5. instance type, zone, operating system, and architecture as a fallback shape

The analyzer totals ready nodes, scheduled Pods, requested CPU and memory, and
allocatable CPU and memory for each Track.

The controller also reads Karpenter `NodePool` and `NodeClaim` resources when
their CRDs are available. It records NodePool readiness, active nodes, limits,
requirements, taints, disruption settings, and NodeClaim count.

Karpenter objects are read during reconciliation but are not currently direct
watches. Their changes are observed on the next Track reconciliation.

## Workload Owner Resolution

Pod-level findings are promoted to the workload that an operator normally owns:

```text
Pod -> ReplicaSet -> Deployment
Pod -> Job -> CronJob
Pod -> StatefulSet
Pod -> DaemonSet
```

The workload becomes `spec.subject`, while the original Pod is retained in
`spec.related`. If no useful owner exists, the Pod remains the subject.

This logic lives in `internal/controller/workload_owner.go`.

## Finding Lifecycle

Controllers use deterministic finding names derived from the source object or
Track plus a short hash. This gives each finding a stable identity.

On creation:

- the controller writes the `spec`
- `status.firstSeen` and `status.lastSeen` are set

On update:

- the current `spec` is replaced
- `status.lastSeen` is refreshed
- `status.firstSeen` is preserved

On resolution:

- per-Pod controllers delete the corresponding finding
- the Track controller deletes `tracks` findings that are no longer expected

Updates use `RetryOnConflict` because Kubernetes objects use optimistic
concurrency.

## Adding A Controller

When adding another controller:

1. Put cluster reads, writes, watches, and reconcile behavior in
   `internal/controller`.
2. Put deterministic decision logic in `internal/analyzer`.
3. Add focused analyzer unit tests.
4. Define stable finding names and cleanup behavior.
5. Register the reconciler in `cmd/yardmaster/main.go`.
6. Add the minimum RBAC permissions under `config/rbac`.
7. Document watched resources, output category, and failure behavior here.

Before considering the controller complete, verify create, update, no-op,
resolution, deletion, retry, and restart behavior.
