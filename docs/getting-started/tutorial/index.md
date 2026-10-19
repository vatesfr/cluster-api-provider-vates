# Tutorial: a cluster from A to Z

This walkthrough builds a working Kubernetes cluster on Xen Orchestra, starting
from nothing: the VM image in Xen Orchestra, the management cluster, the
credentials, the machine templates, the cluster objects, and the nodes joining.
Every object is written out in full, so nothing is hidden behind a template you
have not read yet.

By the end you have `my-cluster`: one control plane node and one worker, both
`Ready`, reachable with `kubectl`.

## The map

| Segment | What it puts in place |
|---|---|
| [1. The VM image](#1-the-vm-image-in-xen-orchestra) | the template Xen Orchestra clones |
| [2. The management cluster](#2-the-management-cluster) | where the providers run |
| [3. The credentials](#3-the-xen-orchestra-credentials) | how the provider talks to XO |
| [4. The providers](#4-the-providers) | Cluster API, kubeadm, and vates-capi |
| [5. The machine templates](cluster.md#5-the-machine-templates) | `XOMachineTemplate`: what a node's VM is |
| [6. The cluster objects](cluster.md#6-the-cluster-objects) | `Cluster`, `XOCluster`, control plane, workers |
| [7. Apply and watch](run.md#7-apply-and-watch-it-come-up) | the VMs, then the nodes |
| [8. Reach the cluster](run.md#8-reach-the-cluster) | the workload kubeconfig |
| [9. Clean up](run.md#9-clean-up) | delete, and see what goes with it |

There are two ways to end up with the same objects: write them, as this tutorial
does, or have `clusterctl` render them from a template
([Cluster templates](../../reference/templates.md)). Writing them is how you
learn which field does what; the templates are those same objects with
placeholders.

## Who does what

Four controllers share the work, and each owns a different question:

| Controller | Question it answers |
|---|---|
| **Cluster API** | which `Machine`s exist (worker ones come from a `MachineDeployment`) |
| **control plane provider** | which control plane `Machine`s exist, and the cluster CA |
| **bootstrap provider** | how a node joins: the bootstrap configuration, as a `Secret` |
| **vates-capi** (this provider) | *where* the node runs: the VM, its config drive, its address |

They never call each other. They watch the same Kubernetes API and write
objects; the `Cluster` graph is what ties them together. See
[How they fit together](../../concept/how-they-link.md) for the full picture.

## 1. The VM image in Xen Orchestra

The provider clones a VM template for every node, so that template is the first
thing to prepare. Import an AlmaLinux 10 cloud image into Xen Orchestra
(`https://repo.almalinux.org/almalinux/10/cloud/`) and check four things:

- it is **bootable with the OS installed** — not a bare ISO;
- it is **cloud-init enabled**: the bootstrap payload travels as cloud-init
  user-data, and a template that ignores it boots and then sits there;
- it runs the **Xen guest tools**: the provider reads a VM's address through the
  guest agent, and without it the machine never reports an address and never
  joins;
- it is **not tied to a pool**, so it can be cloned wherever `poolID` points.

Note its UUID (XO → the template's *Advanced* tab), along with the pool and
network UUIDs:

```bash
export XO_TEMPLATE_UUID=<vm-template-uuid>
export XO_POOL_UUID=<pool-uuid>
export XO_NETWORK_UUID=<network-uuid>
```

!!! tip "Prefer a faster join?"

    `almalinux-prefilled` bakes kubelet, kubeadm, containerd and the CNI images
    into the image: bootstrap is then minimal and fast, at the cost of being tied
    to one Kubernetes version. This tutorial uses the from-scratch variant, whose
    nodes install their packages on first boot.

## 2. The management cluster

The providers run on a Kubernetes cluster, and your `kubectl` must point at it.
It only runs controllers, not workloads, so it can be small — [kind](https://kind.sigs.k8s.io/)
is the quickest way to get one:

```bash
kind create cluster --name capi
kubectl config current-context     # should say kind-capi
```

## 3. The Xen Orchestra credentials

The provider reads its credentials from a `Secret` in its own namespace, and it
looks for that secret **at startup**: create it before installing anything, or
the controller exits and restarts in a loop.

```bash
kubectl create namespace capi-system
kubectl create secret generic xo-credentials -n capi-system \
  --from-literal=url="https://<your-xoa>" \
  --from-literal=token="<your-xo-token>" \
  --from-literal=insecure="true"     # only for a self-signed XO certificate
```

The keys are `url` and `token`; `insecure` is optional. A cluster or a single
machine can override them with its own `identityRef`, which is what makes
multi-tenant setups possible.

## 4. The providers

`clusterctl` installs Cluster API with its reference kubeadm providers, then the
infrastructure provider:

```bash
clusterctl init --bootstrap kubeadm --control-plane kubeadm
```

`clusterctl` does not know vates-capi, so point it at the release first — in
`~/.config/cluster-api/clusterctl.yaml` (note *cluster-api*, not *clusterctl*):

```yaml
providers:
  - name: vates
    url: https://github.com/vatesfr/cluster-api-provider-vates/releases/latest/infrastructure-components.yaml
    type: InfrastructureProvider
```

```bash
clusterctl init --infrastructure vates
```

Without a version, that installs the latest release; append `:vX.Y.Z` to pin one.

!!! warning "The order matters"

    The credentials secret from segment 3 must exist before this step. If the
    controller is already crash-looping because it started without it, create the
    secret and restart the deployment:

    ```bash
    kubectl -n capi-system rollout restart deployment/vates-capi-controller-manager
    ```

Check that everything is running:

```bash
kubectl -n capi-system get deployment
kubectl get crds | grep vates.infrastructure.cluster.x-k8s.io
```

That lists `xoclusters`, `xoclustertemplates`, `xomachines` and
`xomachinetemplates`.

Next: [the machine templates and the cluster objects](cluster.md).
