package controller

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"text/template"
	"time"

	"k8s.io/apimachinery/pkg/runtime"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	addonsv1 "sigs.k8s.io/cluster-api/api/addons/v1beta2" // ClusterResourceSet

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
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
// +kubebuilder:rbac:groups=addons.cluster.x-k8s.io,resources=clusterresourcesets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

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

	if controllerutil.AddFinalizer(vatesCluster, vatesClusterFinalizer) {
		if err := r.Update(ctx, vatesCluster); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	patchBase := vatesCluster.DeepCopy()

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

	if !needsRequeue {
		vatesCluster.Status.Ready = true
		vatesCluster.Status.Initialization = &infrastructurev1beta2.InitializationStatus{Provisioned: true}
		r.setCondition(vatesCluster, "Ready", metav1.ConditionTrue, "ClusterInfrastructureReady", "Cluster infrastructure is ready")
	} else {
		r.setCondition(vatesCluster, "Ready", metav1.ConditionFalse, "WaitingForControlPlaneEndpoint", "Waiting for a ready control plane machine to discover the endpoint")
	}

	if err := r.Status().Patch(ctx, vatesCluster, client.MergeFrom(patchBase)); err != nil {
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

	// Ensure CCM ConfigMap + CRS exist for automatic CCM deployment into
	// workload clusters. This uses the same xo-credentials secret as the
	// management controller — no extra configuration needed.
	if err := r.reconcileCCM(ctx); err != nil {
		logger.Error(err, "Failed to reconcile CCM resources, will retry")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *VatesClusterReconciler) reconcileDelete(ctx context.Context, vatesCluster *infrastructurev1beta2.VatesCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(vatesCluster, vatesClusterFinalizer) {
		logger.Info("Removing finalizer from VatesCluster", "name", vatesCluster.Name)

		controllerutil.RemoveFinalizer(vatesCluster, vatesClusterFinalizer)
		if err := r.Update(ctx, vatesCluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *VatesClusterReconciler) getOwnerCluster(ctx context.Context, vatesCluster *infrastructurev1beta2.VatesCluster) (*clusterv1.Cluster, error) {
	ownerRef := metav1.GetControllerOf(vatesCluster)
	if ownerRef == nil {
		return nil, nil
	}

	cluster := &clusterv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: vatesCluster.Namespace,
		Name:      ownerRef.Name,
	}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	if cluster.UID != ownerRef.UID {
		return nil, nil
	}
	return cluster, nil
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
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(r.clusterToVatesCluster),
		).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(r.machineToVatesCluster),
		).
		Named("vatescluster").
		Complete(r)
}

// clusterToVatesCluster maps a Cluster change to the corresponding VatesCluster.
func (r *VatesClusterReconciler) clusterToVatesCluster(_ context.Context, o client.Object) []reconcile.Request {
	cluster, ok := o.(*clusterv1.Cluster)
	if !ok {
		return nil
	}
	if cluster.Spec.InfrastructureRef.Kind != "VatesCluster" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Spec.InfrastructureRef.Name,
		},
	}}
}

// machineToVatesCluster maps a control plane Machine change to the corresponding
// VatesCluster, so that endpoint discovery is re-triggered when a machine becomes Ready.
func (r *VatesClusterReconciler) machineToVatesCluster(ctx context.Context, o client.Object) []reconcile.Request {
	machine, ok := o.(*clusterv1.Machine)
	if !ok {
		return nil
	}
	if _, ok := machine.Labels[clusterv1.MachineControlPlaneLabel]; !ok {
		return nil
	}
	clusterName, ok := machine.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		return nil
	}
	cluster := &clusterv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: machine.Namespace, Name: clusterName}, cluster); err != nil {
		return nil
	}
	if cluster.Spec.InfrastructureRef.Kind != "VatesCluster" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Spec.InfrastructureRef.Name,
		},
	}}
}

// ccmManifestTemplate is the CCM deployment manifest rendered with XO credentials.
//
//go:embed ccm-manifest.yaml
var ccmManifestTemplate string

// ccmManifestData holds the template values for the CCM manifest.
type ccmManifestData struct {
	XOAURL      string
	XOAToken    string
	XOAInsecure string
}

// reconcileCCM creates the CCM ConfigMap + ClusterResourceSet so that
// every workload cluster gets the Xen Orchestra Cloud Controller Manager
// automatically deployed. It reads the XO credentials from the management
// cluster's xo-credentials secret.
func (r *VatesClusterReconciler) reconcileCCM(ctx context.Context) error {
	logger := log.FromContext(ctx)

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: "capi-system", Name: "xo-credentials"}, secret); err != nil {
		return fmt.Errorf("get xo-credentials secret: %w", err)
	}

	data := ccmManifestData{
		XOAURL:      string(secret.Data["url"]),
		XOAToken:    string(secret.Data["token"]),
		XOAInsecure: string(secret.Data["insecure"]),
	}
	if data.XOAInsecure == "" {
		data.XOAInsecure = "true"
	}

	tmpl, err := template.New("ccm").Parse(ccmManifestTemplate)
	if err != nil {
		return fmt.Errorf("parse CCM template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute CCM template: %w", err)
	}

	// Create or update the ConfigMap
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "ccm-manifests", Namespace: "default"}}
	op, err := ctrl.CreateOrUpdate(ctx, r.Client, cm, func() error {
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}
		cm.Data["ccm.yaml"] = buf.String()
		return nil
	})
	if err != nil {
		return fmt.Errorf("create/update ConfigMap ccm-manifests: %w", err)
	}
	logger.Info("CCM manifests ConfigMap reconciled", "operation", op)

	// Create or update the ClusterResourceSet
	crs := &addonsv1.ClusterResourceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ccm-deployment", Namespace: "default"},
	}
	op, err = ctrl.CreateOrUpdate(ctx, r.Client, crs, func() error {
		crs.Spec = addonsv1.ClusterResourceSetSpec{
			ClusterSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"topology.cluster.x-k8s.io/owned": ""},
			},
			Resources: []addonsv1.ResourceRef{
				{Kind: "ConfigMap", Name: "ccm-manifests"},
			},
			Strategy: "ApplyOnce",
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("create/update ClusterResourceSet ccm-deployment: %w", err)
	}
	logger.Info("CCM ClusterResourceSet reconciled", "operation", op)

	return nil
}

// setCondition updates or adds a condition in the VatesCluster's Conditions slice.
// LastTransitionTime is only updated when the condition status changes.
func (r *VatesClusterReconciler) setCondition(vatesCluster *infrastructurev1beta2.VatesCluster, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i, c := range vatesCluster.Status.Conditions {
		if c.Type == conditionType {
			// Only update LastTransitionTime when the status actually changes
			if c.Status != status {
				vatesCluster.Status.Conditions[i].LastTransitionTime = now
			}
			vatesCluster.Status.Conditions[i].Status = status
			vatesCluster.Status.Conditions[i].Reason = reason
			vatesCluster.Status.Conditions[i].Message = message
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
