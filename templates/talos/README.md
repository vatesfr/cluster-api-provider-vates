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
│   ├── machinetemplates/ # XOMachineTemplate (control plane / worker)
│   ├── example-cluster/  # Cluster scaffold + MachineHealthChecks
│   ├── clusterctl/       # clusterctl-compatible template (distribution)
│   └── kustomization.yaml
└── overlays/             # Per-environment values (git-ignored, not distributed)
    └── <your-env>/       # Your environment — create one, see "Create an overlay"
```

`base/` is the community artifact: it uses placeholders
(`<your-xo-talos-template-uuid>`, `<your-xo-pool-uuid>`, `<your-xo-network-uuid>`,
`<your-cp-vip>`) and contains **no** environment-specific resource IDs.
`overlays/` holds per-environment overrides and is meant for local testing only.
Create your own overlay directory (e.g. `my-env/`); do not edit `base/`.

The layout mirrors the kubeadm templates (`templates/kubeadm/`). Unlike kubeadm,
Talos has no `clusterclass/` directory: the Talos control plane provider (CACPPT)
does not support ClusterClass / managed topology yet, so these are flat
(`TalosControlPlane` + `MachineDeployment`) templates.

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

The `TalosControlPlane` and `TalosConfig` CRDs are **not** provided by this
repo — they come from the Talos control plane / bootstrap providers (CACPPT /
CABPT), which install their CRDs into the management cluster. These are known
by `clusterctl` by default. The vates infrastructure provider, however, is not
published to the network yet and must be installed manually.

Two approaches:

**Option A — everything via `clusterctl` (recommended)**

The vates provider is not published to the network, so configure a local file
override **before** running `clusterctl init`. One command regenerates `dist/`,
refreshes the local clusterctl overrides and creates
`~/.config/cluster-api/clusterctl.yaml` if needed:

```bash
make -f Makefile.dev dev-overrides
```

If an existing `~/.config/cluster-api/clusterctl.yaml` does not register vates
(the command warns about it), add it manually:

```yaml
providers:
  - name: vates
    url: file://${HOME}/.config/cluster-api/overrides/infrastructure-vates/v0.1.0/infrastructure-components.yaml
    type: InfrastructureProvider
```

Then install CAPI core + Talos bootstrap/control plane + vates in one command:

```bash
clusterctl init --bootstrap talos --control-plane talos --infrastructure vates
```

**Option B — vates deployed via `install.yaml`**

Install the CAPI core and the Talos providers with `clusterctl` (this is what
installs the `TalosControlPlane` / `TalosConfig` CRDs):

```bash
clusterctl init --bootstrap talos --control-plane talos
```

Then deploy the vates infrastructure provider manually. The bundled
`dist/install.yaml` contains everything (CRDs, RBAC, Deployment):

```bash
kubectl apply -f dist/install.yaml
```

> `dist/install.yaml` only deploys the vates provider — it does **not** install
> CAPI or the Talos CRDs. Those must come from `clusterctl init` (Option A or B).

### Apply the Talos RBAC binding

The default vates RBAC only binds the kubeadm control plane (KCP). For the Talos
flow, the control plane (CACPPT) and bootstrap (CABPT) providers need access to
the `XOMachineTemplate` resources. This binding is **not** included by default;
apply it separately:

```bash
kubectl apply -k config/rbac/talos
```

Without it, the Talos control plane cannot read `XOMachineTemplate` and no
control plane Machine is created.

Verify the controllers are running:

```bash
kubectl get deployment -n capi-system -l cluster.x-k8s.io/provider
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

### Alternative — clusterctl generate

For distribution or one-off clusters, use `clusterctl generate` instead of an
overlay:

```bash
export CP_VIP=<your-cp-vip>
export CP_SUBNET=<your-subnet-cidr-bits>
export VM_NAME_PREFIX=<your-vm-name-prefix>
export KUBERNETES_VERSION=v1.36.1
export TALOS_VERSION=v1.13.9
export XO_TEMPLATE_UUID=<your-xo-talos-template-uuid>
export XO_POOL_UUID=<your-xo-pool-uuid>
export XO_NETWORK_UUID=<your-xo-network-uuid>

clusterctl generate cluster my-cluster \
  --from templates/talos/base/clusterctl/cluster-template.yaml \
  | kubectl apply -f -
```

`CONTROL_PLANE_MACHINE_COUNT` and `WORKER_MACHINE_COUNT` are clusterctl
built-in flags that **default to 1 and 0** (not the `base/` replicas). To match
the base templates (3 control plane, 2 workers), pass them explicitly:

```bash
clusterctl generate cluster my-cluster \
  --from templates/talos/base/clusterctl/cluster-template.yaml \
  --control-plane-machine-count 3 --worker-machine-count 2 \
  | kubectl apply -f -
```

See `templates/talos/base/clusterctl/cluster-template.yaml` for the full list
of supported variables.

Then monitor:

```bash
kubectl get cluster,taloscontrolplane,machinedeployment,xomachine -w
```

Retrieve the `talosconfig` once the control plane is up:

```bash
kubectl get talosconfig -n default -o yaml -o jsonpath='{.status.talosConfig}'
```

## Self-healing

When a VM is killed or becomes unresponsive on Xen Orchestra, the node reports
`NotReady` and the Cluster API **MachineHealthCheck** (MHC) triggers the
replacement — the infrastructure provider never recreates VMs on its own.

The base templates ship two MHC objects (applied automatically through
`base/` and your overlay):

| MHC | Targets | Remediation |
|---|---|---|
| `talos-cluster-cp` | control plane nodes (`cluster.x-k8s.io/control-plane`) | The `TalosControlPlane` replaces the unhealthy machine |
| `talos-cluster-worker` | worker nodes (`cluster.x-k8s.io/deployment-name: talos-worker-md-0`) | MHC deletes the Machine, the MachineSet creates a replacement |

A node stuck in `Ready=False` / `Unknown` for 5 minutes is considered
unhealthy. On remediation, a new `Machine` (and thus a new `XOMachine` → VM)
is created automatically. MHC requires the core Cluster API controllers,
installed by `clusterctl init`.

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