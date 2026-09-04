package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
)

const (
	xoMachineFinalizer = "vates.infrastructure.cluster.x-k8s.io/xomachine"
)

// XOMachineReconciler reconciles a XOMachine object.
type XOMachineReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	XoCreds       *xok8scommon.XoConfig
	newClientFunc func(ctx context.Context, cfg *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) // overridable for tests
}

var _ reconcile.Reconciler = (*XOMachineReconciler)(nil)

// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=xomachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=xomachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=xomachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=vates.infrastructure.cluster.x-k8s.io,resources=xomachinetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinesets;machinesets/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments;machinedeployments/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

func (r *XOMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	xoMachine := &infrastructurev1beta2.XOMachine{}
	if err := r.Get(ctx, req.NamespacedName, xoMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !xoMachine.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, xoMachine)
	}

	return r.reconcileNormal(ctx, xoMachine)
}

// SetupWithManager sets up the controller with the Manager.
func (r *XOMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1beta2.XOMachine{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(r.machineToXOMachine),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.secretToXOMachine),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5}).
		Named("xomachine").
		Complete(r)
}

func (r *XOMachineReconciler) machineToXOMachine(ctx context.Context, o client.Object) []reconcile.Request {
	machine, ok := o.(*clusterv1.Machine)
	if !ok {
		return nil
	}
	if machine.Spec.InfrastructureRef.Kind != infrastructurev1beta2.KindXOMachine {
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

// secretToXOMachine maps a Secret change to the XOMachine associated
// with its owner Machine. Registered as a Watch map function in SetupWithManager
// so that bootstrap data Secret updates trigger reconciliation of the
// corresponding XOMachine.
func (r *XOMachineReconciler) secretToXOMachine(ctx context.Context, o client.Object) []reconcile.Request {
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
			if machine.Spec.InfrastructureRef.Kind == infrastructurev1beta2.KindXOMachine {
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
