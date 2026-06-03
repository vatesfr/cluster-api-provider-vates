# ClusterClass Templates

A single ClusterClass for **AlmaLinux 10 / RHEL-like** nodes on the Vates infrastructure provider:

| ClusterClass | Worker flavor |
|---|---|
| `vates-rhel-from-scratch` | `worker-from-scratch` |

Kubelet/kubeadm/kubectl are downloaded dynamically at bootstrap time from `https://dl.k8s.io/release/`, matching the version specified in the Cluster's `spec.topology.version`.

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
│   └── from-scratch/
│       ├── clusterclass-rhel-from-scratch.yaml
│       ├── rhel-from-scratch-control-plane.yaml
│       └── rhel-from-scratch-worker.yaml
└── machinetemplates/                      # VatesMachineTemplate (edit per environment)
    ├── kustomization.yaml
    └── from-scratch/
        ├── kustomization.yaml
        ├── rhel-vatesmachinetemplate-cp-from-scratch.yaml
        └── rhel-vatesmachinetemplate-worker-from-scratch.yaml
```

## Customising for your Xen Orchestra environment

The VatesMachineTemplate manifests in `config/samples/machinetemplates/` reference VM template UUIDs, pool IDs, and network names that are specific to a given XenServer / XCP-ng pool. Before applying, update the following fields in each file:

| Field | Description |
|---|---|
| `spec.template.spec.templateID` | UUID of the VM template (AlmaLinux 10 with containerd and Xen guest tools) |
| `spec.template.spec.poolID` | UUID of your Xen Orchestra pool |
| `spec.template.spec.networkConfig.networks[].name` | Name of the network attached to the VM |

These values are placeholders and will differ in your environment.
