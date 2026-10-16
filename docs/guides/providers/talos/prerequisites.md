# Talos prerequisites

Talos needs a VM template prepared very specifically. Getting this wrong is the
usual reason a Talos machine silently stays in maintenance mode.

Unlike kubeadm, there is no cloud-init and no shell: Talos is configured by
feeding it a machine configuration at boot, and everything afterwards goes
through its API. That changes what the image must be, and how the version is
chosen. Work through the checks below **before** you create an overlay.

## The Talos image

Build the image with the [Talos Image Factory](https://factory.talos.dev/) using
the **Server Cloud** platform, whose schematic id is **`nocloud`**. The image
must contain the Xen guest agent system extension:

```
siderolabs/xen-guest-agent
```

On the Image Factory, select the `nocloud` platform and add that extension to
the schematic before downloading. It is not built in.

Once downloaded, and before importing it into Xen Orchestra:

- Create the template from the **RAW** image.
- Do **not** boot it. A booted template carries stale certificates and state
  that the clone would inherit, which breaks the cluster's identity.
- Register it in XO, then note its **UUID**. You will use it as `templateID` in
  the machine templates.

### Why the guest agent matters

Xen Orchestra reports a VM's IP addresses through the guest agent running inside
it. Without the agent:

- XO never reports an address, so vates-capi cannot publish
  `XOMachine.status.addresses`.
- Without an address, Cluster API cannot match the `Machine` to a node, so the
  node never joins and the machine never becomes `Ready`.
- The Xen Orchestra CCM cannot link the node to its VM either.

Talos does not ship the Xen agent, which is exactly why the
`siderolabs/xen-guest-agent` extension has to be added to the schematic.
Installing a plain Talos image and hoping for the best is a dead end.

## The template must never be booted

The clone is what joins the cluster, and it must start from a pristine image.
A Talos node that has booted once has written machine state and certificates;
cloning from a booted template hands every node the same identity, and Talos
rejects it. Build the template straight from the RAW image and leave it
powered off.

## `viridian: false` in Xen Orchestra

Set the template's `viridian` flag to **`false`**. When `viridian` is `true`,
XO prepends an MBR to the cloud-init config drive, and Talos' block utilities
cannot mount it. The node then finds no machine configuration and stays in
maintenance mode. The image factory target is a "cloud" (NoCloud) image, so the
config drive must stay in the plain form Talos expects.

| Setting | Required value | Why |
|---|---|---|
| Image platform | `nocloud` | Talos reads its config from the NoCloud drive |
| Extension | `siderolabs/xen-guest-agent` | XO needs it to report VM addresses |
| Template state | never booted | No stale certificates or state |
| `viridian` in XO | `false` | Otherwise XO breaks the config drive |

## Pin the Talos version

The image has a **Talos version**. The `TalosControlPlane` must be told the same
one:

```yaml
spec:
  controlPlaneConfig:
    controlplane:
      talosVersion: <your-talos-version>   # e.g. v1.13.9 — must match the image
```

When you use an overlay, this is set in `patch-controlplane.yaml`. When you use
`clusterctl generate`, the template reads it from the `TALOS_VERSION`
environment variable.

### Why the version must match

If `talosVersion` is left out, CABPT generates a machine config for **its own
default version**, which may not be the version running in your image. Talos
validates the configuration against the version it is running: documents it does
not recognize are rejected, the node never applies the config, and it stays in
**maintenance mode**. The symptom is a machine that boots, reports an address,
and then simply never becomes a cluster node.

The image and the pinned version must therefore agree exactly. Bump both
together: rebuild the image for the new Talos version, update the template in
XO, and set `talosVersion` to match.

## Match the providers to the image

Three things have to agree: the Talos image, the shape of the kubelet patch, and
the CABPT/CACPPT versions.

| Image | kubelet `extraArgs` | CABPT | CACPPT |
|---|---|---|---|
| Talos 1.13.x | `machine.kubelet.extraArgs`, inline | v0.6.12 | v0.5.13 |
| Talos 1.14.x | standalone `KubeletConfig` document | v0.6.13 | v0.5.14 |

Getting it wrong fails at boot, with the node stuck in `STAGE: Booting` and
`apid` never listening — two different errors for two different mismatches:

- **kubelet patch in the wrong shape** — the node refuses the whole machine
  configuration: `"KubeletConfig" "v1alpha1": not registered` on 1.13.x when the
  standalone document is used; `kubelet config is already set in v1alpha1 config
  (.machine.kubelet)` on 1.14.x when the inline form is used.
- **provider too new for the image** — CABPT v0.6.13 emits the Talos 1.14
  multi-document configuration (`DiscoveryServiceConfig`,
  `DiscoveryIdentityConfig`, `ResolverConfig`, `VolumeConfig`, …) for a
  `generateType: join` machine. A 1.13.x node does not know those documents and
  refuses the configuration. Control plane machines are unaffected: their
  configuration comes from CACPPT, not CABPT, which is why a mixed setup can show
  three healthy control plane nodes and no worker.

!!! warning "The Talos providers are not actively developed"

    Sidero Labs states it is no longer actively developing the Cluster API
    Control Plane Provider Talos. Pin the provider versions in your clusterctl
    configuration (`clusterctl init --bootstrap talos:vX.Y.Z --control-plane
    talos:vX.Y.Z`) instead of taking the latest, and treat a Talos image upgrade
    as a change to be tested against those pinned providers.

## Checklist

Before moving on, confirm every item:

- [ ] Image built for the **`nocloud`** platform.
- [ ] `siderolabs/xen-guest-agent` extension included.
- [ ] Template created from the **RAW** image and **never booted**.
- [ ] `viridian: false` on the XO template.
- [ ] Template UUID, pool UUID and network UUID noted.
- [ ] `talosVersion` chosen to match the image exactly.

Next: [install the Talos providers](installation.md).

## Where next

- [Installation](installation.md) — the providers and the extra RBAC.
- [Building a Talos cluster](cluster.md) — the end-to-end walkthrough.
- General XO access and the credentials secret:
  [Prerequisites](../../../getting-started/prerequisites.md).
