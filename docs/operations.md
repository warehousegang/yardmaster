# Yardmaster Operations

This guide covers deployment, configuration, observation, and troubleshooting.
For a cautious real-cluster walkthrough, also read
[Production demo](prod-demo.md).

## Installed Resources

The default Kustomize configuration installs:

- `DispatchFinding` CRD
- `yardmaster-system` Namespace
- operator and dashboard ServiceAccounts
- operator ClusterRole and ClusterRoleBinding
- dashboard namespaced Role and RoleBinding
- operator Deployment
- dashboard Deployment and Service

Render the complete installation without applying it:

```bash
kubectl kustomize config/default
```

## Build And Deploy

Build and publish an image the cluster can pull:

```bash
make docker-build IMG=<registry>/yardmaster:<tag>
docker push <registry>/yardmaster:<tag>
```

Deploy:

```bash
make deploy IMG=<registry>/yardmaster:<tag>
```

For an immutable image reference:

```bash
make deploy IMG=<registry>/yardmaster@sha256:<digest>
```

The Make target generates an ignored Kustomize overlay under `tmp/deploy`,
applies `IMG` through Kustomize's image transformer, and applies the complete
default installation. It does not edit tracked manifests.

Inspect exactly what would be applied:

```bash
make render-deploy IMG=<registry>/yardmaster:<tag>
```

Do not use the demo or sample targets against a real cluster.

## Verify The Deployment

```bash
kubectl -n yardmaster-system get deployments,pods,service
kubectl -n yardmaster-system rollout status deployment/yardmaster
kubectl -n yardmaster-system rollout status deployment/yardmaster-dashboard
kubectl get crd dispatchfindings.yardmaster.dev
```

Inspect findings:

```bash
kubectl get df -n yardmaster-system
kubectl get df -n yardmaster-system -o yaml
```

## Runtime Configuration

### Operator

| Flag | Default | Purpose |
| --- | --- | --- |
| `--metrics-bind-address` | `:8080` | controller-runtime metrics listener. |
| `--health-probe-bind-address` | `:8081` | Liveness and readiness listener. |
| `--finding-namespace` | `yardmaster-system` | Namespace where findings are written. |
| `--ignored-request-namespaces` | Kubernetes system and Yardmaster namespaces | Namespaces skipped by request coverage. |
| `--leader-elect` | `false` | Enables controller manager leader election. |

### Dashboard

| Flag | Default | Purpose |
| --- | --- | --- |
| `--addr` | `:8088` | HTTP listen address. |
| `--kubeconfig` | local kubeconfig when available | Explicit cluster configuration for local use. |
| `--finding-namespace` | `yardmaster-system` | Namespace read by the dashboard. |

## Health And Metrics

The operator exposes:

```text
:8081/healthz
:8081/readyz
:8080/metrics
```

The dashboard exposes:

```text
:8088/healthz
```

The Kubernetes Deployments configure liveness and readiness probes for both
components. The dashboard health endpoint confirms that the HTTP process is
running; it does not currently verify Kubernetes API access.

## Accessing The Dashboard

The Service is intended for in-cluster access or local port forwarding:

```bash
make dashboard-port-forward
```

Open `http://localhost:8088`.

The dashboard currently has no built-in authentication or TLS. Do not expose it
publicly without an authenticated ingress or another trusted access layer.

## Permissions

The operator ServiceAccount can:

- read Pods, Nodes, Events, supported workload owners, and PodDisruptionBudgets
- read Karpenter NodePools and NodeClaims
- create, read, update, patch, watch, and delete `DispatchFinding` objects
- update the `DispatchFinding` status subresource

The dashboard ServiceAccount can only list `DispatchFinding` objects in
`yardmaster-system`.

## Logs

Operator logs:

```bash
kubectl -n yardmaster-system logs deployment/yardmaster
kubectl -n yardmaster-system logs deployment/yardmaster --previous
```

Dashboard logs:

```bash
kubectl -n yardmaster-system logs deployment/yardmaster-dashboard
```

Useful controller log messages include recorded pending Pod findings, request
coverage findings, Track summary counts, and Karpenter read errors.

## Troubleshooting

### No Findings Appear

Check:

```bash
kubectl -n yardmaster-system get pods
kubectl -n yardmaster-system logs deployment/yardmaster
kubectl auth can-i list pods \
  --as=system:serviceaccount:yardmaster-system:yardmaster \
  --all-namespaces
kubectl auth can-i create dispatchfindings.yardmaster.dev \
  --as=system:serviceaccount:yardmaster-system:yardmaster \
  -n yardmaster-system
```

Also verify that the source condition still exists. Controllers delete findings
when a problem resolves.

### Dashboard Shows A Cluster Error

Check dashboard logs and its permission:

```bash
kubectl -n yardmaster-system logs deployment/yardmaster-dashboard
kubectl auth can-i list dispatchfindings.yardmaster.dev \
  --as=system:serviceaccount:yardmaster-system:yardmaster-dashboard \
  -n yardmaster-system
```

### Karpenter Data Is Missing

Verify that Karpenter CRDs and objects exist:

```bash
kubectl get crd nodepools.karpenter.sh nodeclaims.karpenter.sh
kubectl get nodepools.karpenter.sh
kubectl get nodeclaims.karpenter.sh
```

Yardmaster continues without Karpenter data when those CRDs are absent.

### ImagePullBackOff

The cluster must be able to pull the image supplied through `IMG`. For kind,
use the provided `make kind-load` or `make demo-kind` workflow.

### Duplicate Reconciliation With Multiple Replicas

The default Deployment uses one replica. Before scaling the operator above one,
add `--leader-elect=true` to its arguments and verify the leader-election RBAC
required by controller-runtime.

## Updating Yardmaster

For API-compatible code changes:

1. build and push a new immutable image tag
2. apply any changed manifests
3. update both Deployment images
4. monitor rollout and controller logs
5. inspect findings for unexpected churn

The Dockerfile pins both build and runtime base images by multi-architecture
digest. Updating a base image is an intentional source change: inspect the new
registry digest, update the Dockerfile, rebuild, and run `make verify`.

For CRD schema changes, generate and review the CRD first:

```bash
make generate
make manifests
kubectl diff -k config/crd
```

Because the API is `v1alpha1`, treat destructive schema changes as migrations,
not routine image updates.

## Removing Yardmaster

Remove running components:

```bash
make undeploy
```

This removes the operator Deployment and dashboard Deployment and Service. It
leaves RBAC, the Namespace, the CRD, and stored findings.

Complete removal is destructive:

```bash
kubectl delete -k config/rbac
kubectl delete -k config/crd
```

Deleting the Namespace removes namespaced findings. Deleting the CRD removes
all `DispatchFinding` objects in every namespace.

## Current Hardening Gaps

Before treating Yardmaster as a production service, consider:

- immutable versioned images
- container CPU and memory requests and limits
- pod and container security contexts
- NetworkPolicies
- dashboard authentication and TLS
- leader election and availability strategy
- integration and end-to-end tests
- alerting for controller errors and stale findings
