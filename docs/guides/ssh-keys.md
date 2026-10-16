# SSH keys

Injecting the Xen Orchestra user's SSH public keys into every VM's
`ssh_authorized_keys`, so you can log in to nodes for debugging.

By default, the provider leaves the bootstrap payload exactly as the bootstrap
provider wrote it. Turn `injectSSHKeys` on and it also merges the SSH public keys
from your Xen Orchestra user profile into the cloud-config, on every VM of the
cluster.

## What it does

The switch is a single boolean on the `XOCluster`:

```yaml
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: my-cluster
spec:
  injectSSHKeys: true
```

| Field | Type | Default | Effect |
|---|---|---|---|
| `XOCluster.spec.injectSSHKeys` | `bool` | `false` | Merge the XO user's SSH keys into each VM's cloud-config. |

It is **off by default**. When it is on, the provider fetches the SSH public keys
of the Xen Orchestra user the cluster authenticates as, and adds them to the
`ssh_authorized_keys` section of the cloud-init user-data of every machine in
the cluster — control plane and worker alike. Keys already present in the
payload are not duplicated.

## Why it is opt-in

The provider is a courier. The bootstrap data it delivers comes from the
bootstrap provider and, indirectly, from you. Injecting SSH keys **modifies that
data**: it is not a delivery detail, it is a change to the document the node
boots with. Doing that silently would be surprising, and it would widen access to
every node — a security-relevant side effect — without anyone asking for it. So
it happens only when the cluster explicitly opts in.

Turning it off again does not remove keys from VMs that already booted; the
injection happens once, when the VM is created. To revoke access, rotate the keys
in Xen Orchestra and replace the machines.

## The pairing with the cloud-init format

Injection only happens for machines that declare their payload as cloud-init:

```yaml
spec:
  template:
    spec:
      bootstrap:
        format: cloudinit
```

The declaration lives on the `XOMachineTemplate`, not on the `XOCluster`. A key
consequence follows: the cluster can set `injectSSHKeys: true`, but a machine
whose payload is passed through untouched (`passthrough`, the default) is left
alone. The provider will not guess that an arbitrary payload is a cloud-config it
can edit.

This is the same declaration kube-vip uses; both enrichments are gated on it. See
[Bootstrap payload](../reference/bootstrap.md) for the full picture.

## Where the keys come from

Not from a Kubernetes `Secret` and not from a field on the `XOCluster`. The
provider asks Xen Orchestra, as the authenticated user, for that user's profile,
and reads its `sshKeys` list:

```
XO session.getUser  →  preferences.sshKeys[].key
```

Practically, those are the public keys you manage in Xen Orchestra under your
**user profile → SSH keys**. The user in question is the same one whose `url` and
`token` the cluster authenticates with — the global credentials secret, or the
cluster's [`identityRef`](../reference/crds/xocluster.md) if it overrides them.

!!! tip "Add the keys where the cluster authenticates"

    Because the keys are read from the authenticated XO user, a cluster with its
    own `identityRef` uses **that** user's keys, not yours. In a multi-tenant
    setup, each cluster's keys follow its credentials.

## Verify the keys landed

The merge happens at VM creation, on the way to the config drive — it is not
written back into the bootstrap `Secret`. So look at the VM, not at the Secret.

On the node itself (through the XO console, or once the cluster is reachable):

```bash
# On the node, as the default cloud-init user
cat ~/.ssh/authorized_keys
```

Or reach a node through the API server without a separate login, then inspect it:

```bash
kubectl --kubeconfig my-cluster.kubeconfig debug node/<node-name> -it --image=alpine
```

If the keys are missing, check, in order: `XOCluster.spec.injectSSHKeys` is
`true`; the machine template declares `bootstrap.format: cloudinit`; and the
authenticated XO user actually has SSH keys configured in its profile. A missing
key set is not an error — the provider merges nothing and the machine boots
normally.

## Where next

- What `format: cloudinit` unlocks, and why it is declared per machine:
  [Bootstrap payload](../reference/bootstrap.md).
- The other cloud-init enrichment: [VIP](vip.md).
- The `XOCluster` fields, including `identityRef`:
  [XOCluster](../reference/crds/xocluster.md).
- Template preparation, including SSH and the default user:
  [Prerequisites](../getting-started/prerequisites.md).
