# Cluster templates

The repository ships ready-made cluster templates under `templates/`, split by
bootstrap flow:

```
templates/
├── README.md                        # overview of both flows
├── kubeadm/                         # kubeadm + cloud-init (default)
│   ├── README.md
│   ├── base/                        # community templates — placeholders
│   │   ├── clusterclass/            # ClusterClass + XOClusterTemplate
│   │   │   ├── almalinux-xoclustertemplate.yaml
│   │   │   ├── almalinux-fromscratch/
│   │   │   └── almalinux-prefilled/
│   │   ├── machinetemplates/        # XOMachineTemplate (control plane / worker)
│   │   │   ├── almalinux-fromscratch/
│   │   │   └── almalinux-prefilled/
│   │   ├── example-cluster/         # Cluster scaffold + MachineHealthChecks
│   │   ├── clusterctl/              # flat templates for `clusterctl generate`
│   │   └── kustomization.yaml
│   ├── overlays/                    # per-environment values (create your own)
│   └── packer/                      # AlmaLinux cloud image builder
└── talos/                           # Talos (immutable OS)
    ├── README.md
    ├── base/
    │   ├── machinetemplates/        # XOMachineTemplate (control plane / worker)
    │   ├── example-cluster/         # Cluster scaffold + MachineHealthChecks
    │   ├── clusterctl/              # flat templates for `clusterctl generate`
    │   └── kustomization.yaml
    ├── components/                  # optional Cilium kustomize components
    └── kustomization.yaml           # wrapper that points at base/
```

The two top-level directories are the two **bootstrap flows**. They share the
infrastructure provider; they differ in OS, bootstrap provider, and control
plane provider:

| Flow | OS / bootstrap | Control plane | Managed topology |
|---|---|---|---|
| `kubeadm/` (default) | AlmaLinux + `KubeadmConfig` (cloud-init) | `KubeadmControlPlane` | yes — ships a `ClusterClass` |
| `talos/` | Talos `nocloud` + `TalosConfig` | `TalosControlPlane` | no — flat templates |

The rest of this page covers the layout both flows share, then one worked
overlay per flow. For the field-level detail of the objects involved, see
[Custom resources](crds/index.md) and [Bootstrap payload](bootstrap.md).

## The `base/` + `overlays/` convention

Every flow follows the same split, whether or not it ships a `ClusterClass`:

- **`base/`** is the *community artifact*. It ships **placeholders**
  (`<your-xo-template-uuid>`, `<your-xo-pool-uuid>`, `<your-xo-network-uuid>`,
  `<your-cp-vip>`) and contains **no** environment-specific resource IDs, so the
  same files work for everyone.
- **`overlays/`** holds one sub-directory per environment. An overlay is a
  small `kustomization.yaml` that pulls in `base/` and *patches* the
  placeholders with your values.

!!! warning "Never edit `base/` directly"

    `base/` is meant to be reusable and distributable. If you write your UUIDs
    into it, you can no longer pull updates cleanly, and you risk committing
    environment-specific IDs. Always go through an overlay. The `overlays/`
    directories are git-ignored for exactly this reason.

`kubectl apply -k templates/kubeadm/base/` is therefore **not** a valid
deployment target on its own: the placeholders are not real IDs. Apply your
overlay instead.

## Option A — overlay with `kubectl apply -k`

This is the development flow. The `ClusterClass` is cluster-scoped and applied
once; the overlay carries the per-environment values.

```bash
# 1. ClusterClass + control plane/bootstrap templates (once)
kubectl apply -k templates/kubeadm/base/clusterclass/

# 2. Your overlay (created below)
kubectl apply -k templates/kubeadm/overlays/my-env/
```

!!! note "The `ClusterTopology` feature gate"

    Managed topologies (the `ClusterClass` flow) require the
    `ClusterTopology` feature gate on the management cluster. `clusterctl init`
    disables it by default, so export it *before* initializing:

    ```bash
    export CLUSTER_TOPOLOGY=true
    ```

    The flat `clusterctl generate` flow below does not need it.

## Option B — `clusterctl generate cluster`

`clusterctl generate cluster` reads a template file, substitutes `${...}`
variables, and prints the rendered manifests. Pipe them into `kubectl apply`.
Each flow ships self-contained (**flat**) templates under
`base/clusterctl/`: a `Cluster`, `XOCluster`, the machine templates, the control
plane and bootstrap objects, the `MachineDeployment`, and the
`MachineHealthChecks` — no `ClusterClass` is installed first.

