# kubeadm

The kubeadm flow is the default way to build clusters with vates-capi. The VMs
boot a cloud-init capable OS, and the kubeadm bootstrap provider hands them the
configuration that turns them into control plane or worker nodes.

This page covers the moving parts: the cloud-init lifecycle, the two AlmaLinux
machine images, generating a cluster from the `clusterctl` template, the `base/`
+ `overlays/` layout, version pinning, and building the image with Packer. For
the concepts underneath — providers, the bootstrap `Secret`, the reference graph
— read [How they fit together](../../concept/how-they-link.md) first.

## The cloud-init lifecycle, step by step

Cloud-init is the seam between the providers. Nothing calls anything directly:
every step is either an object that appears in the management cluster or a file
written into the VM's config drive.

1. You apply a `Cluster`, a `KubeadmControlPlane` and a `MachineDeployment`.
2. Cluster API creates the worker `Machine`s; the control plane provider
   (KCP) creates the control plane ones.
3. For each `Machine`, the kubeadm bootstrap provider writes a **bootstrap
   `Secret`** holding the cloud-init document — the `kubeadm init` or
   `kubeadm join` payload.
4. vates-capi sees the `Machine`, creates an `XOMachine`, and reads that
   `Secret`.
5. It renders the payload as cloud-init **user-data**, plus the network config,
   and hands both to Xen Orchestra as the VM's config drive.
6. Xen Orchestra clones `templateID` into a new VM, sizes it from
   `resourceSet`, attaches the interfaces in `networkConfig.networks`, and
   starts it.
7. The guest boots, cloud-init runs, and `kubeadm` joins the node to the
   cluster.
8. The provider reads the VM's addresses through the Xen guest tools and
   publishes them on `XOMachine.status.addresses`, along with the `providerID`
   `xenorchestra://<pool-uuid>/<vm-uuid>`.

Step 5 only happens because the machine templates **declare** the payload as
cloud-init:

```yaml
spec:
  template:
    spec:
      bootstrap:
        format: cloudinit
```

With that declaration set, the provider may also enrich the payload: it merges
the Xen Orchestra user's SSH keys when `XOCluster.spec.injectSSHKeys` is `true`,
and injects kube-vip when the `XOCluster` sets `controlPlaneLB: kube-vip`. Keep
the declaration when you copy or derive a template — with it unset (or set to
`passthrough`), the payload is delivered untouched and neither enrichment runs.
See [Bootstrap payload](../../reference/bootstrap.md) for the full semantics.

## Choosing a machine image

Machines are provisioned through the **NoCloud** datasource, so the VM template
must be cloud-init capable regardless of the variant. The repository ships
templates for two AlmaLinux 10 variants:

| Variant | Template content | Bootstrap |
|---|---|---|
| `almalinux-fromscratch` | Minimal: containerd and Xen guest tools | Full — `kubelet`, `kubeadm`, `kubectl` installed at bootstrap via `preKubeadmCommands` |
| `almalinux-prefilled` | kubelet, kubeadm, containerd, kube-vip and Cilium images pre-installed | Minimal — the image already has everything |

`almalinux-fromscratch` keeps the template small and lets the cluster's
requested Kubernetes version decide what gets installed. `almalinux-prefilled`
boots faster because nothing is downloaded, at the cost of baking a fixed
version into the image.

Whatever you build, the template must be:

- **Bootable**, with the OS already installed — not a bare ISO.
- **Cloud-init enabled** (NoCloud datasource). A template that ignores
  cloud-init boots and then sits there doing nothing.
- **Running Xen guest tools.** The provider reads the VM's addresses through the
  guest agent; without it the machine never reports an address and never joins.
- **Not tied to a specific pool**, so it can be cloned wherever `poolID` points.

Once the template exists in XO, note three identifiers: its **UUID**
(`templateID`, without any pool ID in it), your **pool UUID** (`poolID`) and
your **network UUID** (`networkID`).

### The Kubernetes version contract

The cluster's Kubernetes version must be exactly the version installed on the
nodes. If it is not, KCP treats the `Machine`s as out of date and rolls them
forever. The two variants keep that promise differently:

- **from-scratch** — `preKubeadmCommands` read `KUBERNETES_VERSION` and pin it:
  `kubelet-<version>`, `kubeadm-<version>` and `kubectl-<version>` are installed
  from `pkgs.k8s.io`. Requesting `vX.Y.Z` installs `X.Y.Z`; bootstrap fails if
  that patch is not available in the repository.
