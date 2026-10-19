# VIP

A self-managed control plane needs a **floating control plane endpoint** — the
single address every client points at (`CP_HOST`, `CP_VIP`), which survives the
loss of any one node. How you get one depends on the flow:

| Flow | Floating endpoint |
|---|---|
| [kubeadm](providers/kubeadm.md) | **kube-vip**, injected into the control plane cloud-init by vates-capi (`XOCluster.spec.controlPlaneLB: kube-vip`). The provider does not run it — the guest does, on first boot. |
| [Vates Kube OS](providers/vates-kube-os.md) | **kube-vip** as well, and it is the default for this flow: the node runs it, driven by `VatesBootstrapConfig.spec.cluster.vip`. |
| [Talos](providers/talos/index.md) | Talos's **built-in** shared IP (`machine.network.interfaces[].vip.ip`). No kube-vip involved. |

This page details the **kubeadm** injection; [Vates Kube OS](providers/vates-kube-os.md)
uses the same kube-vip but configures it through the bootstrap document, and
[Talos](providers/talos/index.md) is covered by its own pages.

## What a floating endpoint is, and why you need one

A self-managed control plane is a set of nodes: three of them, typically, each
running an API server. But `kubeconfig`, the kubelets, and everything else point
at **one** address. If that address is a single node's IP, losing that node takes
the cluster's front door with it.

A **floating** (virtual) IP solves this. It lives on exactly one control plane
node at a time and moves to a survivor when that node goes away. kube-vip
implements it: it runs as a static pod on every control plane node and elects a
leader through the Kubernetes API. The leader answers ARP for the VIP and routes
traffic to the local API server; if it disappears, another node re-claims the
VIP within seconds.

The address is set once, on the cluster, as
`XOCluster.spec.controlPlaneEndpoint.host`. That is also the `CP_HOST` you export
when you generate a cluster, as in the
[tutorial](../getting-started/tutorial/index.md).

## Enabling it

kube-vip is opt-in and driven from two places: the **cluster** says whether it
wants a floating endpoint, and each **machine template** says that its payload is
cloud-init (and therefore may be enriched).

### On the cluster

Set the endpoint and the load balancer on the `XOCluster`:

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: my-cluster
spec:
  controlPlaneEndpoint:
    host: 192.168.1.240    # the VIP — must be free and routed
    port: 6443
    subnet: 16             # CIDR bits of the VIP's subnet
  controlPlaneLB: kube-vip
```

| Field | Role |
|---|---|
| `controlPlaneEndpoint.host` | The VIP itself. This is what kube-vip advertises. |
| `controlPlaneEndpoint.port` | The API server port (`6443` by convention). |
| `controlPlaneEndpoint.subnet` | CIDR mask of the VIP's network. Passed to kube-vip as `--vipSubnet`; auto-detected when unset. |
| `controlPlaneLB` | Selection of the load balancer. The only supported value is `kube-vip`. |

`controlPlaneLB` is an enum: `kube-vip` is the one accepted value. Leaving it
unset (or empty) means no floating endpoint — the endpoint is then whatever
address the cluster ends up with.

### On the machines

Each `XOMachineTemplate` — control plane and worker — declares that its bootstrap
payload is a cloud-init document:

```yaml
spec:
  template:
    spec:
      bootstrap:
        format: cloudinit
```

This is the same declaration that unlocks SSH key injection, described in
[Bootstrap payload](../reference/bootstrap.md). Without it (or with
`format: passthrough`), the payload is delivered untouched and **nothing is
injected** — neither kube-vip nor SSH keys.

Injection happens only on **control plane** machines. The reconciler checks the
`cluster.x-k8s.io/control-plane` label on the owning `Machine`, so workers are
never touched even when the cluster enables kube-vip.

!!! note "kube-vip is a cluster policy, not a machine flag"

    There is a deprecated `XOMachine.spec.bootstrap.kubeVIP` boolean. It has no
    effect. Whether kube-vip is injected is decided by
    `XOCluster.spec.controlPlaneLB` and the machine's role.

## What the provider injects

The provider does not run anything. It rewrites the bootstrap cloud-config so the
**guest** builds and runs kube-vip on first boot. Concretely, it parses the
cloud-init document, appends two files under `write_files`, and brackets the
`kubeadm init` / `kubeadm join` command in `runcmd` with calls to them:

```
runcmd:
  … your commands …
  - bash /etc/kubernetes/kube-vip-pre.sh     # pull the image
  - kubeadm init …   (or kubeadm join …)     # the API server comes up
  - bash /etc/kubernetes/kube-vip-post.sh    # generate the static pod
  … your commands …
