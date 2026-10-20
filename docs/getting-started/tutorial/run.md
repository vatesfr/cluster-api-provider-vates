# 7-9. Apply, reach, clean up

## 7. Apply and watch it come up

```bash
kubectl apply -f machine-templates.yaml
kubectl apply -f cluster.yaml
```

Nothing happens for a few seconds, then it starts. Three views, from the most
generic to the most specific:

```bash
kubectl get machines -w                 # the generic view: what CAPI knows
kubectl get xomachines -w               # one per VM, the infrastructure view
kubectl get cluster my-cluster -w       # the summary
```

The order is always the same, and knowing it is most of the debugging:

| What appears | Meaning |
|---|---|
| `Machine` (Pending) | CAPI created it and is waiting for a bootstrap payload |
| `XOMachine` | vates-capi created the infrastructure object, one per `Machine` |
| `XOMachine.spec.providerID` set | the VM exists: cloned, sized, started |
| `XOMachine.status.addresses` set | the guest agent reported an address, so the node is up |
| `Machine` (Provisioned) | CAPI matched the node and gave it its `nodeRef` |
| `Machine` (Running, Ready) | the node joined the cluster |

In Xen Orchestra, VMs named `my-cluster-cp-…` and `my-cluster-worker-…` appear,
cloned from your template, each with a small `XO CloudConfigDrive` beside its
disk. That drive is the seam: it carries the cloud-init payload the node reads at
first boot.

The provider's own view of all this:

```bash
kubectl -n capi-system logs -l app.kubernetes.io/name=vates-capi -f
```

!!! note "First boot is slow with a from-scratch image"

    Each node installs kubelet, kubeadm and containerd before it can join. Give
    the first control plane node several minutes, and remember that each node
    installs itself, reboots once, then joins: three boot cycles, not one.

### When something does not come up

| Symptom | Look at |
|---|---|
| `Machine` stays `Pending`, log says *waiting for bootstrap data secret* | the bootstrap provider: `kubectl -n cabpk-system logs deploy/cabpk-controller-manager` |
| `XOMachine` has no `providerID` | vates-capi's log; usually XO credentials or a bad `templateID` / `poolID` |
| `XOMachine` has a `providerID` but no `addresses` | the guest agent inside the VM: without it, no address is ever reported |
| the node boots but never joins | the console of the VM in XO, and [Troubleshooting](../../operations/troubleshooting.md) |
| the node is rolled over and over | the Kubernetes version disagrees between KCP and the image |

## 8. Reach the cluster

Once the control plane is up, take its kubeconfig:

```bash
clusterctl get kubeconfig my-cluster > my-cluster.kubeconfig
kubectl --kubeconfig my-cluster.kubeconfig get nodes
```

The nodes stay `NotReady` until a CNI is running. The shipped template installs
**Cilium** through a `ClusterResourceSet`, which takes a minute or two after the
first node registers:

```bash
kubectl --kubeconfig my-cluster.kubeconfig -n kube-system get pods
```

When the Cilium pods are up, the nodes turn `Ready` and you have a working
cluster: `kubectl --kubeconfig my-cluster.kubeconfig get pods -A`.

## 9. Clean up

```bash
kubectl delete -f cluster.yaml
kubectl delete -f machine-templates.yaml
```

Deleting cascades: the provider stops each VM and removes it from Xen Orchestra,
and the config drive goes with it. Watch it happen:

```bash
kubectl get xomachines -w
kubectl -n capi-system logs -l app.kubernetes.io/name=vates-capi -f
```

!!! warning "Check Xen Orchestra afterwards"

    A create that failed and retried can leave a `Halted` VM behind, with no tags
    and no owner. If you see one, delete it by hand: nothing will ever reclaim
    it.

## Where next

- Scale the workers by editing `replicas`, or grow the cluster with
  `kubectl scale machinedeployment my-cluster-md-0 --replicas 3`.
- Add SSH keys, change the CNI, size the VMs differently:
  [Custom resources](../../reference/crds/xocluster.md).
- Give the nodes static addresses from an IPAM pool instead of DHCP:
  [IPAM](../../guides/ipam.md).
- Prefer an immutable, cloud-init-free OS: the
  [Talos flow](../../guides/providers/talos/index.md).