- **prefilled** — the image ships a version baked at build time. Build the image
  for the target version (`make K8S_VERSION=X.Y.Z k8s` in `packer/`); a cluster
  requesting any other version fails fast at bootstrap.

## Generate a cluster from the clusterctl template

For a one-off cluster or for distribution, `clusterctl generate cluster` renders
the self-contained flat templates under `templates/kubeadm/base/clusterctl/`.
They inline the `Cluster`, `XOCluster`, `XOMachineTemplate`,
`KubeadmControlPlane`, `KubeadmConfigTemplate`, `MachineDeployment` and
MachineHealthChecks, so **no ClusterClass needs to be installed first**.

Describe your environment through the template's variables, then generate and
apply in one pipe:

```bash
export CP_HOST=<your-control-plane-ip>       # control plane VIP
export CP_PORT=6443
export CP_LB=kube-vip                        # or "" for no load balancer
export CP_SUBNET=<your-subnet-cidr-bits>     # e.g. 16
export XO_TEMPLATE_UUID=<your-xo-template-uuid>
export XO_POOL_UUID=<your-xo-pool-uuid>
export XO_NETWORK_UUID=<your-xo-network-uuid>

clusterctl generate cluster my-cluster \
  --from templates/kubeadm/base/clusterctl/almalinux-fromscratch.yaml \
  --kubernetes-version v1.36.1 \
  --control-plane-machine-count 3 --worker-machine-count 2 \
  | kubectl apply -f -
```

For the pre-baked image, use the `almalinux-prefilled.yaml` template instead.
`XO_CNI` selects the CNI the provider installs via `ClusterResourceSet`
(`cilium` by default; set `none` to install one yourself). See the variable
header at the top of each template for the full list.

!!! tip "The published default template"

    `clusterctl generate cluster` **without** `--from` uses the provider's
    default `cluster-template.yaml`, shipped in `dist/`. It is the
    `almalinux-fromscratch` variant:

    ```bash
    clusterctl generate cluster my-cluster --infrastructure vates \
      --control-plane-machine-count 3 --worker-machine-count 2 \
      | kubectl apply -f -
    ```

## The `base/` + `overlays/` layout

The ClusterClass flow is a second way to build the same cluster, meant to be
applied with kustomize and then managed as a **managed topology**. It is split
into a reusable community artifact and your own environment values:

```
templates/kubeadm/
├── base/                        # community templates — placeholders, no IDs
│   ├── clusterclass/            # ClusterClass + control plane/worker templates
│   ├── machinetemplates/        # XOMachineTemplate (prefilled / fromscratch)
│   ├── example-cluster/         # Cluster scaffold + MachineHealthChecks
│   ├── clusterctl/              # clusterctl-compatible flat templates
│   └── kustomization.yaml
├── overlays/                    # per-environment values (git-ignored)
│   └── <your-env>/              # you create this
└── packer/                      # AlmaLinux cloud image builder
```

`base/` is the community artifact. It uses placeholders
(`<your-xo-template-uuid>`, `<your-xo-pool-uuid>`, `<your-xo-network-uuid>`) and
contains **no** environment-specific resource IDs. `overlays/` holds
per-environment overrides and is meant for local testing only. Never edit
`base/`; create your own overlay directory instead.

!!! warning "The ClusterClass gate"

    Managed topologies require the `ClusterTopology` feature gate, which
    `clusterctl init` **disables by default**. Export it *before* initializing,
    or the management cluster rejects every `ClusterClass` and `...Template`
    with `spec: Forbidden: can be set only if the ClusterTopology feature flag
    is enabled`:

    ```bash
    export CLUSTER_TOPOLOGY=true
    clusterctl init --bootstrap kubeadm --control-plane kubeadm --infrastructure vates
    ```

    The flat `clusterctl generate` flow does not need the gate.

A `ClusterClass` references its `XOMachineTemplate`s by name. Those live in
`templates/kubeadm/base/machinetemplates/`, a **separate** kustomization from
`base/clusterclass/`, so apply both — or the whole `base/`, which includes them.

### Create an overlay

An overlay pulls in all of `base/` through kustomize and overrides the
environment-specific values with patches. Create a directory for your
environment with a `kustomization.yaml` that lists one patch per resource:

```bash
mkdir -p templates/kubeadm/overlays/my-env/
```

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ../../base

