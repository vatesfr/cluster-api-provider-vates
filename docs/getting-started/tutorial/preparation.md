# Preparation

Four things have to exist before a cluster can be created: the VM image Xen
Orchestra clones, a management cluster to run the providers, the Xen Orchestra
credentials, and the providers themselves.

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

    The credentials secret from step 3 must exist before this step. If the
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
