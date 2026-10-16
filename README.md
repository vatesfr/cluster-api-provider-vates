# vates-capi

[![Documentation Status](https://readthedocs.org/projects/vates-capi/badge/?version=latest)](https://vates-capi.readthedocs.io/en/latest/?badge=latest)

A [Cluster API](https://cluster-api.sigs.k8s.io/) infrastructure provider for
[Xen Orchestra](https://xen-orchestra.com/). Manages the full lifecycle of VMs
on XenServer / XCP-ng pools as Kubernetes worker and control plane nodes.

## Quick start

> **Prerequisites:** A management cluster (Kind, k3s, etc.), [clusterctl](https://cluster-api.sigs.k8s.io/clusterctl/overview.html), Xen Orchestra access (VM template UUID, pool UUID, network name).

### 1. Install the providers

Create the XO credentials secret **first**: the vates provider reads it at
startup and exits if it is missing. The secret lives in the provider namespace
(`capi-system`), so create that namespace beforehand.

```bash
# 1. Create the provider namespace and the XO credentials secret
kubectl create namespace capi-system
kubectl create secret generic xo-credentials -n capi-system \
  --from-literal=url="https://<your-xoa>" \
  --from-literal=token="<your-xo-token>" \
  --from-literal=insecure="true"

# 2. Install CAPI core + the kubeadm bootstrap/control-plane providers
clusterctl init --bootstrap kubeadm --control-plane kubeadm

# 3. Deploy the vates infrastructure provider
kubectl apply -f https://raw.githubusercontent.com/vatesfr/cluster-api-provider-vates/refs/heads/main/dist/install.yaml
```

### 2. Create a cluster

All VM templates must have **cloud-init** support enabled and **Xen guest tools**
installed and running. Export your Xen Orchestra values and generate the cluster
from the kubeadm template:

```bash
export CP_HOST=<your-cp-vip>         # control plane VIP
export CP_PORT=6443
export CP_LB=kube-vip
export CP_SUBNET=16
export VM_NAME_PREFIX=my-cluster
export XO_TEMPLATE_UUID=<your-vm-template-uuid>   # no pool ID, must be bootable
export XO_POOL_UUID=<your-pool-uuid>
export XO_NETWORK_UUID=<your-network-uuid>

clusterctl generate cluster my-cluster \
  --from templates/kubeadm/base/clusterctl/almalinux-fromscratch.yaml \
  --kubernetes-version v1.36.1 \
  --control-plane-machine-count 3 --worker-machine-count 2 | kubectl apply -f -
```

### 3. ClusterClass (optional)

The kubeadm flow also ships ClusterClass-based variants (managed topologies,
`almalinux-prefilled` / `almalinux-fromscratch`) used through the `base/` +
`overlays/` layout. This requires ClusterTopology enabled and is an alternative
to the flat template above. See
[templates/kubeadm/README.md](templates/kubeadm/README.md) for the full file
contents.

### Installing the provider with clusterctl

`clusterctl` needs to know where the provider lives, via
`~/.config/cluster-api/clusterctl.yaml` (the file clusterctl reads — not
`~/.config/clusterctl/`).

Register the published release (no repo clone needed):

```yaml
providers:
  - name: vates
    type: InfrastructureProvider
    url: https://github.com/vatesfr/cluster-api-provider-vates/releases/latest/infrastructure-components.yaml
```

Then create the XO credentials secret (the vates provider reads it at startup)
and initialize the providers:

```bash
kubectl create namespace capi-system
kubectl create secret generic xo-credentials -n capi-system \
  --from-literal=url="https://<your-xoa>" \
  --from-literal=token="<your-xo-token>" \
  --from-literal=insecure="true"

clusterctl init --infrastructure vates
clusterctl generate cluster my-cluster --infrastructure vates \
  --control-plane-machine-count 3 --worker-machine-count 2
```

Without a version, `clusterctl` installs the **latest** release; append `:vX.Y.Z`
to pin one. For **local development** (running the provider from source against
a local overrides layer), see
[docs/contributing/development.md](docs/contributing/development.md).

`clusterctl generate cluster` (without `--from`) uses the provider's default
`cluster-template.yaml`, shipped as a release asset. See
[Releasing](docs/operations/releasing.md) for how to publish a release.

## Using with Talos

The vates provider also supports [Talos Linux](https://www.talos.dev/) as an
immutable, cloud-init-free alternative to kubeadm. The `TalosControlPlane` and
`TalosConfig` CRDs come from the Talos bootstrap / control plane providers
(CABPT / CACPPT). Install them alongside the vates provider:

```bash
clusterctl init --bootstrap talos --control-plane talos
```

> `dist/install.yaml` deploys the vates provider but **not** CAPI or the Talos
> CRDs. If you install the vates provider this way, you must still run the
> `clusterctl init` above for `TalosControlPlane` / `TalosConfig` to exist.
> See [templates/talos/README.md](templates/talos/README.md) for both installation options.

The default vates RBAC only binds the kubeadm control plane (KCP). For the Talos
flow, grant the Talos providers (CACPPT / CABPT) access to the `XOMachineTemplate`
resources:

```bash
kubectl apply -k config/rbac/talos
```

Prerequisites: a Talos VM template built for the **`nocloud`** platform with
the `siderolabs/xen-guest-agent` extension, **never booted**, and with
**`viridian: false`** in XO.

> Set `TalosControlPlane.spec.controlPlaneConfig.controlplane.talosVersion` to
> the Talos version of the image (the clusterctl template reads it from
> `TALOS_VERSION`). Without it, CABPT generates a config for its own default
> version, which the node rejects — the machine stays in maintenance.

The `templates/talos/base/` templates use placeholders and must **not** be
edited directly. Instead, create an **overlay** to hold your environment's
values (real UUIDs, VIP address, etc.).

Create a directory for your environment with a `kustomization.yaml` that pulls
in `base/` and patches each resource with your values:

```
templates/talos/overlays/my-env/
├── kustomization.yaml
├── patch-xomachinetemplate-cp.yaml        # templateID, poolID, networkID
├── patch-xomachinetemplate-worker.yaml    # templateID, poolID, networkID
├── patch-controlplane.yaml                # control plane VIP + machine config
└── patch-xocluster.yaml                   # control plane endpoint
```

Wherever a YAML contains a `<your-...-uuid>` or `<your-cp-vip>` placeholder,
replace it with your Xen Orchestra template ID, pool UUID, network UUID, and
control plane VIP. Then apply:

```bash
kubectl apply -k templates/talos/overlays/my-env/
```

See [templates/talos/README.md](templates/talos/README.md) for the complete file contents.

## Bootstrap behavior

The provider never identifies the bootstrap provider: what to do with the
bootstrap payload is **declared** on each `XOMachine` / `XOMachineTemplate`
under `spec.bootstrap`.

| Field | Effect |
|---|---|
| `cloudInit` | treat the payload as cloud-init: allows merging the XO user's SSH keys (only when the `XOCluster` sets `injectSSHKeys: true`) |
| `kubeVIP` | allows injecting kube-vip (only when the `XOCluster` sets `controlPlaneLB: kube-vip`) |

With **no** `spec.bootstrap`, the payload is passed through untouched. The
shipped templates set it for kubeadm; the Talos flow needs none. Adding a new
bootstrap provider does not require any change to this controller — only the
declaration in its `XOMachineTemplate`.

## Development

Contributors building from source (build/push loop for kind and k3s,
clusterctl overrides, tests, debugging): see
[Development](docs/contributing/development.md).

## Project structure

```
├── api/                           # CRD types (+kubebuilder markers)
├── cmd/                           # Manager entry point
├── internal/
│   ├── bootstrap/                 # Bootstrap providers (kubeadm, talos)
│   ├── controller/                # Reconciliation logic
│   └── kubevip/                   # kube-vip static pod injection
├── config/
│   ├── crd/                       # Generated CRDs (DO NOT EDIT)
│   ├── manager/                   # Manager Deployment
│   ├── rbac/                      # RBAC: role.yaml generated (DO NOT EDIT), bindings/ + talos/ hand-written
├── templates/
│   ├── kubeadm/                   # kubeadm cluster templates
│   │   ├── base/                  # ClusterClass + machinetemplates (placeholders)
│   │   ├── overlays/              # per-environment values (create one)
│   │   ├── clusterctl/            # Template for clusterctl generate
│   │   └── packer/                # AlmaLinux cloud image builder
│   ├── talos/                     # Talos cluster templates (base + overlays)
│   └── README.md                  # Template usage guide
├── examples/                      # Example manifests (kind)
├── dist/                          # Distribution artifacts
│   ├── install.yaml               # kubectl apply bundle (committed)
│   └── chart/                     # Helm chart (committed)
│                                  # clusterctl assets are generated at release time

```

## Release artifacts

```bash
# Generate dist/ from the current source
make release-manifests IMG=ghcr.io/vatesfr/cluster-api-provider-vates:latest
```

`make release-manifests` writes the three assets `clusterctl` needs —
`infrastructure-components.yaml`, `metadata.yaml` and `cluster-template.yaml` —
into `dist/`. They are generated (git-ignored) and attached to the release by
CI. See [Releasing](docs/operations/releasing.md) for the full release workflow.

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0.
