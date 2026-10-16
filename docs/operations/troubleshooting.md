# Troubleshooting

Most problems surface the same way: a `Machine` that never becomes `Ready`. The
work is to find out *which stage* it is stuck at. This page walks the common
ones, then gives a quick symptom-to-cause table.

## First moves

Get the three views side by side. A `Machine` is the generic object, an
`XOMachine` is its infrastructure, and the `Node` is what joined the cluster —
when it does.

```bash
kubectl -n <ns> get machines -w
kubectl -n <ns> get xomachines -o wide
kubectl -n <ns> get nodes -o wide
```

Then read the condition on the infrastructure object. The provider reports every
failure as the **`Ready`** condition, with a specific `reason`:

```bash
kubectl -n <ns> get xomachine <name> \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}: {.message}{"\n"}{end}'
```

Follow the controller logs and the events; they carry the underlying Xen
Orchestra or Kubernetes error:

```bash
kubectl -n capi-system logs -l app.kubernetes.io/name=vates-capi -f
kubectl -n <ns> get events --sort-by=.lastTimestamp
clusterctl describe cluster <cluster>
```

!!! tip "The reason is the branch, the message is the error"

    Think of `reason` as which code path you are on (`WaitingForIPAddress`,
    `TemplateNotFound`, `VmCreationFailed`, …) and `message` as what the last
    attempt returned. The two together usually point straight at the fix.

## A machine never gets an address

The VM is created and running, but `status.addresses` stays empty. The condition
reason tells you which of two very different problems you have.

### `WaitingForIPAddress` — no guest agent

```
Ready=False WaitingForIPAddress: VM is running, waiting for IP address from
Xen guest tools
```

The provider reads the VM's address **through the Xen guest agent**, not from
DHCP or the network. If the template has no guest tools installed and running, the
address never appears and the machine waits forever (requeueing every few
seconds).

- Install and enable Xen guest tools in the VM template.
- Confirm inside the guest that the agent is running and reporting an address.

!!! note "Link-local addresses are ignored on purpose"

    The provider accepts only a usable IPv4 address. `169.254.x.x` (the DHCP
    placeholder) and `fe80::` (IPv6 link-local) are rejected, because a node
    advertising either is unreachable. A VM that only shows those is not ready
    yet, not misconfigured — give it a moment and check again.

### `WaitingForAddress` — IPAM has not answered

```
Ready=False WaitingForAddress: the IPAM pool has not answered the address claim
yet
```

This one means the machine uses IPAM (`networkConfig.addressesFromPools`) and the
`IPAddressClaim` exists but no address came back. The provider deliberately does
**not** create the VM until the pool answers.

```bash
kubectl -n <ns> get ipaddressclaims
kubectl -n <ns> get ipaddresses
kubectl -n <ns> get inclusterippool <pool> -o yaml
```

Common causes, all covered in [IPAM](../guides/ipam.md):

- the pool does not exist (or was applied after the machines);
- the pool is exhausted, or two clusters share one namespaced pool;
- the pool overlaps the control plane VIP.

An `AddressClaimFailed` reason instead means the claim itself could not be created
or read — check the IPAM CRDs are installed and the message for details.

## A machine boots but never joins

The VM is up and has an address, but no `Node` ever appears, or the `Node` stays
`NotReady`. Work outward from the VM.

**The template ignores cloud-init.** This is the most common cause. The provider
hands the bootstrap payload to Xen Orchestra, which writes it to the config
drive; if the guest does not run cloud-init (NoCloud datasource), it boots and
does nothing. Check that the template is cloud-init enabled and, inside the
guest, look at the cloud-init logs and whether the config drive was consumed.

**The Kubernetes version disagrees.** With kubeadm, the version the control plane
expects and the version the node installs must match. When they do not, the
machine is rolled forever. See [Upgrading](upgrading.md) for how the version is
pinned, and the [kubeadm guide](../guides/providers/kubeadm.md) for the image
variants.

