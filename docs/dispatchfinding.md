# DispatchFinding API

`DispatchFinding` is the main Yardmaster API object. It represents a scheduling,
capacity, or workload-configuration finding that can be consumed through normal
Kubernetes clients.

## Identity

```text
API group:  yardmaster.dev
Version:    v1alpha1
Kind:       DispatchFinding
Plural:     dispatchfindings
Short name: df
Scope:      Namespaced
```

The default storage namespace is `yardmaster-system`.

## Schema

### Spec

| Field | Required | Meaning |
| --- | --- | --- |
| `severity` | Yes | `info`, `warning`, or `critical`. |
| `category` | Yes | Finding group such as `scheduling`, `requests`, or `tracks`. |
| `subject` | Yes | The primary Kubernetes or Yardmaster object the operator should consider. |
| `related` | No | Supporting objects, such as the Pod behind a workload finding. |
| `summary` | Yes | Short human-readable explanation. |
| `detail` | No | Evidence or technical reasoning behind the summary. |
| `recommendations` | No | Suggested next checks or actions. |

A subject contains:

| Field | Required | Meaning |
| --- | --- | --- |
| `apiVersion` | Yes | API version of the referenced object. |
| `kind` | Yes | Kind of the referenced object. |
| `namespace` | No | Namespace for namespaced objects. |
| `name` | Yes | Object name. |

### Status

| Field | Meaning |
| --- | --- |
| `firstSeen` | When the controller first created the current finding. |
| `lastSeen` | Most recent successful reconciliation that observed the finding. |

The CRD enables the Kubernetes status subresource, so controllers update status
separately from spec.

## Example

```yaml
apiVersion: yardmaster.dev/v1alpha1
kind: DispatchFinding
metadata:
  name: requests-pod-default-api-8d13a642
  namespace: yardmaster-system
  labels:
    yardmaster.dev/category: requests
    yardmaster.dev/subject: deployment
spec:
  severity: info
  category: requests
  subject:
    apiVersion: apps/v1
    kind: Deployment
    namespace: default
    name: api
  related:
    - apiVersion: v1
      kind: Pod
      namespace: default
      name: api-6f775cf4f6-x87rv
  summary: Workload has containers without CPU or memory requests.
  detail: "Missing requests: container api cpu, container api memory."
  recommendations:
    - Set CPU and memory requests for each container.
status:
  firstSeen: "2026-07-16T12:00:00Z"
  lastSeen: "2026-07-16T12:05:00Z"
```

## Labels

Controllers currently add:

```text
yardmaster.dev/category
yardmaster.dev/subject
```

The Track controller uses the category label when removing stale Track
findings. Treat these labels as part of Yardmaster's current internal contract;
they are not yet a separately versioned public API.

## Working With Findings

```bash
kubectl get dispatchfindings -n yardmaster-system
kubectl get df -n yardmaster-system
kubectl get df -n yardmaster-system -o yaml
kubectl get df -n yardmaster-system \
  -l yardmaster.dev/category=scheduling
```

The CRD adds printer columns for severity, category, subject, and age.

## Go Types And Scheme Registration

The source files under `api/v1alpha1` have separate responsibilities:

| File | Responsibility |
| --- | --- |
| `dispatchfinding_types.go` | API structs, validation markers, status subresource, and printer columns. |
| `groupversion_info.go` | Group/version identity and scheme registration. |
| `doc.go` | Package-level API group and object-generation markers. |
| `zz_generated.deepcopy.go` | Generated safe-copy implementations required by Kubernetes runtime objects. |

`DispatchFindingList` is the list form used by Kubernetes list operations:

```go
var findings yardv1alpha1.DispatchFindingList
err := k8sClient.List(ctx, &findings)
```

## Generation Workflow

The Go definitions and Kubebuilder markers are the source of truth.

After changing API types:

```bash
make generate
make manifests
go test ./...
```

`make generate` recreates DeepCopy code.

`make manifests` recreates the CRD YAML under `config/crd`.

Do not manually edit:

```text
api/v1alpha1/zz_generated.deepcopy.go
config/crd/yardmaster.dev_dispatchfindings.yaml
```

## Versioning

`v1alpha1` means the API is still evolving. Before changing field meaning,
removing fields, or introducing another served version, decide how existing
stored objects will remain readable. A future multi-version CRD may require
conversion and migration planning.
