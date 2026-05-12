# ClusterClass Templates

Two ClusterClasses for **AlmaLinux 10 / RHEL-like** nodes on the Vates infrastructure provider:

| ClusterClass | Control plane default | Worker flavors |
|---|---|---|
| `vates-rhel-from-scratch` | Full OS bootstrap (containerd, kubelet, kubeadm installed at provisioning time) | `worker-from-scratch`, `worker-prefilled` |
| `vates-rhel-prefilled` | Pre-baked image (Packer-built with containerd, kubelet, kubeadm pre-installed) | `worker-from-scratch`, `worker-prefilled` |

Both ClusterClasses expose the same two machine deployment classes:
- **`worker-from-scratch`** — bootstraps the node from minimal OS (installs containerd, kubelet, kubeadm via cloud-init)
- **`worker-prefilled`** — uses a pre-baked image (expects containerd, kubelet, kubeadm already present)

## Apply once

```bash
# 1. ClusterClass + control plane/bootstrap templates
kubectl apply -k config/samples/clusterclass/

# 2. Machine templates (edit templateID/poolID/network first)
kubectl apply -k config/samples/machinetemplates/
```

Then create a cluster:

```bash
kubectl apply -k config/samples/example-cluster/
```

## Directory layout

```
config/samples/
├── clusterclass/                          # ClusterClass + non-machine templates
│   ├── kustomization.yaml
│   ├── rhel-vatesclustertemplate.yaml     # shared VatesClusterTemplate
│   ├── from-scratch/
│   │   ├── clusterclass-rhel-from-scratch.yaml
│   │   ├── rhel-from-scratch-control-plane.yaml
│   │   └── rhel-from-scratch-worker.yaml
│   └── prefilled/
│       ├── clusterclass-rhel-prefilled.yaml
│       ├── rhel-prefilled-control-plane.yaml
│       └── rhel-prefilled-worker.yaml
└── machinetemplates/                      # VatesMachineTemplate (edit per environment)
    ├── kustomization.yaml
    ├── from-scratch/
    │   ├── kustomization.yaml
    │   ├── rhel-vatesmachinetemplate-cp-from-scratch.yaml
    │   └── rhel-vatesmachinetemplate-worker-from-scratch.yaml
    └── prefilled/
        ├── kustomization.yaml
        ├── rhel-vatesmachinetemplate-cp-prefilled.yaml
        └── rhel-vatesmachinetemplate-worker-prefilled.yaml
```

## Customising for your Xen Orchestra environment

The VatesMachineTemplate manifests in `config/samples/machinetemplates/` reference VM template UUIDs, pool IDs, and network names that are specific to a given XenServer / XCP-ng pool. Before applying, update the following fields in each file:

| Field | Description |
|---|---|
| `spec.template.spec.templateID` | UUID of the VM template (e.g. the Packer-built AlmaLinux 10 image) |
| `spec.template.spec.poolID` | UUID of your Xen Orchestra pool |
| `spec.template.spec.networkConfig.networks[].name` | Name of the network attached to the VM |

These values are placeholders and will differ in your environment.
