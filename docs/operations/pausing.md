# Pausing reconciliation

Run these commands against the management cluster. To pause the cluster and all
its XO infrastructure, set `spec.paused` on the CAPI `Cluster`:

```bash
kubectl -n <namespace> patch cluster <cluster> --type=merge -p '{"spec":{"paused":true}}'
# Resume
kubectl -n <namespace> patch cluster <cluster> --type=merge -p '{"spec":{"paused":false}}'
```

To pause one machine, annotate its CAPI `Machine`. This provider also checks the
owner Machine annotation before reconciling its `XOMachine`:

```bash
kubectl -n <namespace> annotate machine <machine> cluster.x-k8s.io/paused="" --overwrite
# Resume
kubectl -n <namespace> annotate machine <machine> cluster.x-k8s.io/paused-
```

The same annotation can be applied directly to an `XOMachine` or `XOCluster` to
pause that infrastructure object only. Its presence pauses reconciliation even
when its value is `"false"`; remove it to resume. The `Paused` status condition
becomes `True` while paused and `False` after resuming. If multiple pause sources
apply, all must be lifted to resume.

Pause blocks subsequent provisioning, updates, addon reconciliation and deletion
(including finalizer removal). It does not stop running VMs, workloads or XO
tasks already started. Pausing a Machine does not pause its MachineSet,
MachineDeployment, control plane or MachineHealthCheck controllers.