patches:
  - path: patch-xomachinetemplate-cp.yaml
    target:
      kind: XOMachineTemplate
      name: almalinux-cp-prefilled
  - path: patch-xomachinetemplate-worker.yaml
    target:
      kind: XOMachineTemplate
      name: almalinux-worker-prefilled
  - path: patch-cluster.yaml
    target:
      kind: Cluster
      name: my-cluster
```

The two machine-template patches carry the VM template, pool and network
(replace the `<your-...-uuid>` with your XO values):

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOMachineTemplate
metadata:
  name: almalinux-cp-prefilled
spec:
  template:
    spec:
      templateID: <your-xo-template-uuid>
      poolID: <your-xo-pool-uuid>
      networkConfig:
        networks:
          - networkID: <your-xo-network-uuid>
```

The cluster patch sets the topology: the control plane endpoint, the load
balancer, and the replica counts.

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: my-cluster
spec:
  topology:
    classRef:
      name: vates-almalinux-prefilled
    version: v1.36.1
    variables:
      - name: controlPlaneEndpoint
        value:
          host: <your-control-plane-ip>
          port: 6443
          subnet: <your-subnet-cidr-bits>
      - name: controlPlaneLB
        value: kube-vip
    controlPlane:
      replicas: 3
    workers:
      machineDeployments:
        - class: worker-almalinux-prefilled
          name: worker-md-0
          replicas: 2
```

For the `almalinux-fromscratch` variant, target `almalinux-cp-fromscratch` /
`almalinux-worker-fromscratch`, use `classRef: vates-almalinux-fromscratch` and
`class: worker-almalinux-fromscratch`.

The `ClusterClass` is cluster-scoped: install it once, then apply the overlay:

```bash
kubectl apply -k templates/kubeadm/base/clusterclass/
kubectl apply -k templates/kubeadm/overlays/my-env/
```

The `example-cluster/` also ships a control plane `MachineHealthCheck`; add the
equivalent resource to your overlay so it is applied with your cluster.

!!! warning "Do not apply `base/` directly"

    `base/` contains no resource IDs and would fail. Always go through your
    overlay.

## Version pinning

Two versions matter, and they are the same number:

- **Kubernetes** — the `Cluster.spec.topology.version` (ClusterClass flow) or
  the `--kubernetes-version` flag (clusterctl flow), which KCP copies onto every
  `Machine`. It also feeds `KUBERNETES_VERSION` into the from-scratch
  `preKubeadmCommands`.
- **kubeadm** — there is no separate knob. The from-scratch bootstrap installs
  `kubeadm-<version>` and `kubectl-<version>` at the same version as the
  kubelet, derived from `KUBERNETES_VERSION`. The prefilled image bakes the same
  three binaries at build time via `make K8S_VERSION=X.Y.Z k8s`.

Pick the patch release you actually want and keep it consistent: a node whose
installed kubelet does not match the `Machine` version is rolled by KCP
indefinitely. With the prefilled image, the build-time version and the
cluster's version must match exactly; with from-scratch, the requested patch
must exist in `pkgs.k8s.io`.

## Building the AlmaLinux image with Packer

The `templates/kubeadm/packer/` directory builds an AlmaLinux 10 template with
containerd, kubelet, kubeadm and kubectl pre-installed — the input for the
`almalinux-prefilled` variant.

Requirements:

- [Packer](https://www.packer.io/) (tested with 1.9+)
- QEMU (`qemu-img`, `qemu-system-x86_64`)
- An AlmaLinux 10 cloud image, downloaded automatically by Packer

Build a qcow2 image, then convert it to VHD for import into Xen Orchestra:

```bash
cd templates/kubeadm/packer/

make k8s       # build the qcow2 image
make vhd       # convert it to VHD for XenServer import
make clean     # remove build artifacts
```

The Kubernetes version defaults to `1.36.1` and is overridden with
`K8S_VERSION`. It **must** match the `KUBERNETES_VERSION` of any cluster using
the prefilled template:

```bash
make K8S_VERSION=1.36.1 k8s
```

Output lands in `output-almalinux10-k8s/`. The build runs
`scripts/install-k8s.sh` and pulls everything from the upstream registries.

Import the resulting disk into XO as a **bootable template** with cloud-init
enabled and Xen guest tools running, then use its UUID as `templateID`.

## Where next

- Shape future machines and roll changes: [ClusterClass](../clusterclass.md).
- The floating control plane endpoint: [VIP](../vip.md).
- Stable node addresses from a pool: [IPAM](../ipam.md).
- The non-cloud-init alternative: [Talos](talos/index.md).
