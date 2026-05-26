# Yardmaster Production Demo

This is the safe path for trying Yardmaster against a real cluster.

Yardmaster is advisory at this stage. It reads Kubernetes objects and writes only
`DispatchFinding` resources in the `yardmaster-system` namespace.

## Do Not Run On Real Clusters

These targets are for local `kind` demos only:

```bash
make sample
make smoke-kind
make demo-kind
```

They create sample pods and label nodes.

## Real Cluster Trial

Use a kube context that points at the target cluster:

```bash
kubectl config current-context
```

Build and publish an image your cluster can pull, then deploy it:

```bash
make docker-build IMG=<registry>/yardmaster:<tag>
docker push <registry>/yardmaster:<tag>
make deploy IMG=<registry>/yardmaster:<tag>
```

Wait for the controller and dashboard:

```bash
kubectl -n yardmaster-system rollout status deployment/yardmaster
kubectl -n yardmaster-system rollout status deployment/yardmaster-dashboard
```

Open the dashboard locally:

```bash
make dashboard-port-forward
```

Then open:

```text
http://localhost:8088
```

Print the CLI report:

```bash
make report
```

Inspect raw findings:

```bash
kubectl get dispatchfindings -n yardmaster-system
kubectl get dispatchfindings -n yardmaster-system -o yaml
```

## Current Demo Capabilities

- Explains pending pod scheduling blockers.
- Flags active pods with missing or zero CPU/memory requests.
- Groups ready nodes into Tracks using common node pool labels.
- Summarizes requested vs allocatable CPU and memory by Track.
- Shows findings in a local dashboard as Yard, Track, Cargo, and Dispatch objects.

## Current Limits

- No workload mutation.
- No node provisioning.
- No cloud-provider calls.
- No Karpenter `NodePool` or `NodeClaim` API integration yet.
- Dashboard is intended for local port-forward access, not public exposure.
