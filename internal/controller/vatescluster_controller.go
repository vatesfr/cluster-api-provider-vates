package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1beta2 "git.vates.tech/patrice.ferlet/vates-capi/api/v1beta2"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
)

const (
	vatesClusterFinalizer = "vates.infrastructure.cluster.x-k8s.io/vatescluster"
)

// VatesClusterReconciler reconciles a VatesCluster object.
type VatesClusterReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	XoCreds *xok8scommon.XoConfig
}

var _ reconcile.Reconciler = (*VatesClusterReconciler)(nil)

// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=vatesclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=vatesclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=vatesclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=vatesclustertemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *VatesClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	vatesCluster := &infrastructurev1beta2.VatesCluster{}
	if err := r.Get(ctx, req.NamespacedName, vatesCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !vatesCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, vatesCluster)
	}

	return r.reconcileNormal(ctx, vatesCluster)
}

func (r *VatesClusterReconciler) reconcileNormal(ctx context.Context, vatesCluster *infrastructurev1beta2.VatesCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	controllerutil.AddFinalizer(vatesCluster, vatesClusterFinalizer)
	if err := r.Update(ctx, vatesCluster); err != nil {
		logger.Error(err, "Failed to add finalizer")
		return ctrl.Result{}, err
	}

	cluster, err := r.getOwnerCluster(ctx, vatesCluster)
	if err != nil {
		logger.Error(err, "Failed to get owner Cluster")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		logger.Info("Waiting for owner Cluster to be created")
		return ctrl.Result{}, nil
	}

	// -------------------------------------------------------------------------
	// CONTROL PLANE ENDPOINT
	// Priority order:
	// 1. spec.controlPlaneEndpoint (set by user or ClusterClass topology)
	// 2. Discover from a ready control plane VatesMachine (backward compat)
	// -------------------------------------------------------------------------
	needsRequeue := false
	if vatesCluster.Status.ControlPlaneEndpoint == nil || vatesCluster.Status.ControlPlaneEndpoint.Host == "" {
		ep := vatesCluster.Spec.ControlPlaneEndpoint
		if ep != nil && ep.Host != "" {
			vatesCluster.Status.ControlPlaneEndpoint = ep
			logger.Info("Control plane endpoint set from spec", "host", ep.Host, "port", ep.Port)
		} else {
			endpoint, err := r.discoverControlPlaneEndpoint(ctx, cluster)
			if err != nil {
				logger.Error(err, "Failed to discover control plane endpoint")
				return ctrl.Result{}, err
			}
			if endpoint != nil {
				vatesCluster.Status.ControlPlaneEndpoint = endpoint
				logger.Info("Control plane endpoint discovered from machine", "host", endpoint.Host, "port", endpoint.Port)
			} else {
				logger.Info("Waiting for a ready control plane machine to discover endpoint")
				needsRequeue = true
			}
		}
	}

	// Mark infrastructure ready so CAPI can proceed with machine creation
	vatesCluster.Status.Ready = true
	vatesCluster.Status.Initialization = &infrastructurev1beta2.InitializationStatus{Provisioned: true}
	r.setCondition(vatesCluster, "Ready", metav1.ConditionTrue, "ClusterInfrastructureReady", "Cluster infrastructure is ready")

	if err := r.Status().Update(ctx, vatesCluster); err != nil {
		logger.Error(err, "Failed to update VatesCluster status")
		return ctrl.Result{}, err
	}

	// CAUTION: ControlPlaneEndpoint may be nil if not yet discovered.
	// Only log the host if the endpoint exists to avoid a nil pointer panic.
	if vatesCluster.Status.ControlPlaneEndpoint != nil {
		logger.Info("VatesCluster reconciled", "name", vatesCluster.Name, "endpoint", vatesCluster.Status.ControlPlaneEndpoint.Host)
	} else {
		logger.Info("VatesCluster reconciled", "name", vatesCluster.Name, "endpoint", "not yet discovered")
	}

	// If the endpoint has not been discovered yet, requeue to re-check
	// in 10 seconds (time for the VM to boot and guest tools to report the IP).
	if needsRequeue {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *VatesClusterReconciler) reconcileDelete(ctx context.Context, vatesCluster *infrastructurev1beta2.VatesCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(vatesCluster, vatesClusterFinalizer) {
		logger.Info("Cleaning up VatesCluster", "name", vatesCluster.Name)

		controllerutil.RemoveFinalizer(vatesCluster, vatesClusterFinalizer)
		if err := r.Update(ctx, vatesCluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *VatesClusterReconciler) getOwnerCluster(ctx context.Context, vatesCluster *infrastructurev1beta2.VatesCluster) (*clusterv1.Cluster, error) {
	clusterList := &clusterv1.ClusterList{}
	if err := r.List(ctx, clusterList, client.InNamespace(vatesCluster.Namespace)); err != nil {
		return nil, err
	}

	for i := range clusterList.Items {
		c := &clusterList.Items[i]
		if c.Spec.InfrastructureRef.Kind == "VatesCluster" &&
			c.Spec.InfrastructureRef.Name == vatesCluster.Name {
			return c, nil
		}
	}

	return nil, nil
}

func (r *VatesClusterReconciler) discoverControlPlaneEndpoint(ctx context.Context, cluster *clusterv1.Cluster) (*infrastructurev1beta2.APIEndpoint, error) {
	machineList := &clusterv1.MachineList{}
	if err := r.List(
		ctx, machineList,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels{clusterv1.MachineControlPlaneLabel: ""},
	); err != nil {
		return nil, fmt.Errorf("failed to list control plane machines: %w", err)
	}

	for i := range machineList.Items {
		m := &machineList.Items[i]
		if m.Spec.ClusterName != cluster.Name {
			continue
		}

		if m.Spec.InfrastructureRef.Kind != infrastructurev1beta2.KindVatesMachine {
			continue
		}

		vatesMachine := &infrastructurev1beta2.VatesMachine{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: m.Namespace,
			Name:      m.Spec.InfrastructureRef.Name,
		}, vatesMachine); err != nil {
			continue
		}

		if vatesMachine.Status.Ready && len(vatesMachine.Status.Addresses) > 0 {
			for _, addr := range vatesMachine.Status.Addresses {
				if addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP {
					return &infrastructurev1beta2.APIEndpoint{
						Host: addr.Address,
						Port: 6443,
					}, nil
				}
			}
		}
	}

	return nil, nil
}

func (r *VatesClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1beta2.VatesCluster{}).
		Named("vatescluster").
		Complete(r)
}

// setCondition updates or adds a condition in the VatesCluster's Conditions slice.
func (r *VatesClusterReconciler) setCondition(vatesCluster *infrastructurev1beta2.VatesCluster, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i, c := range vatesCluster.Status.Conditions {
		if c.Type == conditionType {
			vatesCluster.Status.Conditions[i].Status = status
			vatesCluster.Status.Conditions[i].Reason = reason
			vatesCluster.Status.Conditions[i].Message = message
			vatesCluster.Status.Conditions[i].LastTransitionTime = now
			return
		}
	}
	vatesCluster.Status.Conditions = append(vatesCluster.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}
