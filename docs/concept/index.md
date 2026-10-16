# Concept

This section is the *why* and the *how it works*. No installation, no YAML to
copy — just the mental model you need before touching a management cluster.

If you already know Cluster API well and only want the Vates specifics, you can
skim the first three pages and jump to
[Where Vates CAPI fits](vates-provider.md). If Cluster API is new to you, read them
in order: each page builds on the previous one.

1. [Cluster API in a nutshell](cluster-api.md) — the management cluster, the
   `Machine` abstraction, and the contract that ties everything together.
2. [The three providers](providers.md) — infrastructure, bootstrap, and control
   plane: what each one is responsible for.
3. [How they fit together](how-they-link.md) — how the three providers cooperate
   through object references to build a cluster, without ever calling each other.
4. [Where Vates CAPI fits](vates-provider.md) — the infrastructure provider for Xen
   Orchestra, and the custom resources it brings.

!!! tip "One idea worth holding onto"

    In Cluster API, **providers never call each other directly**. They
    communicate by reading and writing Kubernetes objects in the management
    cluster. Once that clicks, the rest is detail.
