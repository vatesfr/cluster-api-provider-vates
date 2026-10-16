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
│   ├── clusterctl/       # clusterctl-compatible templates (distribution)
│   └── kustomization.yaml
├── components/           # Optional kustomize components (Cilium CNI modes)
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
    url: file://${HOME}/.config/cluster-api/overrides/infrastructure-vates/v1.1.0-rc4/infrastructure-components.yaml
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
      talosVersion: <your-talos-version>   # e.g. v1.13.9 — must match the image
      strategicPatches:
        - |
          machine:
            time:
              servers:
                - time.cloudflare.com
                - time.google.com
            network:
              interfaces:
                - interface: eth0
                  dhcp: true
                  vip:
                    ip: <your-cp-vip>
        - |
          apiVersion: v1alpha1
          kind: KubeletConfig
          extraArgs:
            cloud-provider: external
```

The kubelet lives in its own document since Talos 1.12. Patching
`machine.kubelet` here is rejected by Talos at boot with
`kubelet config is already set in v1alpha1 config`, and the node stops
before starting any service.

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
export TALOS_VERSION=<your-talos-version>   # e.g. v1.13.9 — must match the image
export XO_TEMPLATE_UUID=<your-xo-talos-template-uuid>
export XO_POOL_UUID=<your-xo-pool-uuid>
export XO_NETWORK_UUID=<your-xo-network-uuid>

clusterctl generate cluster my-cluster \
  --from templates/talos/base/clusterctl/cluster-template.yaml \
  --kubernetes-version v1.36.1 \
  | kubectl apply -f -
```

`CONTROL_PLANE_MACHINE_COUNT` and `WORKER_MACHINE_COUNT` are clusterctl
built-in flags that **default to 1 and 0** (not the `base/` replicas). To match
the base templates (3 control plane, 2 workers), pass them explicitly:

```bash
clusterctl generate cluster my-cluster \
  --from templates/talos/base/clusterctl/cluster-template.yaml \
  --kubernetes-version v1.36.1 \
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

## CNI: Cilium

By default the templates keep the Talos default CNI (flannel) and kube-proxy.
The vates provider can instead install [Cilium](https://cilium.io/) (1.16) in
one of two modes:

| Mode | Talos machine config (all nodes) | `XOCluster.spec.addons` |
|---|---|---|
| **Standard** — Cilium as CNI, kube-proxy kept | `cluster.network.cni.name: none` | `cni: cilium` |
| **Kube-proxy replacement** — Cilium replaces kube-proxy | `cluster.network.cni.name: none` and `cluster.proxy.disabled: true` | `cni: cilium` and `cilium.kubeProxyReplacement: true` |

The Talos side and the `XOCluster` side must agree: the provider does not read
the Talos machine config. The kustomize components and the clusterctl flavor
below set both sides together.

### How Cilium is installed

The `XOCluster` reconciler renders the Cilium manifest and stores it in a
`ClusterResourceSet` (`cni-deployment-<cluster>`, strategy `ApplyOnce`). Cluster
API applies it from the management cluster through the control plane endpoint
as soon as the control plane is initialized, so no CNI, kube-proxy, FluxCD or
any other in-cluster component is needed beforehand:

1. CABPT/CACPPT generate the machine configs with the patches above: Talos
   deploys neither flannel nor (in replacement mode) kube-proxy.
2. The first control plane boots; its API server runs on the host network and
   is reachable through the VIP.
3. Cluster API applies the Cilium `ClusterResourceSet` (and the CCM/CSI ones).
4. The Cilium agent and operator run on the host network and tolerate the
   not-ready taints, so they start on nodes without a CNI and reach the API
   server directly (see below). Nodes become `Ready` once the agent is up.
5. The Xen Orchestra CCM (host network) sets the node `providerID`; in
   replacement mode it reaches the API server through the `kubernetes` Service
   once Cilium routes it.

On Talos, the provider detects the cluster through its `TalosControlPlane` and
renders the Talos values from the
[Talos Cilium guide](https://docs.siderolabs.com/kubernetes-guides/cni/deploying-cilium):
no `SYS_MODULE` capability (Talos does not let workloads load kernel modules),
the cgroup v2 hierarchy mounted by Talos at `/sys/fs/cgroup`, and
`ipam.mode=kubernetes` (pod CIDRs taken from the `Cluster` pods CIDR).

### API server endpoint (kube-proxy replacement)

Without kube-proxy, the `kubernetes` Service ClusterIP is only routed once
Cilium runs, so Cilium itself must reach the API server directly
(`k8sServiceHost` / `k8sServicePort` Helm values). On Talos the provider uses
**KubePrism**, `localhost:7445`: a load balancer to all the control plane
nodes that Talos runs on every node, enabled by default since Talos 1.6. Unlike
the control plane VIP, it is local to each node and does not depend on which
control plane node holds the VIP. Do not disable
`machine.features.kubePrism`.

To use another endpoint, set both fields:

```yaml
spec:
  addons:
    cni: cilium
    cilium:
      kubeProxyReplacement: true
      k8sServiceHost: <host>
      k8sServicePort: <port>
