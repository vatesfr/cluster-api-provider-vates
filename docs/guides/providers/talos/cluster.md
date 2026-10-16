# Building a Talos cluster

An end-to-end walkthrough for a Talos-backed cluster on Xen Orchestra.

It assumes the two prior steps are done: a [Talos VM template prepared
correctly](prerequisites.md), and the [Talos providers and RBAC
installed](installation.md). If either is missing, a machine will boot and then
sit in maintenance — the trap at the end of this page.

## The `templates/talos/base/` layout

Talos has no ClusterClass: the Talos control plane provider (CACPPT) does not
support managed topologies yet, so the templates are **flat** — a
`TalosControlPlane` plus a `MachineDeployment`:

```
templates/talos/
├── base/                 # community templates — placeholders, no IDs
│   ├── machinetemplates/ # XOMachineTemplate (control plane / worker)
│   ├── example-cluster/  # Cluster, TalosControlPlane, TalosConfigTemplate,
│   │                     # MachineDeployment, XOCluster, MachineHealthChecks
│   ├── clusterctl/       # clusterctl-compatible templates
│   └── kustomization.yaml
├── components/           # optional kustomize components (Cilium modes)
└── overlays/             # per-environment values (git-ignored)
    └── <your-env>/       # you create this
```

`base/` is the community artifact. Every value that is environment-specific is a
placeholder — `<your-xo-talos-template-uuid>`, `<your-xo-pool-uuid>`,
`<your-xo-network-uuid>`, `<your-cp-vip>`, `<your-talos-version>` — and no real
resource ID appears anywhere. That is what makes it shareable. **Never edit
`base/`**; put your values in an overlay. Editing `base/` is how you end up
committing a pool UUID and having a future `git pull` overwrite your changes.

`overlays/` is git-ignored and meant for local testing only.

## Create an overlay

An overlay pulls in all of `base/` through kustomize and patches the resources
that need your values. Create a directory and a `kustomization.yaml`:

```bash
mkdir -p templates/talos/overlays/my-env/
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

Four patches, one per thing that varies between environments. Wherever a `base/`
file contains a `<your-...>` placeholder, one of these four patches supplies the
real value.

## The four patch files

### 1. `patch-xomachinetemplate-cp.yaml` — control plane VM template

The XO template, pool and network the control plane VMs clone from:

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

### 2. `patch-xomachinetemplate-worker.yaml` — worker VM template

The same three values for the worker machines:

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

### 3. `patch-controlplane.yaml` — VIP and machine config

The control plane endpoint, the Talos **shared IP** (the VIP) and the machine
configuration patches. Talos provides this VIP **natively** — there is no
kube-vip here, unlike the [kubeadm](../kubeadm.md) and
[Vates Kube OS](../vates-kube-os.md) flows (see [VIP](../../vip.md)).
The `strategicPatches` are applied to every control plane node's Talos config:

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

Three things are going on here:

- `talosVersion` must be the Talos version of the image (see the
  [maintenance-mode trap](#the-maintenance-mode-trap)).
- `vip.ip` is Talos' **native** shared IP, not kube-vip. It must equal
  `XOCluster.spec.controlPlaneEndpoint.host` in the fourth patch.
- The kubelet settings live in their own `KubeletConfig` document.
  **Since Talos 1.12 the kubelet is a standalone document**: patching
  `machine.kubelet` here is rejected at boot with
  `kubelet config is already set in v1alpha1 config`, and the node stops before
  starting any service.

The worker's `TalosConfigTemplate` in `base/` carries the time servers and the
same standalone `KubeletConfig` patch, so workers and control plane nodes agree
on the CCM (`cloud-provider: external`).

### 4. `patch-xocluster.yaml` — control plane endpoint

The cluster-level infrastructure object. Note there is **no** `controlPlaneLB`
(Talos manages the VIP natively) and no `injectSSHKeys` (Talos is immutable and
has no SSH):

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

## Apply the overlay

```bash
kubectl apply -k templates/talos/overlays/my-env/
```

This creates the `Cluster`, the `XOCluster`, the `TalosControlPlane`, the
`TalosConfigTemplate`, both `XOMachineTemplate`s, the `MachineDeployment` and
the two `MachineHealthCheck`s. Do **not** apply `templates/talos/base/` on its
own — it is full of placeholders and would fail.

## Watch it come up

Following the resources is the fastest way to see where a cluster is stuck:

```bash
kubectl get cluster,taloscontrolplane,machinedeployment,xomachine -w
```

What you should see, in order:

1. The `XOCluster` becomes ready and publishes the control plane endpoint.
2. `TalosControlPlane` creates the control plane `Machine`s.
3. vates-capi creates an `XOMachine` per `Machine` and asks XO to clone the VM.
4. Each VM boots and appears in XO; the guest agent reports its address.
5. CABPT/CACPPT apply the machine config; the node joins and becomes `Ready`.
6. The `MachineDeployment` brings up the workers.

Once the control plane is up, retrieve its `talosconfig` to talk to the
workload cluster:

```bash
kubectl get talosconfig -n default -o yaml -o jsonpath='{.status.talosConfig}'
```

By default the templates keep Talos' built-in CNI (flannel) and kube-proxy.
Cilium can be installed instead, as a CNI or as a kube-proxy replacement, using
the components under `templates/talos/components/` or the
`cluster-template-cilium.yaml` flavor.

## The maintenance-mode trap

A machine that boots, shows an address in XO, and then never joins is almost
always stuck **before it applies its machine configuration**: in Talos
maintenance mode when the configuration was found and rejected, still in the
boot stage when the configuration was never found at all.

| Symptom | Cause |
|---|---|
| Node in maintenance, config rejected as unknown documents | `talosVersion` does not match the image's Talos version |
| Node in maintenance, config drive not mounted | `viridian: true` on the XO template, or an image not built for `nocloud` |
| Still in `STAGE: Booting`, `TYPE: unknown`, no config applied | The machine template does not declare `bootstrap.format: passthrough`, so the config drive is not labelled `cidata` and Talos never finds the machine config. Fixed in v1.1.0 — check the version you deploy |
| No address reported in XO at all | The `siderolabs/xen-guest-agent` extension is missing from the image |
| Nodes share an identity / fail to join | The template was booted before being cloned, or built from a booted image |

The first is the most common. CABPT generates a config for **its own default
version** if `talosVersion` is unset; when that differs from the running image,
the node rejects the config and stays in maintenance. Always set `talosVersion`
to the image's exact version, and rebuild the image when you bump it.

The `STAGE: Booting` row is a provider-side failure, not something you can fix
in the cluster: the machine config was written to the config drive, but the
drive was not labelled `cidata`, so Talos never looked at it. Declaring
`bootstrap.format: passthrough` on the machine template is what tells the
provider to label the drive — the templates in this repository do it, a template
you wrote yourself may not.

The rest are handled by the checks on the [prerequisites](prerequisites.md)
page. If a machine is not coming up, work through that checklist before
suspecting the provider.

## Where next

- The image build and version pinning: [Prerequisites](prerequisites.md).
- The providers and the extra RBAC: [Installation](installation.md).
- Network and address details: [Networking](../../networking.md) and
  [IPAM](../../ipam.md).
- Every field: [Custom resources](../../../reference/crds/index.md).
