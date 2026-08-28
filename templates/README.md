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
- **Containerd pre-installed** (`prefilled` variant) or installed at boot
  (`from-scratch` variant)
- **SSH enabled** — for debugging via `kubectl debug node` or direct access
- **Access to a container registry mirror** (optional but recommended) — the
  bootstrap downloads kubelet/kubeadm from `https://pkgs.k8s.io`

Create this template in XO by importing an AlmaLinux 10 cloud image from
`https://repo.almalinux.org/almalinux/10/cloud/`, or build one with Packer
(see `packer/`). Set `HARBOR_HOST` to your Harbor registry hostname
to configure containerd registry mirrors (optional).

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
│   │   │   ├── rhel-xoclustertemplate.yaml  # shared XOClusterTemplate
│   │   │   ├── from-scratch/            # full bootstrap (dnf install kubelet...)
│   │   │   └── prefilled/               # pre-baked image (minimal bootstrap)
│   │   ├── machinetemplates/            # XOMachineTemplate (edit per environment)
│   │   │   ├── from-scratch/
│   │   │   └── prefilled/
│   │   ├── example-cluster/             # Cluster scaffold + CP MachineHealthCheck
│   │   ├── clusterctl/                  # clusterctl-compatible template (kubeadm)
│   │   └── kustomization.yaml
│   ├── overlays/                        # per-environment values (not distributed)
│   │   └── cogent1/                     # local example — real UUIDs, VIP
│   └── packer/                          # AlmaLinux cloud image builder
├── talos/                               # Talos bootstrap flow
│   ├── base/                            # community templates — placeholders
│   ├── overlays/                        # per-environment values (not distributed)
│   │   └── cogent1/                     # local example — real UUIDs, VIP
│   └── kustomization.yaml
└── README.md
```

Two bootstrap flows are supported, one per top-level directory. Each flow follows
a `base/` + `overlays/` layout:

- **kubeadm** (default): `kubeadm/base/clusterclass/` + `kubeadm/base/machinetemplates/` +
  `kubeadm/base/example-cluster/` with `KubeadmControlPlane` and cloud-init. See below.
- **Talos** : `talos/base/` (community, placeholders) + `talos/overlays/`
  (per-environment values) with `TalosControlPlane` + `TalosConfigTemplate` on
  an immutable, `nocloud` Talos image. See `talos/README.md`.

> The `overlays/` directories contain environment-specific UUIDs (and are
> git-ignored) — **not** intended for distribution. Use `base/` for community usage.

## Workflows

### Option A — kubectl apply -k (development)

Apply the ClusterClass and templates once (they are cluster-scoped):

```bash
# 1. ClusterClass + control plane/bootstrap templates
kubectl apply -k templates/kubeadm/base/clusterclass/

# 2. Machine templates (edit templateID/poolID/network first)
kubectl apply -k templates/kubeadm/base/machinetemplates/
```

Then create a cluster:

```bash
# 3. Customise templates/kubeadm/base/example-cluster/capi-cluster.yaml first
kubectl apply -k templates/kubeadm/base/example-cluster/
```

### Option B — clusterctl generate cluster (distribution)

```bash
export CP_HOST=10.30.139.10
export CP_PORT=6443
export CP_LB=kube-vip
export CP_SUBNET=16
export VM_NAME_PREFIX=my-cluster
export KUBERNETES_VERSION=v1.36.0

clusterctl generate cluster my-cluster \
  --from templates/kubeadm/base/clusterctl/cluster-template.yaml \
  | kubectl apply -f -
```

## Customising for your Xen Orchestra environment

Before applying the machine templates, update the following fields in
`templates/kubeadm/base/machinetemplates/` to match your XO pool:

| Field | Description |
|---|---|
| `spec.template.spec.networkConfig.networks[].networkID` | UUID of the XO network (recommended — direct, dependency-free lookup) |
| `spec.template.spec.networkConfig.networks[].name` | XO network name (alternative — resolved via V1 client if UUID is unknown) |
| `spec.template.spec.templateID` | UUID of the VM template |
| `spec.template.spec.poolID` | UUID of your Xen Orchestra pool |
| `HARBOR_HOST` (env var) | Hostname of your Harbor registry (e.g. `10.30.139.100`). Set before running `make` in `packer/` to enable containerd registry mirrors. Leave unset to pull directly from upstream. |
| `HARBOR_CA_PATH` (env var) | Path to the Harbor CA certificate file (PEM). Required if Harbor uses a self-signed cert. Copy your Harbor CA PEM to a known path and set this variable before running `packer build`. Example: `export HARBOR_CA_PATH=/etc/pki/harbor-ca.crt`. If unset, the script skips CA installation (mirrors still work if Harbor uses a publicly-trusted CA). |

These values are placeholders (`<your-xo-network-uuid>`, `<your-xo-template-uuid>`,
`<your-xo-pool-uuid>`) and will differ in your environment.
