# Cluster Templates

This directory contains reusable CAPI templates for deploying Kubernetes clusters on
XenServer / XCP-ng via the Vates infrastructure provider.

## Prerequisites — VM template preparation in XO

Machines are provisioned via **cloud-init**. You need a VM template in Xen Orchestra
that meets the following requirements:

- **cloud-init installed and working** — the template must accept cloud-init
  configuration (NoCloud datasource, used by the provider)
- **XE guest tools installed** — required for the provider to retrieve the VM IP
  after creation
- **Containerd pre-installed** (`almalinux-prefilled` variant) or installed at boot
  (`almalinux-fromscratch` variant)
- **SSH enabled** — for debugging via `kubectl debug node` or direct access
- **Access to a container registry mirror** (optional but recommended) — the
  bootstrap downloads kubelet/kubeadm from `https://pkgs.k8s.io`

Create this template in XO by importing an AlmaLinux 10 cloud image from
`https://repo.almalinux.org/almalinux/10/cloud/`, or build one with Packer
(see `packer/`).

Once the template is ready, note its **UUID** (templateID) and your **pool UUID**
(poolID) — you will need them in the XOMachineTemplate manifests.

> **Note:** an empty template (no OS) or a VM cloned from an existing VM will
> **not** work — cloud-init must be able to run on first boot to configure
> the hostname, SSH keys, and pre/post kubeadm commands.

## Structure

```
templates/
├── kubeadm/                             # kubeadm bootstrap flow (default)
│   ├── base/                            # community templates — placeholders
│   │   ├── clusterclass/                # ClusterClass + non-machine templates
│   │   │   ├── kustomization.yaml
│   │   │   ├── almalinux-xoclustertemplate.yaml  # shared XOClusterTemplate
│   │   │   ├── almalinux-fromscratch/            # full bootstrap (dnf install kubelet...)
│   │   │   └── almalinux-prefilled/               # pre-baked image (minimal bootstrap)
│   │   ├── machinetemplates/            # XOMachineTemplate (placeholders)
│   │   │   ├── almalinux-fromscratch/
│   │   │   └── almalinux-prefilled/
│   │   ├── example-cluster/             # Cluster scaffold + CP MachineHealthCheck
│   │   ├── clusterctl/                  # clusterctl-compatible template (kubeadm)
│   │   └── kustomization.yaml
│   ├── overlays/                        # per-environment values (not distributed)
│   │   └── <your-env>/                  # create one per environment
│   └── packer/                          # AlmaLinux cloud image builder
├── talos/                               # Talos bootstrap flow
│   ├── base/                            # community templates — placeholders
│   │   ├── machinetemplates/            # XOMachineTemplate (control plane / worker)
│   │   ├── example-cluster/             # Cluster scaffold + MachineHealthChecks
│   │   ├── clusterctl/                  # clusterctl-compatible template (Talos)
│   │   └── kustomization.yaml
│   ├── overlays/                        # per-environment values (not distributed)
│   │   └── <your-env>/                  # create one per environment
│   └── kustomization.yaml
├── vates-kube-os/                       # Vates Kube OS flow (experimental)
│   ├── base/                            # community templates — placeholders
│   │   └── clusterctl/                  # clusterctl-compatible template
│   └── README.md
└── README.md
```

Three bootstrap flows are supported, one per top-level directory. Each flow
follows a `base/` + `overlays/` layout:

- **kubeadm** (default): `kubeadm/base/clusterclass/` + `kubeadm/base/machinetemplates/` +
  `kubeadm/base/example-cluster/` with `KubeadmControlPlane` and cloud-init. See below.
- **Talos** : `talos/base/` (community, placeholders) + `talos/overlays/`
  (per-environment values) with `TalosControlPlane` + `TalosConfigTemplate` on
  an immutable, `nocloud` Talos image. Its `base/` mirrors the kubeadm layout
  (`machinetemplates/`, `example-cluster/`, `clusterctl/`) but has no
  `clusterclass/`: the Talos control plane provider does not support
  ClusterClass / managed topology yet, so these are flat templates.
- **Vates Kube OS** (experimental): `vates-kube-os/base/clusterctl/` with
  `VatesControlPlane` + `VatesBootstrapConfigTemplate` on the immutable Vates
  Kube OS image. The node reads a single `vates-node.yaml` document from the
  config drive, so the payload is neither cloud-init nor a machine
  configuration. It is committed here to keep the node-OS flow reproducible and
  debuggable — without it, the only copy lives in someone's local notes — and
  it is not part of the released, tested set.

> The Talos flow also needs the Talos RBAC binding (it is not part of the
> default install): `kubectl apply -k config/rbac/talos`. See [talos/README.md](talos/README.md).

> The `overlays/` directories contain environment-specific UUIDs (and are
> git-ignored) — **not** intended for distribution. Use `base/` for community usage.

## Workflows

> `base/` templates use placeholders and must **not** be edited directly.
> Always create an **overlay** to provide your environment's values — this
> keeps `base/` reusable and avoids committing environment-specific UUIDs.
> See [kubeadm/README.md](kubeadm/README.md) and [talos/README.md](talos/README.md) for the complete file contents
> of each overlay.

## Machine self-healing

