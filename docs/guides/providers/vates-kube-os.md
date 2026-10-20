# Vates Kube OS

!!! warning "Experimental"

    The Vates Kube OS flow is **experimental**. Its providers are still
    `v1alpha1`, the API can change between releases, and less mileage has gone
    into it than into the [kubeadm](kubeadm.md) flow. Use it to try the
    immutable node image, not for production.

[Vates Kube OS](https://github.com/vatesfr/vates-kube-os) is an **immutable**
node image whose only job is to run Kubernetes: the OS comes up read-only and
every Kubernetes component is a container the node fetches at boot. There is no
`kubeadm` on the host and no cloud-init payload to run — the node reads a single
`vates-node.yaml` document and does the rest.

That document is the seam, and it is why this flow needs **two** Cluster API
providers instead of the usual pair:

| Provider | CRDs | Owns |
|---|---|---|
| **CAVBP** (bootstrap) | `VatesConfig` (+ `Template`) | turns a `Machine` into the `vates-node.yaml` user-data the node reads at first boot (role, Kubernetes version, endpoint, node name, PKI, join material) |
| **CAVCPP** (control plane) | `VatesControlPlane` (+ `Template`) | the cluster certificate authority, and the control plane Machines |

Both ship from one module — `vates-kube-os-capi-providers` — and run the same
image with different commands, landing in `cavp-system`. The infrastructure
half is the same provider you use everywhere else: [vates-capi](../../concept/vates-provider.md)
makes the VM and attaches the config drive holding `vates-node.yaml`.

## How it differs from kubeadm

| | kubeadm | Vates Kube OS |
|---|---|---|
| Node OS | a general-purpose cloud image | an immutable image, `vates-node.yaml` on the config drive |
| Who runs kubeadm | cloud-init, inside the guest | the node image itself, from the document |
| Kubernetes binaries | installed in the image at build time | fetched by the node at first boot |
| Bootstrap CRD | `KubeadmConfig` (+ `Template`) | `VatesConfig` (+ `Template`) |
| Control plane CRD | `KubeadmControlPlane` | `VatesControlPlane` |

The infra side is identical: an `XOMachineTemplate` with `bootstrapProvider:
vates`, a `networkConfig`, and a `resourceSet`, exactly as in the kubeadm flow.

## Install the providers

`clusterctl` does not know these providers, and the bootstrap / control plane
ones are not in the upstream provider index. Exactly like the infrastructure
provider in [Installation](../../getting-started/installation.md), you must
first tell `clusterctl` where to find each of them, in
`~/.config/cluster-api/clusterctl.yaml`:

```yaml
providers:
  - name: vates
    type: BootstrapProvider
    url: https://github.com/vatesfr/vates-kube-os-capi-providers/releases/latest/bootstrap-components.yaml
  - name: vates
    type: ControlPlaneProvider
    url: https://github.com/vatesfr/vates-kube-os-capi-providers/releases/latest/control-plane-components.yaml
  - name: vates
    type: InfrastructureProvider
    url: https://github.com/vatesfr/cluster-api-provider-vates/releases/latest/infrastructure-components.yaml
```

The bootstrap and control plane providers come from **one** repository and
**one** image but **two** components files, because `clusterctl` keys a provider
by its `(type, name)` pair — hence the two entries, and note the
`control-plane-…` spelling. Each `url` may instead be a `file://` path to
manifests you built yourself (`make release-manifests` in the provider
repository). Without this list, `clusterctl` has no source for the `vates`
bootstrap, control plane or infrastructure providers and the command below
fails.

Then install all three together:

```shell
clusterctl init \
  --bootstrap vates \
  --control-plane vates \
  --infrastructure vates
```

Installing the control plane provider adds the cluster CA it owns; the bootstrap
provider needs nothing beyond its own CRDs, and both land in `cavp-system`.

## A cluster

The bootstrap payload is **not** cloud-init: `bootstrapProvider: vates` tells
vates-capi to pass `vates-node.yaml` through untouched. A minimal control plane
side looks like this (the worker `XOMachineTemplate`, `MachineDeployment` and
`MachineHealthCheck` are the usual ones):

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: my-cluster
spec:
  clusterNetwork:
    pods:
      cidrBlocks: [10.244.0.0/16]
  controlPlaneEndpoint:
    host: 10.30.139.30          # the floating endpoint (see the kube-vip guide)
    port: 6443
  controlPlaneRef:
    apiGroup: controlplane.cluster.x-k8s.io
    kind: VatesControlPlane
    name: my-cluster
  infrastructureRef:
    apiGroup: vates.infrastructure.cluster.x-k8s.io
    kind: XOCluster
    name: my-cluster
---
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: my-cluster
spec:
  controlPlaneEndpoint:
    host: 10.30.139.30
    port: 6443
    subnet: 16
  addons:
    ccm: true
    csi: true
    cni: none                   # the node installs flannel itself (cni.plugin below)
---
# Stable node addresses from a pool — see the IPAM guide.
apiVersion: ipam.cluster.x-k8s.io/v1alpha2
kind: InClusterIPPool
metadata:
  name: my-cluster-pool
spec:
  addresses: [10.30.139.200-10.30.139.220]
  gateway: 10.30.0.1
  prefix: 16
---
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachineTemplate
metadata:
  name: my-cluster-cp
spec:
  template:
    spec:
      bootstrapProvider: vates      # the payload is vates-node.yaml, not cloud-init
      namePrefix: my-cluster-cp
      templateID: <your-xo-template-uuid>
      poolID: <your-xo-pool-uuid>
      resourceSet:
        cpus: 2
        memory: 4Gi
        diskSize: 20Gi
      networkConfig:
        addressesFromPools:
          - apiGroup: ipam.cluster.x-k8s.io
            kind: InClusterIPPool
            name: my-cluster-pool
        networks:
          - networkID: <your-xo-network-uuid>
---
apiVersion: bootstrap.cluster.x-k8s.io/v1alpha1
kind: VatesConfigTemplate
metadata:
  name: my-cluster-cp
spec:
  template:
    spec:
      kubernetesVersion: v1.37.1
      cluster:
        vip:                        # only a control plane owns the VIP
          address: 10.30.139.30
          interface: eth0
      network:
        iface: eth0
        mode: dhcp
      cni:
        plugin: flannel
        cidr: 10.244.0.0/16
      cloud:
        provider: external
---
apiVersion: controlplane.cluster.x-k8s.io/v1alpha1
kind: VatesControlPlane
metadata:
  name: my-cluster
spec:
  version: v1.37.1
  replicas: 3
  machineTemplate:
    infrastructureRef:
      apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
      kind: XOMachineTemplate
      name: my-cluster-cp
    bootstrapConfigTemplateRef:
      apiVersion: bootstrap.cluster.x-k8s.io/v1alpha1
      kind: VatesConfigTemplate
      name: my-cluster-cp
```

## The bootstrap config

`VatesConfigSpec` mirrors the OS's own `vatescfg.Config` field for
field: that package stays the single source of truth for semantics and
validation, and the provider encodes the spec into it before a node ever sees
the document. The fields you are most likely to set:

| Field | Meaning |
|---|---|
| `kubernetesVersion` | The Kubernetes release, e.g. `v1.37.1`. Optional — the owner `Cluster`'s version is used when empty. |
| `cluster.controlPlaneEndpoint` | Overrides the `Cluster` endpoint. Normally empty. |
| `cluster.vip` | The floating control plane address. The node runs **kube-vip** for it — kube-vip is this flow's default endpoint. Control plane machines only. |
| `cluster.serviceCIDR`, `cluster.dnsDomain`, `cluster.proxy` | kubeadm addon tuning (`proxy.disabled` only with `cni.plugin: none`). |
| `network` | The node's own addressing: `iface`, `mode` (`dhcp` or `static`), and `cidr`/`gateway`/`dns` when static. |
| `cni` | The pod network: `plugin` (`flannel`, `cilium`, or `none`) and the required `cidr`. Both `flannel` and `cilium` are installed by the node and run alongside kube-proxy. |
| `registry`, `binaries` | Where container images and Kubernetes binaries come from. Optional; upstream by default. |
| `cloud.provider` | `external` to defer the node to an external cloud provider (the XO CCM). |
| `time.servers` | NTP servers the node synchronizes from. |

The join material (`cluster.token`, `cluster.certificateKey`, `cluster.caCertHash`)
is filled in by the provider for workers and joining control plane machines; you
do not set it by hand.

## Verify

```shell
kubectl -n <ns> get vatesconfigs        # one per Machine
kubectl -n <ns> get vatescontrolplane <name>     # control plane ready/available replicas
kubectl -n <ns> get secrets | grep <cluster>     # the per-machine bootstrap Secret
```

Once a node is up, its `vates-node.yaml` is on the config drive and the OS
reports through the Xen guest tools — the same address path the infrastructure
provider uses everywhere. If a machine boots but never joins, check the
bootstrap `Secret` first.

## Where next

- Stable node addresses from a pool: [IPAM](../ipam.md).
- The floating control plane endpoint: [VIP](../vip.md).
- Network configuration: [Networking](../networking.md).
- The other bootstrap flows: [kubeadm](kubeadm.md) and [Talos](talos/index.md).
