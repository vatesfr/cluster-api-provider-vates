# Talos CAPI templates

Flat (non-ClusterClass) templates to deploy a Talos Linux Kubernetes cluster on
Xen Orchestra through Cluster API.

This is an alternative to the kubeadm templates (`templates/kubeadm/base/clusterclass/`,
`templates/kubeadm/base/machinetemplates/`, `templates/kubeadm/base/example-cluster/`) that use the
`KubeadmControlPlane` + cloud-init flow. Talos replaces both the OS and the
bootstrap provider.

## Layout

```
talos/
├── base/                 # Community templates — placeholders, no resource IDs
│   ├── kustomization.yaml
│   ├── talos-cluster.yaml
│   ├── talos-xocluster.yaml
│   ├── talos-xomachinetemplate-cp.yaml
│   ├── talos-xomachinetemplate-worker.yaml
│   ├── talos-controlplane.yaml
│   ├── talos-configtemplate.yaml
│   └── talos-machinedeployment.yaml
└── overlays/
    └── lab1/             # Local environment example — real UUIDs, not distributed
```

`base/` is the community artifact: it uses placeholders
(`<your-xo-talos-template-uuid>`, `<your-xo-pool-uuid>`, `<your-xo-network-uuid>`,
`<your-cp-vip>`) and contains **no** environment-specific resource IDs.
`overlays/` holds per-environment overrides (e.g. `lab1/` with the actual
UUIDs) and is meant for local testing only.

## Prerequisites

### Xen Orchestra template

The Talos VM template must be:

- built for the **`nocloud`** platform with the `siderolabs/xen-guest-agent`
  system extension (via the [Talos Image Factory](https://factory.talos.dev/),
  "Server Cloud" / `nocloud`);
- created from the RAW image and **never booted** (no stale certs/state);
- with **`viridian: false`**. When `viridian` is `true`, XO prepends an MBR to
  the cloud-init config drive, which Talos blockutils cannot mount, leaving the
  node in maintenance mode.

### Management cluster

The Talos bootstrap and control plane providers must be installed:

```bash
clusterctl init --bootstrap talos --control-plane talos --infrastructure vates
```

## Usage

### Community / from the base templates

Replace the placeholders in `base/` with your XO values, then apply:

```bash
kubectl apply -k templates/talos/base/
```

### With a local overlay

    kubectl apply -k templates/talos/overlays/lab1/

Then monitor:

```bash
kubectl get cluster,taloscontrolplane,machinedeployment,xomachine -w
```

Retrieve the `talosconfig` once the control plane is up:

```bash
kubectl get talosconfig -n default -o yaml -o jsonpath='{.status.talosConfig}'
```

## Machine config patches

The base templates apply the following `strategicPatches`:

| Patch | Purpose |
|---|---|
| `machine.time.servers` | Use `time.cloudflare.com` / `pool.ntp.org` instead of the default NTP server, which may answer "kiss of death" on some networks |
| `machine.network.interfaces[].vip` | Talos native shared IP (control plane VIP) — **not** kube-vip |
| `machine.kubelet.extraArgs.cloud-provider: external` | Offload node initialization to the Xen Orchestra CCM, which sets the `providerID` required by CAPI |

## Cloud Controller Manager

The `XOCluster` reconciler creates a `ClusterResourceSet` that deploys the
Xen Orchestra CCM and CSI driver into the workload cluster. It matches the
Cluster via the `cluster.x-k8s.io/cluster-name` label; the provider adds this
label automatically if it is missing. The CCM assigns the `providerID`
(`xenorchestra://<pool>/<vm>`) to each node, which lets CAPI link
`Machine` ↔ `Node`.