When a VM is killed or becomes unresponsive on Xen Orchestra, the node reports
`NotReady`. The infrastructure provider **never recreates VMs on its own** — it
follows the Cluster API `Machine` lifecycle. Replacement is driven by the
**MachineHealthCheck** (MHC) controller, part of core Cluster API (installed by
`clusterctl init`):

- A node stuck in `Ready=False` / `Unknown` for 5 minutes marks its `Machine`
  as unhealthy.
- **Worker** machines → MHC deletes the unhealthy `Machine`, the `MachineSet`
  creates a replacement, which gets a fresh `XOMachine` → new VM.
- **Control-plane** machines → the control plane provider (`KubeadmControlPlane`
  for kubeadm, `TalosControlPlane` for Talos) remediates, preserving quorum.

The templates ship MHC objects for both roles and both bootstrap flows:

| Bootstrap flow | Control plane | Workers |
|---|---|---|
| kubeadm (flat, `clusterctl/`) | `${CLUSTER_NAME}-cp` | `${CLUSTER_NAME}-worker` |
| kubeadm (example-cluster) | `my-cluster-cp` | `my-cluster-worker` |
| talos (flat) | `talos-cluster-cp` | `talos-cluster-worker` |

These are applied automatically when using `kubectl apply -k` (they are part of
the `kustomization.yaml` resources) or `clusterctl generate cluster --from`
(they are included in `cluster-template.yaml`).

### Option A — kubectl apply -k (development)

Deploy the ClusterClass once (it is cluster-scoped), then apply your overlay
which references `base/` and patches the resource IDs:

```bash
# 1. ClusterClass + control plane/bootstrap templates (once)
kubectl apply -k templates/kubeadm/base/clusterclass/

# 2. Create an overlay under templates/kubeadm/overlays/<your-env>/
#    (see kubeadm/README.md), then apply it
kubectl apply -k templates/kubeadm/overlays/my-env/
```

### Option B — clusterctl generate cluster (distribution)

For distribution or one-off clusters, `clusterctl generate cluster` substitutes
the `${...}` variables in a template file and prints the manifests to
pipe into `kubectl apply`. Each bootstrap flow ships its own self-contained
(flat) template under `<flow>/base/clusterctl/`.

**kubeadm** (flat, cloud-init) — two variants: `almalinux-fromscratch.yaml` (installs
kubelet/kubeadm at bootstrap) and `almalinux-prefilled.yaml` (pre-baked template):

```bash
export CP_HOST=<your-cp-vip>
export CP_PORT=6443
export CP_LB=kube-vip
export CP_SUBNET=16
export VM_NAME_PREFIX=my-cluster
export XO_TEMPLATE_UUID=<your-xo-template-uuid>
export XO_POOL_UUID=<your-xo-pool-uuid>
export XO_NETWORK_UUID=<your-xo-network-uuid>

clusterctl generate cluster my-cluster \
  --from templates/kubeadm/base/clusterctl/almalinux-fromscratch.yaml \
  --kubernetes-version v1.36.1 \
  | kubectl apply -f -
```

Use `almalinux-prefilled.yaml` instead for the pre-baked template variant.

**Talos** (flat, immutable OS):

```bash
export CP_VIP=<your-cp-vip>
export CP_SUBNET=16
export VM_NAME_PREFIX=my-cluster
export XO_TEMPLATE_UUID=<your-xo-talos-template-uuid>
export XO_POOL_UUID=<your-xo-pool-uuid>
export XO_NETWORK_UUID=<your-xo-network-uuid>

clusterctl generate cluster my-cluster \
  --from templates/talos/base/clusterctl/cluster-template.yaml \
  --kubernetes-version v1.36.1 \
  | kubectl apply -f -
```

Both template styles embed the control plane and worker `MachineHealthChecks`, so
killed/unhealthy machines are remediated automatically. See the variables
documented at the top of each template, and the per-flow
READMEs ([kubeadm/README.md](kubeadm/README.md), [talos/README.md](talos/README.md)).

> `CONTROL_PLANE_MACHINE_COUNT` / `WORKER_MACHINE_COUNT` default to `1` / `0`
> (clusterctl built-ins). To match the `base/` replicas, pass
> `--control-plane-machine-count 3 --worker-machine-count 2`.

## Customising for your Xen Orchestra environment

Instead of editing `base/`, put your environment-specific values in an overlay
under `templates/kubeadm/overlays/<your-env>/`. The patches override the
following fields:

| Field | Description |
|---|---|
| `spec.template.spec.networkConfig.networks[].networkID` | UUID of the XO network (recommended — direct, dependency-free lookup) |
| `spec.template.spec.networkConfig.networks[].name` | XO network name (alternative — resolved via V1 client if UUID is unknown) |
| `spec.template.spec.templateID` | UUID of the VM template |
| `spec.template.spec.poolID` | UUID of your Xen Orchestra pool |
| `spec.topology.classRef.name` | ClusterClass variant to use (`vates-almalinux-prefilled` or `vates-almalinux-fromscratch`) |
| `spec.topology.variables` | Control plane endpoint, VM name prefix, load balancer, replicas |

These values are placeholders (`<your-xo-network-uuid>`, `<your-xo-template-uuid>`,
`<your-xo-pool-uuid>`) and will differ in your environment.
