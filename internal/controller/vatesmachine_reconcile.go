package controller

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"

	infrastructurev1beta2 "git.vates.tech/patrice.ferlet/vates-capi/api/v1beta2"
	vatesmachine "git.vates.tech/patrice.ferlet/vates-capi/internal/controller/vatesmachine"
)

func (r *VatesMachineReconciler) reconcileNormal(ctx context.Context, vatesMachine *infrastructurev1beta2.VatesMachine) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.AddFinalizer(vatesMachine, vatesMachineFinalizer) {
		if err := r.Update(ctx, vatesMachine); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	if result, err := r.tryFastPath(ctx, vatesMachine); result != nil {
		return *result, err
	}

	bsResult, err := vatesmachine.ResolveBootstrapData(ctx, r.Client, vatesMachine)
	if err != nil {
		return ctrl.Result{}, err
	}
	if bsResult.Requeue {
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	xoClient, err := vatesmachine.GetOrCreateXOClient(ctx, r.XoCreds, r.xoClient)
	if err != nil {
		logger.Error(err, "Failed to create/connect XO client")
		if updateErr := vatesmachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "VatesConnectionFailed", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}
	if xoClient == nil {
		logger.Info("XO credentials not configured, skipping VM provisioning")
		return ctrl.Result{}, nil
	}

	cloudConfig, err := vatesmachine.BuildCloudConfig(ctx, xoClient, bsResult.Data)
	if err != nil {
		logger.Error(err, "Failed to build cloud config")
		if updateErr := vatesmachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "CloudConfigBuildFailed", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	cloudConfig, err = vatesmachine.MaybeInjectKubeVIP(ctx, r.Client, vatesMachine, bsResult.Machine, cloudConfig)
	if err != nil {
		return ctrl.Result{}, err
	}

	vmName := vatesMachine.Spec.NamePrefix
	templateID, err := vatesmachine.ResolveTemplateID(ctx, xoClient, vatesMachine.Spec.TemplateID, vatesMachine.Spec.TemplateName)
	if err != nil {
		logger.Error(err, "Failed to find template", "templateID", vatesMachine.Spec.TemplateID, "templateName", vatesMachine.Spec.TemplateName)
		if updateErr := vatesmachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "TemplateNotFound", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	poolID, err := vatesmachine.ResolvePoolID(ctx, xoClient, vatesMachine.Spec.PoolID, vatesMachine.Spec.PoolName)
	if err != nil {
		logger.Error(err, "Failed to find pool", "poolID", vatesMachine.Spec.PoolID, "poolName", vatesMachine.Spec.PoolName)
		if updateErr := vatesmachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "PoolNotFound", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	var vm *payloads.VM
	if vatesMachine.Status.ProviderID != nil && *vatesMachine.Status.ProviderID != "" {
		vmID, parseErr := xok8scommon.GetVMID(*vatesMachine.Status.ProviderID)
		if parseErr != nil {
			logger.Error(parseErr, "Failed to parse existing providerID")
			if updateErr := vatesmachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "InvalidProviderID", parseErr.Error()); updateErr != nil {
				logger.Error(updateErr, "Failed to update condition")
			}
			return ctrl.Result{}, parseErr
		}
		vm, err = vatesmachine.LookupExistingVM(ctx, r.Client, xoClient, vatesMachine, vmID)
	} else {
		vm, err = vatesmachine.CreateVM(ctx, r.Client, xoClient, vatesMachine, poolID, templateID, cloudConfig, vmName)
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if !vatesMachine.Status.Ready {
		result, waitErr := vatesmachine.WaitForVMReady(ctx, r.Client, xoClient, vatesMachine, vm)
		if waitErr != nil || !result.IsZero() {
			return result, waitErr
		}
	}

	if vatesMachine.Status.ProviderID != nil && *vatesMachine.Status.ProviderID != "" {
		if pErr := r.patchNodeProviderID(ctx, vatesMachine); pErr != nil {
			logger.Error(pErr, "Failed to patch Node providerID (will retry)")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	logger.Info("VatesMachine reconciled", "name", vatesMachine.Name, "providerID", *vatesMachine.Status.ProviderID)
	return ctrl.Result{}, nil
}

// tryFastPath returns a non-nil result when the VM already has a providerID
// and exists in XO, meaning we can skip directly to the existing-VM path.
func (r *VatesMachineReconciler) tryFastPath(ctx context.Context, vatesMachine *infrastructurev1beta2.VatesMachine) (*ctrl.Result, error) {
	if vatesMachine.Status.ProviderID == nil || *vatesMachine.Status.ProviderID == "" {
		return nil, nil
	}

	vmID, parseErr := xok8scommon.GetVMID(*vatesMachine.Status.ProviderID)
	if parseErr != nil {
		return nil, nil
	}

	xoClient := r.xoClient
	if xoClient == nil && r.XoCreds != nil {
		xoClient, _ = xok8scommon.NewXOClient(r.XoCreds)
	}
	if xoClient == nil {
		return nil, nil
	}

	vm, fetchErr := xoClient.Client.VM().GetByID(ctx, vmID)
	if fetchErr != nil {
		log.FromContext(ctx).Error(fetchErr, "Failed to re-fetch existing VM, falling back to full reconcile")
		return nil, nil
	}

	result, err := r.reconcileExistingVM(ctx, vatesMachine, vm, xoClient)
	return &result, err
}

// reconcileExistingVM handles the fast-path when the VM already exists.
// It skips bootstrap data assembly, cloud-config building, VM creation,
// CPU/boot-order configuration, and providerID assignment — all already done.
func (r *VatesMachineReconciler) reconcileExistingVM(ctx context.Context, vatesMachine *infrastructurev1beta2.VatesMachine, vm *payloads.VM, xoClient *xok8scommon.XoClient) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !vatesMachine.Status.Ready {
		var err error
		vm, _, err = vatesmachine.StartVM(ctx, r.Client, xoClient, vatesMachine, vm)
		if err != nil {
			return ctrl.Result{}, err
		}

		vmIP := vatesmachine.ResolveVMIP(ctx, xoClient, vm)

		if vmIP == "" {
			logger.Info("VM existing but IP not yet reported, requeuing", "id", vm.ID.String())
			if ue := vatesmachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "WaitingForIPAddress", "VM is running, waiting for IP address from Xen guest tools"); ue != nil {
				return ctrl.Result{}, ue
			}
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}

		if err := vatesmachine.ManageVIFs(ctx, xoClient, vatesMachine, vm); err != nil {
			logger.Error(err, "Failed to manage VIFs")
			if ue := vatesmachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "VifManagementFailed", err.Error()); ue != nil {
				return ctrl.Result{}, ue
			}
			return ctrl.Result{}, err
		}

		vatesMachine.Status.Addresses = []corev1.NodeAddress{
			{
				Type:    corev1.NodeInternalIP,
				Address: vmIP,
			},
		}
		vatesMachine.Status.Ready = true
		vatesMachine.Status.Initialization = &infrastructurev1beta2.InitializationStatus{Provisioned: true}
		if ue := vatesmachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionTrue, "VmReady", "VM is created, running and has an IP address"); ue != nil {
			return ctrl.Result{}, ue
		}
	}

	if vatesMachine.Status.ProviderID != nil && *vatesMachine.Status.ProviderID != "" {
		if err := r.patchNodeProviderID(ctx, vatesMachine); err != nil {
			logger.Error(err, "Failed to patch Node providerID (will retry)")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	logger.Info("VatesMachine reconciled (fast path)", "name", vatesMachine.Name, "providerID", *vatesMachine.Status.ProviderID)
	return ctrl.Result{}, nil
}

func (r *VatesMachineReconciler) reconcileDelete(ctx context.Context, vatesMachine *infrastructurev1beta2.VatesMachine) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(vatesMachine, vatesMachineFinalizer) {
		return ctrl.Result{}, nil
	}

	if r.XoCreds == nil && r.xoClient == nil {
		logger.Info("XO credentials not configured, removing finalizer")
		controllerutil.RemoveFinalizer(vatesMachine, vatesMachineFinalizer)
		if err := r.Update(ctx, vatesMachine); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if vatesMachine.Status.ProviderID != nil && *vatesMachine.Status.ProviderID != "" {
		vmID, err := xok8scommon.GetVMID(*vatesMachine.Status.ProviderID)
		if err != nil {
			logger.Error(err, "Failed to parse providerID, removing finalizer")
			controllerutil.RemoveFinalizer(vatesMachine, vatesMachineFinalizer)
			if updateErr := r.Update(ctx, vatesMachine); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, nil
		}

		xoClient := r.xoClient
		if xoClient == nil {
			var err error
			xoClient, err = xok8scommon.NewXOClient(r.XoCreds)
			if err != nil {
				logger.Error(err, "Failed to create XO client for deletion")
				return ctrl.Result{}, err
			}
		}

		if _, err := xoClient.Client.VM().Start(ctx, vmID, nil); err == nil {
			_, _ = xoClient.Client.VM().CleanShutdown(ctx, vmID)
		}

		if err := xoClient.Client.VM().Delete(ctx, vmID); err != nil {
			if strings.Contains(err.Error(), "no such VM") {
				logger.Info("VM already deleted, cleaning up finalizer", "id", vmID.String())
			} else {
				logger.Error(err, "Failed to delete VM", "id", vmID.String())
				return ctrl.Result{}, err
			}
		}

		logger.Info("VM deleted", "id", vmID.String())
	}

	controllerutil.RemoveFinalizer(vatesMachine, vatesMachineFinalizer)
	if err := r.Update(ctx, vatesMachine); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
