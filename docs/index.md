# Clusters on Xen Orchestra, the Cluster API way

**vates-capi** is a [Cluster API](https://cluster-api.sigs.k8s.io/) infrastructure
provider. It lets you describe Kubernetes clusters as ordinary Kubernetes
objects, then builds and manages them as virtual machines on
[Xen Orchestra](https://xen-orchestra.com/) / XCP-ng.

In practice, that means you never SSH into a hypervisor to create a control
plane or a worker again. You write a few YAML manifests, `kubectl apply` them to
a management cluster, and the provider does the rest: it clones VM templates,
starts the machines, wires them to the right networks, hands them their
bootstrap configuration, and reports back when they are ready.

<div class="grid cards" markdown>

-   :material-book-open-variant:{ .lg .middle } **New to Cluster API?**

    ---

    Start with [Concept](concept/index.md). It explains what providers are, how
    the bootstrap, control plane, and infrastructure pieces talk to each other,
    and where this provider fits in.

    [:octicons-arrow-right-24: Understand the pieces](concept/index.md)

-   :material-rocket-launch:{ .lg .middle } **Ready to build a cluster?**

    ---

    Head to [Getting started](getting-started/installation.md). Install the
    provider on a management cluster and create your first XO-backed cluster.

    [:octicons-arrow-right-24: Install and run](getting-started/installation.md)

</div>

## How it looks

<div>
<svg class="arch-diagram" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 900 900" width="100%" role="img" aria-label="You declare a cluster through the Kubernetes API. The providers only watch that API: Cluster API creates the worker Machines, the control plane provider creates the control plane Machines, and the bootstrap provider writes each Machine's bootstrap data Secret. vates-capi reads that Secret, turns it into the VM's user-data and network-config, and asks the Xen Orchestra API to clone the VM template with that config drive. The VM boots, joins the cluster, and becomes a Node of the workload cluster.">
<style>
.arch-diagram .box { fill: currentColor; fill-opacity: 0.05; stroke: currentColor; stroke-width: 1.5; }
.arch-diagram .grp { fill: none; stroke: currentColor; stroke-width: 1; stroke-dasharray: 6 6; opacity: 0.4; }
.arch-diagram .lbl { fill: currentColor; font-family: inherit; font-size: 15px; }
.arch-diagram .strong { font-weight: 600; }
.arch-diagram .sub { fill: currentColor; opacity: 0.7; font-size: 13px; }
.arch-diagram .cap { fill: currentColor; font-size: 12.5px; opacity: 0.6; letter-spacing: 0.06em; }
.arch-diagram .line { stroke: currentColor; stroke-width: 1.6; opacity: 0.75; fill: none; }
.arch-diagram .tip { fill: currentColor; opacity: 0.75; }
</style>
<defs>
  <marker id="arch-tip" viewBox="0 0 10 10" refX="9.5" refY="5" markerWidth="7" markerHeight="7" orient="auto">
    <path d="M0,0 L10,5 L0,10 Z" fill="currentColor" opacity="0.75"/>
  </marker>
</defs>
<rect class="grp" x="20" y="80" width="860" height="485" rx="16"/>
<text class="cap" x="36" y="72">MANAGEMENT CLUSTER</text>
<rect class="box" x="340" y="16" width="220" height="44" rx="10"/>
<text class="lbl strong" x="450" y="44" text-anchor="middle">You — kubectl apply</text>
<rect class="box" x="120" y="92" width="660" height="52" rx="10"/>
<text class="lbl strong" x="450" y="123" text-anchor="middle">Kubernetes API</text>
<line class="line" x1="450" y1="144" x2="450" y2="196"/>
<path class="tip" d="M446,151 L450,144 L454,151 Z"/>
<path class="tip" d="M454,189 L450,196 L446,189 Z"/>
<line class="line" x1="170" y1="144" x2="170" y2="196"/>
<path class="tip" d="M166,151 L170,144 L174,151 Z"/>
<path class="tip" d="M174,189 L170,196 L166,189 Z"/>
<line class="line" x1="730" y1="144" x2="730" y2="196"/>
<path class="tip" d="M726,151 L730,144 L734,151 Z"/>
<path class="tip" d="M734,189 L730,196 L726,189 Z"/>
<rect class="box" x="40" y="196" width="260" height="70" rx="10"/>
<text class="lbl strong" x="170" y="226" text-anchor="middle">Cluster API</text>
<text class="sub" x="170" y="248" text-anchor="middle">creates worker Machines</text>
<rect class="box" x="320" y="196" width="260" height="70" rx="10"/>
<text class="lbl strong" x="450" y="226" text-anchor="middle">control plane provider</text>
<text class="sub" x="450" y="248" text-anchor="middle">creates control plane Machines</text>
<rect class="box" x="600" y="196" width="260" height="70" rx="10"/>
<text class="lbl strong" x="730" y="226" text-anchor="middle">bootstrap provider</text>
<text class="sub" x="730" y="248" text-anchor="middle">writes the bootstrap Secret</text>
<line class="line" x1="170" y1="266" x2="170" y2="320"/>
<path class="tip" d="M174,313 L170,320 L166,313 Z"/>
<line class="line" x1="450" y1="266" x2="450" y2="320"/>
<path class="tip" d="M454,313 L450,320 L446,313 Z"/>
<line class="line" x1="730" y1="266" x2="730" y2="320"/>
<path class="tip" d="M734,313 L730,320 L726,313 Z"/>
<rect class="box" x="40" y="320" width="520" height="54" rx="10"/>
<text class="lbl strong" x="300" y="354" text-anchor="middle">Machines</text>
<rect class="box" x="600" y="320" width="260" height="54" rx="10"/>
<text class="lbl strong" x="730" y="345" text-anchor="middle">bootstrap Secret</text>
<text class="sub" x="730" y="365" text-anchor="middle">one per Machine</text>
<line class="line" x1="300" y1="374" x2="300" y2="385"/>
<path class="tip" d="M304,385 L300,392 L296,385 Z"/>
<rect class="box" x="180" y="392" width="300" height="54" rx="10"/>
<text class="lbl strong" x="330" y="417" text-anchor="middle">XOMachine</text>
<text class="sub" x="330" y="437" text-anchor="middle">resource, one per Machine</text>
<line class="line" x1="330" y1="446" x2="330" y2="463"/>
<path class="tip" d="M334,463 L330,470 L326,463 Z"/>
<line class="line" x1="730" y1="374" x2="560" y2="470" marker-end="url(#arch-tip)"/>
<text class="sub" x="660" y="412" text-anchor="middle">reads</text>
<rect class="box" x="280" y="470" width="340" height="80" rx="10"/>
<image xlink:href="assets/logos/vates-planet.png" x="300" y="490" width="40" height="40"/>
<text class="lbl strong" x="356" y="499">vates-capi</text>
<text class="sub" x="356" y="519">infrastructure provider</text>
<text class="sub" x="356" y="539">reads the Secret → config drive</text>
<line class="line" x1="450" y1="550" x2="450" y2="610"/>
<path class="tip" d="M454,603 L450,610 L446,603 Z"/>
<text class="sub" x="462" y="583">create VM</text>
<rect class="grp" x="190" y="590" width="620" height="180" rx="16"/>
<text class="cap" x="204" y="582">XEN ORCHESTRA</text>
<rect class="box" x="340" y="610" width="220" height="56" rx="10"/>
<image xlink:href="assets/logos/xo-satellite.png" x="356" y="621" width="40" height="30"/>
<text class="lbl" x="404" y="642">Xen Orchestra API</text>
<rect class="box" x="590" y="610" width="200" height="56" rx="10"/>
<text class="lbl" x="690" y="634" text-anchor="middle">VM template</text>
<text class="sub" x="690" y="654" text-anchor="middle">in your pool</text>
<line class="line" x1="590" y1="638" x2="573" y2="638"/>
<path class="tip" d="M573,642 L566,638 L573,634 Z"/>
<line class="line" x1="450" y1="666" x2="450" y2="700"/>
<path class="tip" d="M454,693 L450,700 L446,693 Z"/>
<text class="sub" x="462" y="687">clones the template</text>
<rect class="box" x="340" y="700" width="220" height="56" rx="10"/>
<text class="lbl strong" x="450" y="734" text-anchor="middle">VM (with the config drive)</text>
<line class="line" x1="450" y1="756" x2="450" y2="810"/>
<path class="tip" d="M454,803 L450,810 L446,803 Z"/>
<text class="sub" x="462" y="788">kubelet joins</text>
<rect class="box" x="340" y="810" width="220" height="70" rx="10"/>
<text class="lbl strong" x="450" y="840" text-anchor="middle">Node</text>
<text class="sub" x="450" y="860" text-anchor="middle">of the workload cluster</text>
</svg>
</div>

You declare a cluster through the **Kubernetes API** — a `Cluster`, a control
plane object (`KubeadmControlPlane` / `TalosControlPlane`) and a
`MachineDeployment`. From then on it is objects all the way down, and the
providers only ever **watch** the API; they never call each other. Cluster API
creates the worker Machines, the control plane provider creates the control plane
Machines, and the bootstrap provider writes a **bootstrap Secret for every
Machine** — control plane and worker alike. That Secret is the node's first-boot
configuration: the `kubeadm init` / `join` commands for kubeadm, or the machine
config for Talos.

This provider's own resources are the **`XOCluster`** (one per cluster) and the
**`XOMachine`** (one per Machine). vates-capi reconciles each `XOMachine`: it
**reads the Machine's bootstrap Secret**, turns it into the VM's **user-data**
(cloud-init, optionally enriched with SSH keys or kube-vip) and **network-config**,
and asks the Xen Orchestra API to create the VM. Xen Orchestra clones the **VM
template** (`templateID`) and boots the VM with that config drive. Its kubelet
joins the cluster, and it becomes a **Node** of the workload cluster.

## What this provider owns

vates-capi is only the **infrastructure** half. It knows how to turn a
`XOMachine` into a running VM, and nothing else. Deciding *what* runs on that VM
— kubeadm, Talos, something else — is the job of a bootstrap provider and a
control plane provider, which you install alongside it.

That separation is deliberate, and [Concept](concept/providers.md) unpacks it
fully. Long story short: the provider stays small, and adding a new bootstrap
mechanism never requires touching its code.
