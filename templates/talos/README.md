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
└── overlays/             # Per-environment values (git-ignored, not distributed)
    └── <your-env>/       # Your environment — create one, see "Create an overlay"
```

`base/` is the community artifact: it uses placeholders
(`<your-xo-talos-template-uuid>`, `<your-xo-pool-uuid>`, `<your-xo-network-uuid>`,
`<your-cp-vip>`) and contains **no** environment-specific resource IDs.
`overlays/` holds per-environment overrides and is meant for local testing only.
Create your own overlay directory (e.g. `my-env/`); do not edit `base/`.

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

The Talos bootstrap and control plane providers (CABPT / CACPPT) are not
registered by default in `clusterctl`. You must add them to your clusterctl
configuration before running `init`.

Add the following to `~/.cluster-api/clusterctl.yaml` (create the file if it
does not exist):

```yaml
providers:
  - name: "talos"
    url: "https://github.com/siderolabs/cluster-api-bootstrap-provider-talos/releases/latest/bootstrap-components.yaml"
    type: "BootstrapProvider"
  - name: "talos"
    url: "https://github.com/siderolabs/cluster-api-control-plane-provider-talos/releases/latest/control-plane-components.yaml"
    type: "ControlPlaneProvider"
```

Then install all providers (CAPI core + Talos bootstrap/control plane + vates):

```bash
clusterctl init --bootstrap talos --control-plane talos --infrastructure vates
```

Verify the providers are registered:

```bash
clusterctl get providers
```

Check that the vates controller is running:

```bash
kubectl get deployment -n capi-system vates-capi-controller-manager
kubectl logs -n capi-system deployment/vates-capi-controller-manager -c manager -f
```

For local development, the same `Makefile.dev` workflow applies — the controller
handles both kubeadm and Talos bootstrap providers automatically:

```bash
make -f Makefile.dev build    # Build controller image
make -f Makefile.dev push     # Load into Kind cluster
make -f Makefile.dev restart  # Restart the controller pod
```


## Usage

> `base/` templates use placeholders and must **not** be edited directly.
> Always create an **overlay** to provide your environment's values — this
> keeps `base/` reusable and avoids committing environment-specific UUIDs.

### Create an overlay

An overlay reuses all `base/` resources via kustomize and overrides the
environment-specific values with **patches**. Create a directory for your
environment containing a `kustomization.yaml` that pulls in `base/` plus
one patch file per resource you need to customize.

Create the directory and files:

```bash
mkdir -p templates/talos/overlays/my-env/
```

`kustomization.yaml` — references `base/` and lists the patches:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ../../base

patches:
  - path: patch-xomachinetemplate-cp.yaml
    target:
      kind: XOMachineTemplate
      name: talos-cp
  - path: patch-xomachinetemplate-worker.yaml
    target:
      kind: XOMachineTemplate
      name: talos-worker
  - path: patch-controlplane.yaml
    target:
      kind: TalosControlPlane
      name: talos-cp
  - path: patch-xocluster.yaml
    target:
      kind: XOCluster
      name: talos-cluster
```

`patch-xomachinetemplate-cp.yaml` — the control plane VM template, pool and
network (**replace the `<your-...-uuid>` with your XO values**):

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachineTemplate
metadata:
  name: talos-cp
spec:
  template:
    spec:
      templateID: <your-xo-talos-template-uuid>
      poolID: <your-xo-pool-uuid>
      networkConfig:
        networks:
          - networkID: <your-xo-network-uuid>
```

`patch-xomachinetemplate-worker.yaml` — same values for the worker template:

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachineTemplate
metadata:
  name: talos-worker
spec:
  template:
    spec:
      templateID: <your-xo-talos-template-uuid>
      poolID: <your-xo-pool-uuid>
      networkConfig:
        networks:
          - networkID: <your-xo-network-uuid>
```

`patch-controlplane.yaml` — the control plane VIP and machine config patches
(**replace `<your-cp-vip>` with your cluster VIP**):

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1alpha3
kind: TalosControlPlane
metadata:
  name: talos-cp
spec:
  controlPlaneConfig:
    controlplane:
      strategicPatches:
        - |
          machine:
            time:
              servers:
                - time.cloudflare.com
                - pool.ntp.org
            kubelet:
              extraArgs:
                cloud-provider: external
            network:
              interfaces:
                - interface: eth0
                  dhcp: true
                  vip:
                    ip: <your-cp-vip>
```

`patch-xocluster.yaml` — the cluster's control plane endpoint (**replace the
VIP and subnet with yours**):

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: talos-cluster
spec:
  controlPlaneEndpoint:
    host: <your-cp-vip>
    port: 6443
    subnet: <your-cp-subnet-cidr-bits>
```

Apply your overlay:

```bash
kubectl apply -k templates/talos/overlays/my-env/
```

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