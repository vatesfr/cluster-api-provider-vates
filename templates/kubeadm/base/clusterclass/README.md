# ClusterClass Templates

**Prerequisite:** VM templates must be **cloud-init ready** (NoCloud datasource)
with **XE guest tools** installed. See `templates/README.md` for details.

Two ClusterClass variants for **AlmaLinux 10 / RHEL-like** nodes :

| ClusterClass | Worker flavor | Bootstrap |
|---|---|---|
| `vates-rhel-from-scratch` | `worker-from-scratch` | Full (dnf install kubelet/kubeadm) |
| `vates-rhel-prefilled` | `worker-prefilled` | Minimal (assumes pre-baked image) |

## Apply once (cluster-scoped)

```bash
kubectl apply -k templates/kubeadm/base/clusterclass/
```

This registers the ClusterClass and all referenced templates
(KubeadmControlPlaneTemplate, KubeadmConfigTemplate, XOClusterTemplate) in the
management cluster. It only needs to be run once, before creating any clusters.
