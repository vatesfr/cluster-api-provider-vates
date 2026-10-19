# Vates Kube OS flavour (experimental)

Nodes are **Vates Kube OS** VMs: an immutable image whose first boot reads a
single `vates-node.yaml` document from the config drive. The infrastructure
provider creates the VM and attaches that drive; the bootstrap provider writes
the document; the control plane provider owns the cluster CA and stamps the
control plane Machines.

!!! danger "Experimental"

    All three providers this flavour needs are experimental: **CAVBP** (bootstrap),
    **CAVCPP** (control plane) and **vates-capi** (infrastructure). No support,
    not for production.

    The template is committed here because it is the only way to reproduce the
    node-OS flow end to end, and therefore to debug it. It is **not** part of the
    released, tested set — that is the [kubeadm](../kubeadm.md) and
    [Talos](../talos/index.md) flavours.

## What it wires

| Piece | Object | Role |
|---|---|---|
| Infrastructure | `XOCluster` + `XOMachineTemplate` | the VMs and their config drive |
| Bootstrap | `VatesConfigTemplate` | the `vates-node.yaml` document (role, version, endpoint, PKI, join material) |
| Control plane | `VatesControlPlane` | the cluster CA and the control plane Machines |
| Addresses | `InClusterIPPool` | one address per Machine, drawn through `networkConfig.addressesFromPools` |

The payload is **not** cloud-init: the template declares
`bootstrapProvider: vates`, so vates-capi passes the document through untouched.
The node installs flannel itself (`cni.plugin: flannel`), which is why the
`XOCluster` asks for no CNI — set `cni.plugin` to `none` and
`addons.cni: cilium` to use Cilium instead.

## Prerequisites

- a Vates Kube OS VM template in Xen Orchestra, never booted, with the Xen guest
  tools, and its UUID;
- the three providers, plus the in-cluster IPAM provider:

```bash
clusterctl init --bootstrap vates --control-plane vates \
  --infrastructure vates --ipam in-cluster
```

## Generate a cluster

```bash
export CP_VIP=10.30.139.30
export CP_SUBNET=16
export VM_NAME_PREFIX=my-cluster
export XO_TEMPLATE_UUID=<vates-kube-os-template-uuid>
export XO_POOL_UUID=<xo-pool-uuid>
export XO_NETWORK_UUID=<xo-network-uuid>
export IP_POOL_RANGE=10.30.139.200-10.30.139.220
export IP_POOL_GATEWAY=10.30.0.1

clusterctl generate cluster kubeos \
  --from templates/vates-kube-os/base/clusterctl/cluster-template.yaml \
  --kubernetes-version v1.31.4 \
  --control-plane-machine-count 3 --worker-machine-count 4 | kubectl apply -f -
```

Three control plane Machines is what a highly available cluster needs: the
`InClusterIPPool` hands each node a stable address, and the control plane VIP is
served by kube-vip.

## Where next

- [Vates Kube OS guide](../../../guides/providers/vates-kube-os.md) — the flow in
  detail, and how it differs from kubeadm.
- [IPAM](../../../guides/ipam.md) — the address pool this template draws from.
