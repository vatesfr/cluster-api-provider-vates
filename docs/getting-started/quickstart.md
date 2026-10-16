# Quickstart

This walks you through creating a small XO-backed cluster with the kubeadm flow:
three control plane nodes and two workers. It assumes the provider is already
[installed](installation.md) and the credentials secret exists.

The whole thing is two commands. The rest of the page explains what they do and
how to watch the result.

## 1. Describe your environment

The cluster manifest is generated from a template that reads these variables:

```bash
export CP_HOST=192.168.1.240          # control plane VIP (see the note below)
export CP_PORT=6443
export CP_LB=kube-vip
export CP_SUBNET=16
export VM_NAME_PREFIX=my-cluster
export XO_TEMPLATE_UUID=<your-vm-template-uuid>
export XO_POOL_UUID=<your-pool-uuid>
export XO_NETWORK_UUID=<your-network-uuid>
```

- `CP_HOST` is the address the API server will be reachable at. With `kube-vip`
  it is a floating IP on your network that the control plane nodes share — it
  must be free and routed.
- `VM_NAME_PREFIX` names the VMs in Xen Orchestra, so give each cluster its own.

## 2. Generate and apply

```bash
clusterctl generate cluster my-cluster \
  --from templates/kubeadm/base/clusterctl/almalinux-fromscratch.yaml \
  --kubernetes-version v1.36.1 \
  --control-plane-machine-count 3 \
  --worker-machine-count 2 \
  | kubectl apply -f -
```

`clusterctl generate cluster` renders a template with your variables and prints
the resulting manifests. Piping it into `kubectl apply` creates everything at
once: the `Cluster`, its `XOCluster`, the control plane, and the worker
deployment.

!!! tip "Using the published default template"

    Without `--from`, `clusterctl` uses the provider's default
    `cluster-template.yaml`, shipped in `dist/`. That is the quickest form:

    ```bash
    clusterctl generate cluster my-cluster --infrastructure vates \
      --control-plane-machine-count 3 --worker-machine-count 2 \
      | kubectl apply -f -
    ```

## 3. Watch it come up

```bash
# The cluster-level infrastructure object
kubectl get xocluster my-cluster -w

# The VMs being created
kubectl get xomachines -w

# The generic view: nodes joining
kubectl get machines -w
```

In Xen Orchestra, you will see new VMs appear, named `my-cluster-…`, being cloned
from your template, configured, and started.

When a machine is ready, its `status.providerID` is set to
`xenorchestra://<pool-uuid>/<vm-uuid>` and `status.addresses` holds its IP.

## 4. Reach the workload cluster

Once the control plane is up, fetch its kubeconfig and look around:

```bash
clusterctl get kubeconfig my-cluster > my-cluster.kubeconfig
kubectl --kubeconfig my-cluster.kubeconfig get nodes
```

If the nodes are `NotReady`, the most likely cause is a missing CNI — the
workload cluster has none by default. Enable the Cilium addon (see
[Reference → Custom resources](../reference/crds/xocluster.md)) or install a
CNI of your choice.

## 5. Clean up

```bash
kubectl delete cluster my-cluster
```

Deleting the `Cluster` cascades to everything it owns, and the provider shuts
down and removes the VMs. Watch it happen in Xen Orchestra, or follow the logs:

```bash
kubectl -n capi-system logs -l app.kubernetes.io/name=vates-capi -f
```

## Where next

- Choose a different bootstrap: the [Talos guide](../guides/providers/talos/index.md).
- Tune networking, kube-vip, or SSH key injection: the [Guides](../guides/providers/kubeadm.md).
- Look up every field: [Reference → Custom resources](../reference/crds/index.md).