**The workload cluster has no CNI.** The node may register but never become
`Ready`, because it cannot bring up pod networking. That is expected unless you
enabled an addon or installed one yourself:

```bash
kubectl -n <ns> get xocluster <cluster> -o jsonpath='{.spec.addons}{"\n"}'
kubectl --kubeconfig <cluster>.kubeconfig get nodes
```

See [XOCluster](../reference/crds/xocluster.md) for the `addons` fields.

**The VM is never created at all.** If you expected a VM but nothing appears in
Xen Orchestra, read the condition first — a templating error stops creation
rather than delivering a broken payload. `CloudConfigBuildFailed` or
`KubeVIPInjectionFailed` point at the bootstrap payload; the
[Bootstrap payload](../reference/bootstrap.md) reference explains what the
provider does with it.

## `providerID` stays empty

`status.providerID` (shown by `kubectl get xomachines`) is written **after the VM
is created**. An empty value almost always means creation never completed, not
that reporting failed.

| Reason | What to check |
|---|---|
| `CredentialsResolutionFailed` | The `identityRef` secret exists and has `url`/`token`. |
| `XOConnectionFailed` | The XO URL is reachable and the token is valid. |
| `TemplateNotFound` / `PoolNotFound` | The `templateID` / `poolID` (or the name lookups) resolve to a real object. |
| `VmCreationFailed` | The XO user has permission to clone into that pool, and the pool has room. |

Remember that the manager loads its **global** credentials at startup and exits
if the secret is missing — so a controller pod in `CrashLoopBackOff` with a
"Failed to load XO configuration" message means the `xo-credentials` secret in
`capi-system` is absent or malformed, long before any machine is touched.

!!! note "`providerID` on the Node comes from the CCM"

    The `providerID` on the workload **`Node`** is set by the Xen Orchestra
    cloud-controller-manager addon, not directly by this provider. If
    `XOMachine.status.providerID` is set but the `Node` has none, the addon is
    the place to look — see [XOCluster](../reference/crds/xocluster.md).

## A node stuck in maintenance (Talos)

A Talos node that lands in **maintenance mode** has rejected its machine config.
The usual cause is a version mismatch:

`TalosControlPlane.spec.controlPlaneConfig.controlplane.talosVersion` **must** be
the Talos version the VM image was built with. When it is unset or wrong, the
bootstrap provider generates a config for its own default version; the node
rejects the unknown documents and stays in maintenance instead of joining.

The other prerequisites are just as unforgiving, and they fail the same way:

- the image must be built for the **`nocloud`** platform;
- it must include the `siderolabs/xen-guest-agent` extension;
- the template must be **never booted**;
- `viridian` must be `false` in Xen Orchestra (with `viridian: true`, XO prepends
  an MBR that Talos cannot mount as a config drive).

See the [Talos prerequisites](../guides/providers/talos/prerequisites.md) for the
full list.

## Persistent volumes stuck attached

When a node's VM dies, its volumes stay attached to the dead node until
kube-controller-manager gives up waiting — six minutes by default — and force
detaches them. The provider's CCM ships an **out-of-service controller** that
shortens that: it applies the `node.kubernetes.io/out-of-service` taint to a node
whose Xen Orchestra VM is deleted, halted, paused or suspended and whose kubelet
reports `NotReady`, so the volumes are released promptly.

If volumes seem stuck, check whether the controller is running on the cluster:

```bash
kubectl -n <ns> get xocluster <cluster> \
  -o jsonpath='{.spec.addons.nodeOutOfService}{"\n"}'
kubectl --kubeconfig <cluster>.kubeconfig get nodes \
  -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints
kubectl --kubeconfig <cluster>.kubeconfig get volumeattachments
```

The controller is enabled by default; it is only off if
`addons.nodeOutOfService.enabled` is `false`. A VM that is **deleted** from Xen
Orchestra is tainted immediately; a powered-off one waits out the configured
`gracePeriod` first, so a brief reboot does not trigger a force-detach. See the
`nodeOutOfService` fields in [XOCluster](../reference/crds/xocluster.md).