```bash
export CP_HOST=10.30.139.10
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

Two variable families are substituted:

- **Your variables**, declared at the top of each template: `CP_HOST`,
  `CP_PORT`, `CP_LB`, `CP_SUBNET`, `VM_NAME_PREFIX`, and the XO UUIDs above.
  The Talos templates use `CP_VIP` / `TALOS_VERSION` instead of `CP_HOST` /
  `CP_LB`.
- **clusterctl built-ins**, provided by the command: `CLUSTER_NAME` (the name
  argument), `NAMESPACE`, `KUBERNETES_VERSION` (`--kubernetes-version`),
  `CONTROL_PLANE_MACHINE_COUNT` (`--control-plane-machine-count`) and
  `WORKER_MACHINE_COUNT` (`--worker-machine-count`).

!!! tip "Machine counts default to 1 and 0"

    `CONTROL_PLANE_MACHINE_COUNT` / `WORKER_MACHINE_COUNT` default to `1` / `0`,
    not to the `base/` replicas (3 control plane, 2 workers). Pass the flags to
    match:

    ```bash
    clusterctl generate cluster my-cluster \
      --from templates/kubeadm/base/clusterctl/almalinux-fromscratch.yaml \
      --kubernetes-version v1.36.1 \
      --control-plane-machine-count 3 --worker-machine-count 2 \
      | kubectl apply -f -
    ```

The Talos flow uses the same command with `templates/talos/base/clusterctl/cluster-template.yaml` and the `CP_VIP` / `TALOS_VERSION`
variables.

## The default `dist/cluster-template.yaml`

The release bundles a default template at **`dist/cluster-template.yaml`**. It
is the kubeadm `almalinux-fromscratch` flat template — the one `clusterctl`
would fetch as the provider's default `cluster-template.yaml` — with the same
variables as Option B:

```bash
clusterctl generate cluster my-cluster \
  --from dist/cluster-template.yaml \
  --kubernetes-version v1.36.1 \
  --control-plane-machine-count 3 --worker-machine-count 2 \
  | kubectl apply -f -
```

`dist/` also holds `install.yaml` (the `kubectl apply` bundle for the provider
itself) and `infrastructure-components.yaml` (the `clusterctl` bundle). Only
`cluster-template.yaml` is a *cluster* template; the other two install the
provider.

## Worked example — kubeadm overlay

Create the overlay directory and three patch files, plus a `kustomization.yaml`
that pulls in `base/`. The patches override one resource each.

```bash
mkdir -p templates/kubeadm/overlays/my-env/
```

`kustomization.yaml` — the patches target resources by kind and name:

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

`patch-xomachinetemplate-cp.yaml` — the control plane VM template, pool, and
network. Replace the `<your-...-uuid>` placeholders with your XO values (and
repeat the same values for the worker patch):

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

`patch-cluster.yaml` — the managed topology: control plane endpoint, VM name
prefix, load balancer, and replica counts:

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
          subnet: 16
      - name: vmNamePrefix
        value: my-cluster
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

Apply the class once, then the overlay:

```bash
kubectl apply -k templates/kubeadm/base/clusterclass/
kubectl apply -k templates/kubeadm/overlays/my-env/
```

If you use the `almalinux-fromscratch` variant, target
`almalinux-cp-fromscratch` / `almalinux-worker-fromscratch`, set
`classRef: vates-almalinux-fromscratch`, and use
`class: worker-almalinux-fromscratch`.

## Worked example — Talos overlay

Talos has no `ClusterClass`, so its overlay patches the flat
`TalosControlPlane` and `XOCluster` directly. Create the directory and the
files:

```bash
mkdir -p templates/talos/overlays/my-env/
```

`kustomization.yaml` — patches each resource, and optionally pulls in a Cilium
component:

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

# components:
#   - ../../components/cilium                          # standard mode
#   - ../../components/cilium-kube-proxy-replacement   # kube-proxy replacement
```

`patch-xomachinetemplate-cp.yaml` — the Talos VM template, pool, and network:

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

`patch-controlplane.yaml` — the native Talos control plane VIP and the kubelet
override that hands node initialization to the CCM:

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1alpha3
kind: TalosControlPlane
metadata:
  name: talos-cp
spec:
  controlPlaneConfig:
    controlplane:
      talosVersion: v1.13.9           # must match the image
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
          kubelet:
            extraArgs:
              cloud-provider: external
```

`patch-xocluster.yaml` — the cluster's control plane endpoint:

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: talos-cluster
spec:
  controlPlaneEndpoint:
    host: <your-cp-vip>
    port: 6443
    subnet: 16
```

Apply it:

```bash
kubectl apply -k templates/talos/overlays/my-env/
```

!!! warning "The Talos flow needs its RBAC binding"

    The Talos control plane (CACPPT) and bootstrap (CABPT) providers need access
    to `XOMachineTemplate`, which is not granted by the default install. Apply
    the binding too:

    ```bash
    kubectl apply -k config/rbac/talos
    ```

    Without it, no control plane `Machine` is created. See
    [XOMachineTemplate](crds/xomachinetemplate.md#rbac-for-the-talos-control-plane).

`TalosControlPlane.spec.controlPlaneConfig.controlplane.talosVersion` **must**
match the Talos version of the VM image; otherwise the node rejects the config.
See `templates/talos/README.md` for the full overlay, the Cilium components,
and the image prerequisites.

## Where next

- The objects the templates stamp out: [XOMachine](crds/xomachine.md),
  [XOMachineTemplate](crds/xomachinetemplate.md),
  [XOCluster](crds/xocluster.md), [XOClusterTemplate](crds/xoclustertemplate.md).
- Managed topologies in depth: [ClusterClass](../guides/clusterclass.md).
- The `clusterctl` distribution and `metadata.yaml`: `templates/README.md`,
  `templates/kubeadm/README.md`, `templates/talos/README.md`.
