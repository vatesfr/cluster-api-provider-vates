package controller

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
	"github.com/vatesfr/cluster-api-provider-vates/internal/bootstrap"
	xomachine "github.com/vatesfr/cluster-api-provider-vates/internal/controller/xomachine"
)

func (r *XOMachineReconciler) reconcileNormal(ctx context.Context, vatesMachine *infrastructurev1beta2.XOMachine) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := r.ensureFinalizer(ctx, vatesMachine); err != nil {
		return ctrl.Result{}, err
	}

	if result, err := r.tryFastPath(ctx, vatesMachine); result != nil {
		return *result, err
	}

	bsResult, err := bootstrap.ResolveBootstrapData(ctx, r.Client, vatesMachine)
	if err != nil {
		return ctrl.Result{}, err
	}
	if bsResult.Requeue {
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	xoCreds, err := r.resolveMachineCredentials(ctx, vatesMachine)
	if err != nil {
		logger.Error(err, "Failed to resolve XO credentials")
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "CredentialsResolutionFailed", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	xoClient, err := r.newXOClient(ctx, xoCreds)
	if err != nil {
		logger.Error(err, "Failed to create/connect XO client")
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "XOConnectionFailed", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}
	if xoClient == nil {
		logger.Info("XO credentials not configured, skipping VM provisioning")
		return ctrl.Result{}, nil
	}

	provider := bootstrap.GetProvider(bsResult.BootstrapProvider)
	deps := bootstrap.Dependencies{
		Client:        r.Client,
		XOClient:      xoClient,
		Machine:       bsResult.Machine,
		XOMachine:     vatesMachine,
		BootstrapData: bsResult.Data,
	}

	cloudConfig, err := provider.BuildCloudConfig(ctx, deps)
	if err != nil {
		logger.Error(err, "Failed to build cloud config")
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "CloudConfigBuildFailed", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	networkConfig := provider.NetworkConfig(deps)

	vmName := r.buildVMName(vatesMachine, bsResult)

	templateID, err := xomachine.ResolveTemplateID(ctx, xoClient, vatesMachine.Spec.TemplateID, vatesMachine.Spec.TemplateName)
	if err != nil {
		logger.Error(err, "Failed to find template", "templateID", vatesMachine.Spec.TemplateID, "templateName", vatesMachine.Spec.TemplateName)
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "TemplateNotFound", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	poolID, err := xomachine.ResolvePoolID(ctx, xoClient, vatesMachine.Spec.PoolID, vatesMachine.Spec.PoolName)
	if err != nil {
		logger.Error(err, "Failed to find pool", "poolID", vatesMachine.Spec.PoolID, "poolName", vatesMachine.Spec.PoolName)
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "PoolNotFound", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	var vm *payloads.VM
	if vatesMachine.Status.ProviderID != nil && *vatesMachine.Status.ProviderID != "" {
		vmID, parseErr := xok8scommon.GetVMID(*vatesMachine.Status.ProviderID)
		if parseErr != nil {
			logger.Error(parseErr, "Failed to parse existing providerID")
			if updateErr := xomachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "InvalidProviderID", parseErr.Error()); updateErr != nil {
				logger.Error(updateErr, "Failed to update condition")
			}
			return ctrl.Result{}, parseErr
		}
		vm, err = xomachine.LookupExistingVM(ctx, r.Client, xoClient, vatesMachine, vmID)
	} else {
		vm, err = xomachine.CreateVM(ctx, r.Client, xoClient, vatesMachine, poolID, templateID, cloudConfig, networkConfig, vmName)
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	xomachine.SetVMTags(ctx, vatesMachine, vm.ID, xoClient)

	if !vatesMachine.Status.Ready {
		result, waitErr := xomachine.WaitForVMReady(ctx, r.Client, xoClient, vatesMachine, vm)
		if waitErr != nil || !result.IsZero() {
			return result, waitErr
		}
	}

	if vatesMachine.Status.ProviderID != nil && *vatesMachine.Status.ProviderID != "" {
		logger.Info("XOMachine reconciled", "name", vatesMachine.Name, "providerID", *vatesMachine.Status.ProviderID)
	}
	return ctrl.Result{}, nil
}

func (r *XOMachineReconciler) ensureFinalizer(ctx context.Context, vatesMachine *infrastructurev1beta2.XOMachine) error {
	if !controllerutil.AddFinalizer(vatesMachine, xoMachineFinalizer) {
		return nil
	}
	logger := log.FromContext(ctx)
	if err := r.Update(ctx, vatesMachine); err != nil {
		if !apierrors.IsConflict(err) {
			logger.Error(err, "Failed to add finalizer")
			return err
		}
		logger.Info("Conflict adding finalizer, retrying once")
		if err := r.Get(ctx, types.NamespacedName{Namespace: vatesMachine.Namespace, Name: vatesMachine.Name}, vatesMachine); err != nil {
			return err
		}
		if controllerutil.AddFinalizer(vatesMachine, xoMachineFinalizer) {
			if err := r.Update(ctx, vatesMachine); err != nil {
				logger.Error(err, "Failed to add finalizer after retry")
				return err
			}
		}
	}
	return nil
}

func (r *XOMachineReconciler) buildVMName(vatesMachine *infrastructurev1beta2.XOMachine, bsResult bootstrap.ResolveBootstrapDataResult) string {
	if bsResult.Machine == nil {
		return vatesMachine.Spec.NamePrefix
	}
	cn := bsResult.Machine.Labels["cluster.x-k8s.io/cluster-name"]
	if cn == "" {
		return vatesMachine.Spec.NamePrefix
	}
	role := ""
	np := vatesMachine.Spec.NamePrefix
	for _, s := range []string{"-cp", "-worker"} {
		if strings.HasSuffix(np, s) {
			role = s
			np = strings.TrimSuffix(np, s)
			break
		}
	}
	if suffix := lastDashSegment(bsResult.Machine.Name); suffix != "" {
		role += "-" + suffix
	}
	return np + cn + role
}

// lastDashSegment returns the last dash-separated segment of a name, e.g.
// "demo3-cp-7n62g" -> "7n62g". Used to make VM names unique per Machine.
func lastDashSegment(name string) string {
	if i := strings.LastIndex(name, "-"); i >= 0 {
		return name[i+1:]
	}
	return ""
}

// tryFastPath returns a non-nil result when the VM already has a providerID
// and exists in XO, meaning we can skip directly to the existing-VM path.
func (r *XOMachineReconciler) tryFastPath(ctx context.Context, vatesMachine *infrastructurev1beta2.XOMachine) (*ctrl.Result, error) {
	if vatesMachine.Status.ProviderID == nil || *vatesMachine.Status.ProviderID == "" {
		return nil, nil
	}

	if vatesMachine.Status.Ready {
		if !isConditionTrue(vatesMachine, "VmReady") {
			if ue := xomachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionTrue, "VmReady", "VM is created, running and has an IP address"); ue != nil {
				return &ctrl.Result{}, ue
			}
		}
		// Best-effort: keep VM tags up to date (also tags VMs created before
		// tagging was supported).
		if xoCreds, err := r.resolveMachineCredentials(ctx, vatesMachine); err == nil {
			if xoClient, err := r.newXOClient(ctx, xoCreds); err == nil && xoClient != nil {
				if vmID, parseErr := xok8scommon.GetVMID(*vatesMachine.Status.ProviderID); parseErr == nil {
					xomachine.SetVMTags(ctx, vatesMachine, vmID, xoClient)
				}
			}
		}
		return &ctrl.Result{}, nil
	}

	vmID, parseErr := xok8scommon.GetVMID(*vatesMachine.Status.ProviderID)
	if parseErr != nil {
		return nil, nil
	}

	xoCreds, err := r.resolveMachineCredentials(ctx, vatesMachine)
	if err != nil {
		return nil, nil
	}

	xoClient, err := r.newXOClient(ctx, xoCreds)
	if err != nil {
		return nil, nil
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
func (r *XOMachineReconciler) reconcileExistingVM(ctx context.Context, vatesMachine *infrastructurev1beta2.XOMachine, vm *payloads.VM, xoClient *xok8scommon.XoClient) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if vatesMachine.Status.Ready {
		if vatesMachine.Status.ProviderID != nil && *vatesMachine.Status.ProviderID != "" {
			logger.Info("XOMachine reconciled (fast path)", "name", vatesMachine.Name, "providerID", *vatesMachine.Status.ProviderID)
		}
		return ctrl.Result{}, nil
	}

	var err error
	vm, _, err = xomachine.StartVM(ctx, r.Client, xoClient, vatesMachine, vm)
	if err != nil {
		return ctrl.Result{}, err
	}

	vmIP := xomachine.ResolveVMIP(ctx, xoClient, vm)

	if vmIP == "" {
		logger.Info("VM existing but IP not yet reported, requeuing", "id", vm.ID.String())
		if ue := xomachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "WaitingForIPAddress", "VM is running, waiting for IP address from Xen guest tools"); ue != nil {
			return ctrl.Result{}, ue
		}
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	if err := xomachine.ManageVIFs(ctx, xoClient, vatesMachine, vm); err != nil {
		logger.Error(err, "Failed to manage VIFs")
		if ue := xomachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionFalse, "VifManagementFailed", err.Error()); ue != nil {
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
	if ue := xomachine.UpdateCondition(ctx, r.Client, vatesMachine, metav1.ConditionTrue, "VmReady", "VM is created, running and has an IP address"); ue != nil {
		return ctrl.Result{}, ue
	}

	return ctrl.Result{}, nil
}

func (r *XOMachineReconciler) reconcileDelete(ctx context.Context, vatesMachine *infrastructurev1beta2.XOMachine) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(vatesMachine, xoMachineFinalizer) {
		return ctrl.Result{}, nil
	}

	if vatesMachine.Status.ProviderID != nil && *vatesMachine.Status.ProviderID != "" {
		vmID, err := xok8scommon.GetVMID(*vatesMachine.Status.ProviderID)
		if err != nil {
			logger.Error(err, "Failed to parse providerID, removing finalizer")
			controllerutil.RemoveFinalizer(vatesMachine, xoMachineFinalizer)
			if updateErr := r.Update(ctx, vatesMachine); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, nil
		}

		xoCreds, err := r.resolveMachineCredentials(ctx, vatesMachine)
		if err != nil {
			logger.Error(err, "Failed to resolve XO credentials for VM deletion, removing finalizer")
			controllerutil.RemoveFinalizer(vatesMachine, xoMachineFinalizer)
			if updateErr := r.Update(ctx, vatesMachine); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, nil
		}

		xoClient, err := r.newXOClient(ctx, xoCreds)
		if err != nil || xoClient == nil {
			if err != nil {
				logger.Error(err, "Failed to create XO client for deletion, removing finalizer")
			} else {
				logger.Info("XO credentials not configured for deletion, removing finalizer")
			}
			controllerutil.RemoveFinalizer(vatesMachine, xoMachineFinalizer)
			if updateErr := r.Update(ctx, vatesMachine); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, nil
		}

		logger.Info("Stopping VM", "id", vmID.String())
		taskID, hardErr := xoClient.Client.VM().HardShutdown(ctx, vmID)
		if hardErr != nil {
			if strings.Contains(hardErr.Error(), "unmarshal") {
				if taskPath, extractErr := xomachine.ExtractBareTaskPath(hardErr); extractErr == nil && taskPath != "" {
					logger.Info("HardShutdown issued via bare task, waiting", "id", vmID.String(), "task", taskPath)
					if task, waitErr := xoClient.Client.Task().Wait(ctx, taskPath); waitErr != nil {
						logger.Info("HardShutdown bare task wait failed, continuing cleanup", "id", vmID.String(), "task", taskPath, "error", waitErr)
					} else if task.Status != payloads.Success {
						logger.Info("HardShutdown bare task not successful, continuing cleanup", "id", vmID.String(), "task", taskPath, "status", task.Status)
					}
				} else {
					logger.Info("HardShutdown bare task extraction failed, continuing cleanup", "id", vmID.String(), "error", hardErr)
				}
			} else {
				logger.Info("HardShutdown failed or VM already stopped", "id", vmID.String(), "error", hardErr)
			}
		} else if task, waitErr := xoClient.Client.Task().Wait(ctx, taskID); waitErr != nil {
			logger.Info("HardShutdown task wait failed, continuing cleanup", "id", vmID.String(), "task", taskID, "error", waitErr)
		} else if task.Status != payloads.Success {
			logger.Info("HardShutdown task not successful, continuing cleanup", "id", vmID.String(), "task", taskID, "status", task.Status)
		}

		logger.Info("Deleting VM", "id", vmID.String())
		if delErr := xoClient.Client.VM().Delete(ctx, vmID); delErr != nil {
			if strings.Contains(delErr.Error(), "unmarshal") {
				logger.Info("VM deleted (unmarshal from old API)", "id", vmID.String())
			} else {
				logger.Info("VM delete skipped", "id", vmID.String(), "error", delErr)
			}
		} else {
			logger.Info("VM deleted", "id", vmID.String())
		}
	}

	controllerutil.RemoveFinalizer(vatesMachine, xoMachineFinalizer)
	if err := r.Update(ctx, vatesMachine); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// resolveMachineCredentials resolves the XO credentials for a XOMachine.
// Priority order:
// 1. XOMachine's own identityRef
// 2. Owner XOCluster's identityRef (looked up via the owner Machine's cluster label)
// 3. Global controller credentials (r.XoCreds)
func (r *XOMachineReconciler) resolveMachineCredentials(ctx context.Context, vatesMachine *infrastructurev1beta2.XOMachine) (*xok8scommon.XoConfig, error) {
	// 1. Machine-level identityRef
	if vatesMachine.Spec.IdentityRef != nil {
		return ResolveXOConfig(ctx, r.Client, vatesMachine.Namespace, vatesMachine.Spec.IdentityRef, nil)
	}

	// 2. Try to look up the owner VatesCluster via the Machine's cluster label
	if ownerRef := metav1.GetControllerOf(vatesMachine); ownerRef != nil && ownerRef.Kind == "Machine" {
		machine := &clusterv1.Machine{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: vatesMachine.Namespace, Name: ownerRef.Name}, machine); err == nil {
			clusterName := machine.Labels[clusterv1.ClusterNameLabel]
			if clusterName != "" {
				vatesCluster, err := xomachine.GetXOCluster(ctx, r.Client, vatesMachine.Namespace, clusterName)
				if err == nil && vatesCluster != nil && vatesCluster.Spec.IdentityRef != nil {
					return ResolveXOConfig(ctx, r.Client, vatesMachine.Namespace, vatesCluster.Spec.IdentityRef, nil)
				}
			}
		}
	}

	// 3. Fall back to global controller credentials
	if r.XoCreds == nil {
		return nil, nil
	}

	cfg := *r.XoCreds
	return &cfg, nil
}

// isConditionTrue returns true when the XOMachine has a Ready condition with the given reason and status True.
func isConditionTrue(vatesMachine *infrastructurev1beta2.XOMachine, reason string) bool {
	for _, c := range vatesMachine.Status.Conditions {
		if c.Type == "Ready" && c.Status == metav1.ConditionTrue && c.Reason == reason {
			return true
		}
	}
	return false
}

// newXOClient creates an XO client from the given config. If the reconciler
// has a custom newClientFunc (set in tests), it uses that instead.
func (r *XOMachineReconciler) newXOClient(ctx context.Context, xoCreds *xok8scommon.XoConfig) (*xok8scommon.XoClient, error) {
	if r.newClientFunc != nil {
		return r.newClientFunc(ctx, xoCreds)
	}
	return NewXOClientFromConfig(ctx, xoCreds)
}
