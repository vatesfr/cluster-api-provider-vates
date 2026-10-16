# Bootstrap payload

vates-capi never identifies the bootstrap provider. It never asks "is this
kubeadm or Talos?". Instead, each `XOMachine` / `XOMachineTemplate` **declares
the format** of the payload it is handed, through `spec.bootstrap.format`.

This is a small detail with a large consequence: adding support for a new
bootstrap provider requires **no change to this controller**. You declare the
format on the template, and the provider acts on the declaration.

## The declaration

```yaml
spec:
  bootstrap:
    format: cloudinit   # cloudinit | passthrough (default)
```

| `format` | Effect on the payload |
|---|---|
| `passthrough` (default) | Delivered untouched, and the config drive is made discoverable to the guest platform (see below). |
| `cloudinit` | Treated as a cloud-init document, which may be enriched — see below. |

Leaving `spec.bootstrap` unset is the same as `format: passthrough`.

## The config drive

The payload is not sent to the VM over the network: it is written into the VM's
**config drive**, which the guest platform reads at boot. How that drive is
built differs by format, and a pass-through payload needs one extra thing.

Xen Orchestra only labels the config drive **`cidata`** when a *network-config*
is present on it. A cloud-init guest looks for its `user-data` and does not care
about the label, but a pass-through payload is read by the guest platform — for
Talos, the `nocloud` datasource — which finds its machine configuration **by
that label**. Without it, the drive is written but stays invisible: the node
boots, reports an address, and never applies a machine configuration at all.

The provider therefore writes a `#` comment placeholder as the network-config
whenever the format is pass-through and no guest network config is set. It is
not something you declare: it follows from `format: passthrough`, and it is why
that declaration must be explicit on every template that uses it.

### What `cloudinit` unlocks

Two enrichments, both driven by the **cluster** rather than by the machine:

- **SSH keys** — when `XOCluster.spec.injectSSHKeys` is `true`, the XO user's
  SSH keys are merged into `ssh_authorized_keys`.
- **kube-vip** — when `XOCluster.spec.controlPlaneLB` is `kube-vip`, the kube-vip
  scripts are injected into the control plane nodes.

The machine only declares *what the payload is*; the cluster decides *what to do
with it*. kube-vip in particular is a cluster-wide concern — it carries the
control plane VIP — so it is not a per-machine flag.

## Where the payload comes from

The bootstrap provider writes the payload into a Kubernetes `Secret`. The
provider does not go looking for it: it reads the secret named by the owning
`Machine`'s bootstrap reference. If you set `spec.bootstrapData` directly on the
`XOMachine`, that inline value is used instead.

## Two flows, one code path

=== "kubeadm"

    The kubeadm bootstrap provider emits a cloud-init document. The template
    declares:

    ```yaml
    bootstrap:
      format: cloudinit
    ```

    The provider delivers it, merging SSH keys and injecting kube-vip when the
    cluster asks for them.

=== "Talos"

    The Talos bootstrap provider emits a Talos machine configuration. It is not
    cloud-init, and nothing needs to be injected, so the template declares
    **pass-through** — explicitly:

    ```yaml
    bootstrap:
      format: passthrough
    ```

    The provider delivers the machine config verbatim, and labels the config
    drive so Talos' `nocloud` datasource finds it.

!!! warning "Declare it, do not rely on the default"

    An unset `spec.bootstrap` is *treated* as `passthrough`, so a template that
    declares nothing happens to work. Do not count on it: the declaration is
    what states the intent, and a template that does not declare its format
    breaks silently the day the default changes. Every Talos template in this
    repository declares `format: passthrough` for that reason.

The controller runs the **same code** in both cases. The only difference is the
declaration.

!!! tip "The `bootstrapProvider` field is not this"

    `XOMachine.spec.bootstrapProvider` exists too, but it only sets a
    `bootstrap:<name>` tag on the VM for observability. It has no effect on how
    the payload is handled. If you are tempted to set it and stop there, set
    `spec.bootstrap` instead — or rather, as well.
