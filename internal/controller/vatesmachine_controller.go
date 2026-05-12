package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "git.vates.tech/patrice.ferlet/vates-capi/api/v1beta2"
	vatesmachine "git.vates.tech/patrice.ferlet/vates-capi/internal/controller/vatesmachine"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
)

const (
	vatesMachineFinalizer = "vates.infrastructure.cluster.x-k8s.io/vatesmachine"
)

// VatesMachineReconciler reconciles a VatesMachine object.
type VatesMachineReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	XoCreds  *xok8scommon.XoConfig
	xoClient *xok8scommon.XoClient
}

var _ reconcile.Reconciler = (*VatesMachineReconciler)(nil)

// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=vatesmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=vatesmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=vatesmachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=vatesmachinetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinesets;machinesets/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments;machinedeployments/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

func (r *VatesMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	vatesMachine := &infrastructurev1beta2.VatesMachine{}
	if err := r.Get(ctx, req.NamespacedName, vatesMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !vatesMachine.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, vatesMachine)
	}

	return r.reconcileNormal(ctx, vatesMachine)
}

// SetupWithManager sets up the controller with the Manager.
func (r *VatesMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1beta2.VatesMachine{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(r.machineToVatesMachine),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.secretToVatesMachine),
		).
		Named("vatesmachine").
		Complete(r)
}

func (r *VatesMachineReconciler) machineToVatesMachine(ctx context.Context, o client.Object) []reconcile.Request {
	machine, ok := o.(*clusterv1.Machine)
	if !ok {
		return nil
	}
	if machine.Spec.InfrastructureRef.Kind != "VatesMachine" {
		return nil
	}
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: machine.Namespace,
				Name:      machine.Spec.InfrastructureRef.Name,
			},
		},
	}
}

// secretToVatesMachine maps a Secret change to the VatesMachine associated
// with its owner Machine. Registered as a Watch map function in SetupWithManager
// so that bootstrap data Secret updates trigger reconciliation of the
// corresponding VatesMachine.
func (r *VatesMachineReconciler) secretToVatesMachine(ctx context.Context, o client.Object) []reconcile.Request {
	secret, ok := o.(*corev1.Secret)
	if !ok {
		return nil
	}
	ownerRefs := secret.GetOwnerReferences()
	for _, ref := range ownerRefs {
		if ref.Kind == "Machine" && ref.APIVersion == clusterv1.GroupVersion.String() {
			machine := &clusterv1.Machine{}
			if err := r.Get(ctx, types.NamespacedName{Namespace: secret.Namespace, Name: ref.Name}, machine); err != nil {
				return nil
			}
			if machine.Spec.InfrastructureRef.Kind == "VatesMachine" {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Namespace: machine.Namespace,
							Name:      machine.Spec.InfrastructureRef.Name,
						},
					},
				}
			}
		}
	}
	return nil
}

// patchNodeProviderID reads the workload cluster kubeconfig and patches the Node
// with the Xen Orchestra VM providerID.
func (r *VatesMachineReconciler) patchNodeProviderID(ctx context.Context, vatesMachine *infrastructurev1beta2.VatesMachine) error {
	logger := log.FromContext(ctx)
	machine, err := vatesmachine.GetOwnerMachine(ctx, r.Client, vatesMachine)
	if err != nil {
		return fmt.Errorf("get owner machine: %w", err)
	}
	if machine == nil {
		return nil
	}

	clusterName := machine.Labels["cluster.x-k8s.io/cluster-name"]
	if clusterName == "" {
		return nil
	}

	secretName := fmt.Sprintf("%s-kubeconfig", clusterName)
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: vatesMachine.Namespace, Name: secretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("kubeconfig secret %s not found (cluster not ready yet)", secretName)
		}
		return fmt.Errorf("get kubeconfig secret: %w", err)
	}

	kubeconfigData, ok := secret.Data["value"]
	if !ok {
		return fmt.Errorf("kubeconfig secret has no 'value' key")
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return fmt.Errorf("parse kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list workload nodes: %w", err)
	}

	for i := range nodes.Items {
		if nodes.Items[i].Spec.ProviderID == *vatesMachine.Status.ProviderID {
			logger.V(1).Info("Node already has correct providerID", "node", nodes.Items[i].Name)
			return nil
		}
	}

	var targetNode *corev1.Node
	for i := range nodes.Items {
		if nodes.Items[i].Spec.ProviderID == "" {
			targetNode = &nodes.Items[i]
			break
		}
	}

	if targetNode == nil && machine.Status.NodeRef.Name != "" {
		for i := range nodes.Items {
			if nodes.Items[i].Name == machine.Status.NodeRef.Name {
				targetNode = &nodes.Items[i]
				break
			}
		}
	}

	if targetNode == nil {
		return fmt.Errorf("no node found to patch (nodes may not have registered yet)")
	}

	patchData := fmt.Sprintf(`{"spec":{"providerID":"%s"}}`, *vatesMachine.Status.ProviderID)
	if _, err := clientset.CoreV1().Nodes().Patch(ctx, targetNode.Name, types.StrategicMergePatchType, []byte(patchData), metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch node %s: %w", targetNode.Name, err)
	}

	logger.Info("Patched Node with providerID", "node", targetNode.Name, "providerID", *vatesMachine.Status.ProviderID)
	return nil
}