```

**The pre script** (`/etc/kubernetes/kube-vip-pre.sh`) checks that `ctr` exists —
kube-vip needs containerd — and pulls the kube-vip image with it:

```sh
ctr image pull ghcr.io/kube-vip/kube-vip:v1.1.2
```

**The post script** (`/etc/kubernetes/kube-vip-post.sh`) waits for the local API
server to answer (`https://127.0.0.1:6443/healthz`), builds a kubeconfig from
`super-admin.conf` (or `admin.conf`), and generates the static pod manifest into
`/etc/kubernetes/manifests/kube-vip.yaml`:

```sh
ctr run --rm --net-host ghcr.io/kube-vip/kube-vip:v1.1.2 gen /kube-vip manifest pod \
  --interface "$VIP_INTERFACE" --address 192.168.1.240 \
  --controlplane --services --arp --leaderElection --vipSubnet 16 \
  > /etc/kubernetes/manifests/kube-vip.yaml
```

The interface is taken from the node's default route (falling back to `eth0`),
and `--vipSubnet` is present only when `controlPlaneEndpoint.subnet` is set.
Because the file lands in the kubelet's static-pod directory, kubelet starts
kube-vip on its own — no additional manifest to apply.

!!! warning "Requirements this imposes on the image"

    The node must run **containerd** (for `ctr`) and be able to pull the
    kube-vip image. That is why the shipped AlmaLinux images install containerd
    at bootstrap or bake it in. A node without containerd fails the pre script
    and never runs kube-vip.

## Verify the VIP is claimed

Once a control plane node has booted, the static pod should be running:

```bash
kubectl --kubeconfig my-cluster.kubeconfig -n kube-system get pods | grep kube-vip
```

`kube-vip-<node-name>` appears as a static pod. Because they all run the same
election, only the leader actually holds the VIP; check which one on a control
plane node:

```bash
# via the XO console, or: kubectl debug node/<node> -it --image=alpine
ip -4 addr show | grep 192.168.1.240
```

Finally, reach the API through the VIP — this is the whole point, and the only
check that proves the endpoint works:

```bash
curl -k https://192.168.1.240:6443/healthz
```

## Troubleshooting

**Nothing was injected.** Work down this list in order:

- `XOCluster.spec.controlPlaneLB` is not `kube-vip`.
- `XOCluster.spec.controlPlaneEndpoint.host` is empty — the provider logs
  `Kube-vip enabled but no control plane endpoint set, skipping injection`.
- The machine template does not declare `bootstrap.format: cloudinit`, or the
  machine is a worker.
- The cloud-config has no `runcmd`, or no `kubeadm init` / `kubeadm join` command
  in it. The injection needs a place to anchor; in that case the
  `XOMachine` gets a `Ready: False` condition with reason
  `KubeVIPInjectionFailed` and the message says what was missing.

**The manifest is generated but the pod never starts.** On the node, look at
`/etc/kubernetes/manifests/kube-vip.yaml` and the pre script output. The usual
causes are a missing `ctr` (containerd not installed) and an image that the node
cannot pull — off-network nodes need a registry mirror or a pre-loaded image.

**The VIP is claimed but the API server is unreachable through it.** The API
server's serving certificate must list the VIP in its SANs, or TLS clients
reject it. The shipped templates add `controlPlaneEndpoint.host` to `certSANs`;
if you copied a template and dropped that patch, add it back:

```yaml
    clusterConfiguration:
      apiServer:
        certSANs:
          - localhost
          - 127.0.0.1
          - 192.168.1.240   # the VIP
```

**The VIP fights something else for the address.** The VIP must be free and
outside any DHCP range. If you use [IPAM](ipam.md), carve the pool so it stops
well short of the VIP — otherwise a node is eventually handed the VIP as an
ordinary address and the two compete.

**Traffic reaches the wrong interface.** `--vipSubnet` must match the VIP's
network. A wrong `subnet` on `controlPlaneEndpoint` makes kube-vip advertise the
VIP on the wrong broadcast domain. When unset, kube-vip auto-detects the mask
from the interface.

## Where next

- The end-to-end walkthrough that sets `CP_HOST` and `CP_LB`:
  [Tutorial](../getting-started/tutorial/index.md).
- The IPAM pool that must not contain the VIP: [IPAM](ipam.md).
- The `cloudinit` declaration and what it unlocks:
  [Bootstrap payload](../reference/bootstrap.md).
- The `XOCluster` fields in full: [XOCluster](../reference/crds/xocluster.md).
