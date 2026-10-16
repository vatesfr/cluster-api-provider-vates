# Talos

[Talos Linux](https://www.talos.dev/) is an immutable, minimal operating system
built to run Kubernetes and nothing else. It takes a different path from
kubeadm: there is no cloud-init, no shell, and no SSH. A node is configured by
feeding it a **machine configuration** at boot, and afterwards everything is
managed through its API.

For vates-capi this is an interesting case, because it exercises the design
described in [Concept](../../../concept/how-they-link.md) to the fullest: the
infrastructure provider stays exactly the same, and only the bootstrap and
control plane providers change. The bootstrap payload is a Talos machine config
instead of a cloud-init script, and vates-capi passes it through untouched — no
declaration, no special case.

!!! info "Talos is its own subject"

    Because Talos has different prerequisites (a dedicated image build, a
    `nocloud` platform, extra RBAC), it gets its own section rather than living
    alongside [kubeadm](../kubeadm.md). Read the pages in order:

    1. [Prerequisites](prerequisites.md) — the Talos image and cluster setup.
    2. [Installation](installation.md) — installing the Talos providers and RBAC.
    3. [Building a cluster](cluster.md) — an end-to-end example.

!!! danger "The Talos Cluster API providers are unmaintained"

    Sidero Labs states it is no longer actively developing the Cluster API
    Control Plane Provider Talos, and the bootstrap provider is in the same
    position. Neither CACPPT nor CABPT receives upstream updates: **support for
    them is effectively over.** Pin the provider versions instead of installing
    `latest`, and treat every Talos image upgrade as a change to test against
    those pinned providers — the image, the kubelet patch shape and the provider
    versions form a triple that has to agree, or nodes stop in `STAGE: Booting`
    without a service listening. See
    [Match the providers to the image](prerequisites.md#match-the-providers-to-the-image).

## The short version

```bash
clusterctl init --bootstrap talos --control-plane talos
kubectl apply -k config/rbac/talos
```

Then build a cluster from an overlay under `templates/talos/`. The details — and
the traps — are on the following pages.