```

The API rejects `kubeProxyReplacement: true` without `cni: cilium`, a host
without a port (and vice versa), and an endpoint without
`kubeProxyReplacement: true`.

### Enable it with an overlay

Add one of the components to your overlay `kustomization.yaml` (only one):

```yaml
resources:
  - ../../base

components:
  - ../../components/cilium                          # standard mode
  # - ../../components/cilium-kube-proxy-replacement # kube-proxy replacement
```

The components append a strategic patch to the `TalosControlPlane` and the
`TalosConfigTemplate`, and set `spec.addons` on the `XOCluster`. If your overlay
also patches `spec.addons` of the `XOCluster` (e.g. to disable the CSI), set
the CNI fields in that patch too.

### Enable it with clusterctl

Use the `cluster-template-cilium.yaml` flavor; `CILIUM_KUBE_PROXY_REPLACEMENT`
drives both the Talos `cluster.proxy.disabled` and the Cilium mode:

```bash
export CILIUM_KUBE_PROXY_REPLACEMENT=true   # default: false (standard mode)

clusterctl generate cluster my-cluster \
  --from templates/talos/base/clusterctl/cluster-template-cilium.yaml \
  --kubernetes-version v1.36.1 \
  --control-plane-machine-count 3 --worker-machine-count 2 \
  | kubectl apply -f -
```

### Limitations

- The mode is chosen at cluster creation. The Cilium manifest is applied once
  (`ApplyOnce`), so changing `spec.addons.cilium` afterwards does not
  reconfigure a running cluster, and switching an existing cluster between
  modes also requires rolling the Talos machine config.
- Only the bundled Cilium settings are available (VXLAN tunnel, `eth0`, Hubble
  disabled). For a customized Cilium, keep `cni: none` and install it yourself.

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
| `machine.time.servers` | Use `time.cloudflare.com` / `time.google.com` instead of the default NTP server; some servers (e.g. `pool.ntp.org`) answer "kiss of death" on some networks |
| `machine.network.interfaces[].vip` | Talos native shared IP (control plane VIP) — **not** kube-vip |
| `KubeletConfig.extraArgs.cloud-provider: external` | Offload node initialization to the Xen Orchestra CCM, which sets the `providerID` required by CAPI |

> `TalosControlPlane.spec.controlPlaneConfig.controlplane.talosVersion` **must**
> be set to the Talos version of the image. Without it, CABPT generates a config
> for its own default version, which the node rejects (unknown documents), and
> the machine stays in maintenance.

## Cloud Controller Manager

The `XOCluster` reconciler creates a `ClusterResourceSet` that deploys the
Xen Orchestra CCM and CSI driver into the workload cluster. It matches the
Cluster via the `cluster.x-k8s.io/cluster-name` label; the provider adds this
label automatically if it is missing. The CCM assigns the `providerID`
(`xenorchestra://<pool>/<vm>`) to each node, which lets CAPI link
`Machine` ↔ `Node`.