On the provider side, deleting an `XOMachine` detaches CSI-managed persistent
volume disks before removing the VM, and stops a Paused/Suspended VM first so the
detach can succeed. A deletion stuck on "Failed to detach persistent volume
disks" is a Xen Orchestra problem — read the message.

## The Cluster reports `Available: False`, but it is up

`Cluster.status.conditions[type=Available]` is an **aggregate**, not a liveness
probe:

```
Cluster Available = ControlPlaneAvailable  AND  (every MachineDeployment Available)
```

A `MachineDeployment` is `Available` when

```
availableReplicas >= replicas - maxUnavailable
```

`maxUnavailable` comes from `spec.strategy.rollout.maxUnavailable` and
**defaults to `0`**. With that default the threshold equals `replicas`, so a
MachineDeployment is only `Available` when **every** replica is up — one worker
down is enough to turn the whole Cluster `Available: False`:

```
WorkersAvailable: * MachineDeployment <name>: 3 available replicas, at least 4
required (spec.strategy.rollout.maxUnavailable is 0, spec.replicas is 4)
```

This mirrors upstream Kubernetes: `Deployment` availability uses the same
`replicas - maxUnavailable` rule.

!!! warning "`Available: False` is a signal, not an outage"

    While a single replica is missing the cluster keeps serving: the API is up
    and the remaining nodes work. Read `Available` as "fully at desired
    capacity", not "reachable".

To tolerate one replica down, set the strategy on the MachineDeployment — and
put it in the cluster template so new clusters inherit it:

```yaml
spec:
  strategy:
    rollout:
      maxUnavailable: 1
```

!!! note

    `maxUnavailable: 0` also prevents any rollout: no node can be drained, so
    upgrades that must replace a node stall. Setting it to `1` enables both
    tolerance and rollouts.

Inspect the current numbers with:

```shell
kubectl -n <ns> get cluster <name> \
  -o jsonpath='{range .status.conditions[?(@.type=="Available")]}{.status} {.reason}: {.message}{"\n"}{end}'
kubectl -n <ns> get machinedeployment <name> \
  -o jsonpath='replicas={.spec.replicas} available={.status.availableReplicas}{"\n"}'
```

## Decision tree: from symptom to likely cause

| What you see | Look at | Most likely cause |
|---|---|---|
| No `XOMachine` for a `Machine` | the control plane provider / `MachineDeployment` | The producer is stalled (often `maxUnavailable: 0`), not the provider. |
| `XOMachine` exists, no VM in XO | `Ready` reason on the `XOMachine`, controller logs | Credentials, template/pool lookup, or payload build (`TemplateNotFound`, `VmCreationFailed`, `CloudConfigBuildFailed`). |
| VM exists and runs, no address | `WaitingForIPAddress` vs `WaitingForAddress` | Guest agent missing, or the IPAM pool has not answered. |
| Address present, no `Node` | cloud-init in the guest, version fields | The template ignores cloud-init, or the Kubernetes version disagrees. |
| `Node` exists but `NotReady` | the `Cluster` `addons` | No CNI in the workload cluster. |
| `providerID` empty on the `XOMachine` | `Ready` reason | VM creation never finished; see the credentials/permission causes above. |
| `providerID` empty on the `Node` | the CCM addon | The Xen Orchestra CCM is not running or cannot reach XO. |
| Talos node in maintenance | `talosVersion`, image properties | Version mismatch, or a `viridian`/booted-image problem. |
| Volumes stuck attached | out-of-service controller, `volumeattachments` | Non-graceful shutdown; the controller is disabled or the grace period is elapsing. |
| `Cluster` `Available: False` while serving | `MachineDeployment` replica counts | `maxUnavailable: 0` with a replica missing or rolling. |

## Where next

- The fields behind each condition: [Custom resources](../reference/crds/index.md).
- Operating the cluster: [Upgrading](upgrading.md).
- How the provider handles the payload: [Bootstrap payload](../reference/bootstrap.md).
