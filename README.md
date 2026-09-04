# vates-capi

A [Cluster API](https://cluster-api.sigs.k8s.io/) infrastructure provider for
[Xen Orchestra](https://xen-orchestra.com/). Manages the full lifecycle of VMs
on XenServer / XCP-ng pools as Kubernetes worker and control plane nodes.

## Quick start

> **Prerequisites:** A management cluster (Kind, k3s, etc.), [clusterctl](https://cluster-api.sigs.k8s.io/clusterctl/overview.html), Xen Orchestra access (VM template UUID, pool UUID, network name).

```bash
# 1. Install CAPI with ClusterClass support
CLUSTER_TOPOLOGY=true clusterctl init --bootstrap kubeadm --control-plane kubeadm

# 2. Deploy the vates provider
kubectl apply -f https://raw.githubusercontent.com/vatesfr/cluster-api-provider-vates/refs/heads/main/dist/install.yaml
# 3. Create the XO credentials secret
kubectl create secret generic xo-credentials -n capi-system \
  --from-literal=url="https://<your-xoa>" \
  --from-literal=token="<your-xo-token>" \
  --from-literal=insecure="true"
```

### ClusterClass and machine templates

All VM templates must have **cloud-init** support enabled and **Xen guest tools**
installed and running. Two ClusterClass variants are provided:

- **`almalinux-prefilled`** — for templates that already have kubelet, kubeadm, containerd,
  kube-vip, and Cilium images pre-installed.
- **`almalinux-fromscratch`** — for minimal templates (just containerd + Xen guest tools).
  The ClusterClass handles installing everything via `preKubeadmCommands`.

Note: TemplateID is without PoolID in it and must be bootable.

Deploy the ClusterClass (once, it is cluster-scoped):

```bash
kubectl apply -k templates/kubeadm/base/clusterclass/
```

The `templates/kubeadm/base/` templates use placeholders and must **not** be
edited directly. Instead, create an **overlay** to hold your environment's
values (real template/pool/network UUIDs, control plane endpoint).

Create a directory for your environment with a `kustomization.yaml` that pulls
in `base/` and patches each resource:

```
templates/kubeadm/overlays/my-env/
├── kustomization.yaml
├── patch-xomachinetemplate-cp.yaml        # templateID, poolID, networkID (CP)
├── patch-xomachinetemplate-worker.yaml    # templateID, poolID, networkID (workers)
└── patch-cluster.yaml                     # control plane endpoint + cluster topology
```

Wherever a YAML contains a placeholder, replace it with your Xen Orchestra
values: template ID, pool UUID, network UUID, control plane IP/port/subnet,
machine prefix, and the number of replicas. Then apply your overlay:

```bash
kubectl apply -k templates/kubeadm/overlays/my-env/
```

See [templates/kubeadm/README.md](templates/kubeadm/README.md) for the complete file contents.

### Installing the provider with clusterctl

`clusterctl` needs to know where the provider lives. Register it in
`~/.config/cluster-api/clusterctl.yaml` (the file clusterctl reads — not
`~/.config/clusterctl/`):

For **local development**, point at the local overrides refreshed by
`make -f Makefile.dev dev-overrides` (regenerates `dist/` and copies the three release assets
into `~/.config/cluster-api/overrides/infrastructure-vates/v0.1.0/`):

```yaml
providers:
  - name: vates
    type: InfrastructureProvider
    url: file://${HOME}/.config/cluster-api/overrides/infrastructure-vates/v0.1.0/infrastructure-components.yaml
```

For a **published release**, point at the GitHub release instead (no repo clone
needed):

```yaml
providers:
  - name: vates
    url: https://github.com/vatesfr/cluster-api-provider-vates/releases/latest/infrastructure-components.yaml
    type: InfrastructureProvider
```

Then:

```bash
clusterctl init --infrastructure vates:v0.1.0
clusterctl generate cluster my-cluster --infrastructure vates:v0.1.0 \
  --control-plane-machine-count 3 --worker-machine-count 2
```

`clusterctl generate cluster` (without `--from`) uses the provider's default
`cluster-template.yaml` — shipped in `dist/` for releases. See
[RELEASING.md](RELEASING.md) for how to publish a release.

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

## Development

```bash
make -f Makefile.dev build    # Build controller image
make -f Makefile.dev push     # Load into Kind
make -f Makefile.dev restart  # Restart the controller pod
```

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
├── dist/                          # Generated release artifacts
│   ├── install.yaml               # kubectl apply bundle
│   ├── infrastructure-components.yaml  # clusterctl bundle
│   ├── cluster-template.yaml      # clusterctl template (kubeadm almalinux-fromscratch)
│   └── chart/                     # Helm chart

```

## Release artifacts

```bash
# Generate dist/ from the current source
make release-manifests IMG=ghcr.io/vatesfr/cluster-api-provider-vates:latest
```

`dist/` contains the three assets required by `clusterctl`:
`infrastructure-components.yaml`, `metadata.yaml` and `cluster-template.yaml`.
See [RELEASING.md](RELEASING.md) for the full release workflow.

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0.
