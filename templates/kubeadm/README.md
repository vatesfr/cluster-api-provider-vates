# kubeadm CAPI templates

ClusterClass-based templates to deploy an AlmaLinux 10 / RHEL-like Kubernetes
cluster on Xen Orchestra through Cluster API, using the `KubeadmControlPlane` +
cloud-init flow.

This is the default flow. For an immutable OS alternative, see the Talos
templates (`templates/talos/`).

## Layout

```
kubeadm/
├── base/                        # Community templates — placeholders, no resource IDs
│   ├── clusterclass/            # ClusterClass + control plane/worker templates
│   │   ├── rhel-xoclustertemplate.yaml
│   │   ├── prefilled/           # pre-baked image (minimal bootstrap)
│   │   └── from-scratch/        # full bootstrap (dnf install kubelet...)
│   ├── machinetemplates/        # XOMachineTemplate (prefilled / from-scratch)
│   ├── example-cluster/         # Cluster scaffold + CP MachineHealthCheck
│   ├── clusterctl/              # clusterctl-compatible template
│   └── kustomization.yaml
├── overlays/                    # Per-environment values (git-ignored, not distributed)
│   └── <your-env>/              # Your environment — create one, see "Create an overlay"
└── packer/                      # AlmaLinux cloud image builder
```

`base/` is the community artifact: it uses placeholders
(`<your-xo-template-uuid>`, `<your-xo-pool-uuid>`, `<your-xo-network-uuid>`) and
contains **no** environment-specific resource IDs. `overlays/` holds
per-environment overrides and is meant for local testing only. Create your own
overlay directory (e.g. `my-env/`); do **not** edit `base/`.

## Prerequisites

### Management cluster

```bash
CLUSTER_TOPOLOGY=true clusterctl init --bootstrap kubeadm --control-plane kubeadm
```

### VM template

Machines are provisioned via **cloud-init** (NoCloud datasource). The VM
template must have **cloud-init** working and **Xen guest tools** running, and
use one of two variants:

- **`prefilled`** — kubelet, kubeadm, containerd, kube-vip and Cilium images are
  already pre-installed in the template.
- **`from-scratch`** — a minimal template (just containerd + Xen guest tools);
  the ClusterClass installs everything via `preKubeadmCommands`.

Create the template in XO (e.g. import an AlmaLinux 10 cloud image, or build one
with `packer/`). Once ready, note its **UUID** (templateID), your **pool UUID**
(poolID) and your **network UUID**. TemplateID is without PoolID in it and must
be bootable.

## Usage

> `base/` templates use placeholders and must **not** be edited directly.
> Always create an **overlay** to provide your environment's values — this
> keeps `base/` reusable and avoids committing environment-specific UUIDs.

### Create an overlay

An overlay reuses all `base/` resources via kustomize and overrides the
environment-specific values with **patches**. Create a directory for your
environment containing a `kustomization.yaml` that pulls in `base/` plus one
patch file per resource you need to customize.

Create the directory and files:

```bash
mkdir -p templates/kubeadm/overlays/my-env/
```

`kustomization.yaml` — references `base/` and lists the patches
(adjust the target names if you use the `from-scratch` templates):

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ../../base

patches:
  - path: patch-xomachinetemplate-cp.yaml
    target:
      kind: XOMachineTemplate
      name: rhel-cp-prefilled
  - path: patch-xomachinetemplate-worker.yaml
    target:
      kind: XOMachineTemplate
      name: rhel-worker-prefilled
  - path: patch-cluster.yaml
    target:
      kind: Cluster
      name: my-cluster
```

`patch-xomachinetemplate-cp.yaml` — the control plane VM template, pool and
network (**replace the `<your-...-uuid>` with your XO values**):

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachineTemplate
metadata:
  name: rhel-cp-prefilled
spec:
  template:
    spec:
      templateID: <your-xo-template-uuid>
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
  name: rhel-worker-prefilled
spec:
  template:
    spec:
      templateID: <your-xo-template-uuid>
      poolID: <your-xo-pool-uuid>
      networkConfig:
        networks:
          - networkID: <your-xo-network-uuid>
```

`patch-cluster.yaml` — the cluster topology: control plane endpoint, machine
prefix, load balancer, and the number of replicas (**replace the values with
yours**):

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: my-cluster
spec:
  topology:
    classRef:
      name: vates-rhel-prefilled
    version: <your-kubernetes-version>   # e.g. v1.36.1
    variables:
      - name: controlPlaneEndpoint
        value:
          host: <your-control-plane-ip>
          port: 6443
          subnet: <your-subnet-cidr-bits>
      - name: vmNamePrefix
        value: <your-vm-name-prefix>
      - name: controlPlaneLB
        value: kube-vip
    controlPlane:
      replicas: <your-cp-replicas>
    workers:
      machineDeployments:
        - class: worker-prefilled
          name: worker-md-0
          replicas: <your-worker-replicas>
```

If you use the `from-scratch` variant, target `rhel-cp-from-scratch` /
`rhel-worker-from-scratch`, use `classRef: vates-rhel-from-scratch` and
`class: worker-from-scratch`.

Deploy the ClusterClass once (it is cluster-scoped), then apply your overlay:

```bash
kubectl apply -k templates/kubeadm/base/clusterclass/
kubectl apply -k templates/kubeadm/overlays/my-env/
```

> Do **not** apply `templates/kubeadm/base/` directly — it contains no resource
> IDs and would fail. Always go through your overlay.

### Alternative — clusterctl generate

For distribution or one-off clusters, you can use `clusterctl generate` instead
of an overlay:

```bash
export CP_HOST = <your-control-plane-ip>
export CP_PORT=6443
export CP_LB=kube-vip
export CP_SUBNET=<your-subnet-cidr-bits>
export VM_NAME_PREFIX=<your-vm-name-prefix>
export KUBERNETES_VERSION=v1.36.1

clusterctl generate cluster my-cluster \
  --from templates/kubeadm/base/clusterctl/cluster-template.yaml \
  | kubectl apply -f -
```

See `templates/kubeadm/base/clusterctl/cluster-template.yaml` for the full list
of supported variables.

## MachineHealthCheck

The `example-cluster/` includes a control plane `MachineHealthCheck`. When
using an overlay, add the equivalent resource to your overlay so it is applied
alongside your cluster.