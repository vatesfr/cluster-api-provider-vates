# Tutorial: a cluster from A to Z

This walkthrough builds a working Kubernetes cluster on Xen Orchestra, starting
from nothing: the VM image in Xen Orchestra, the management cluster, the
credentials, the machine templates, the cluster objects, and the nodes joining.

By the end you have `my-cluster`: one control plane node and one worker, both
`Ready`, reachable with `kubectl`.

## What we are going to do

Four controllers share the work, and each owns a different question:

| Controller | Question it answers |
|---|---|
| **Cluster API** | which `Machine`s exist (worker ones come from a `MachineDeployment`) |
| **control plane provider** | which control plane `Machine`s exist, and the cluster CA |
| **bootstrap provider** | how a node joins: the bootstrap configuration, as a `Secret` |
| **vates-capi** (this provider) | *where* the node runs: the VM, its config drive, its address |

They never call each other. They watch the same Kubernetes API and write
objects; the `Cluster` graph is what ties them together. See
[How they fit together](../../concept/how-they-link.md) for the full picture.

The walkthrough is in three parts:

| Part | What it does |
|---|---|
| [Preparation](preparation.md) | the VM image, the management cluster, the credentials, the providers |
| [The objects](cluster.md) | the machine templates and the cluster objects, written out in full |
| [Apply and run](run.md) | applying them, watching the nodes join, reaching the cluster, cleaning up |

There are two ways to end up with the same objects: write them, as this tutorial
does, or have `clusterctl` render them from a template
([Cluster templates](../../reference/templates.md)). Writing them is how you
learn which field does what; the templates are those same objects with
placeholders.

Start with [Preparation](preparation.md).
