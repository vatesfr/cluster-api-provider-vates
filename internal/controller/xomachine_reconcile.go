package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gofrs/uuid"
	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
	"github.com/vatesfr/cluster-api-provider-vates/internal/bootstrap"
	xomachine "github.com/vatesfr/cluster-api-provider-vates/internal/controller/xomachine"
)

// ipamNetworkConfig claims the machine's address from the IPAM pool its network
// configuration names, and returns the netplan document that puts that address
// on the node.
//
// A non-nil result means the caller must stop and requeue: the pool has not
// answered yet, and there is nothing to boot on. The address is not guessed in
// the meantime -- a machine that boots on a DHCP lease it will not keep is
// exactly the machine this replaces.
func (r *XOMachineReconciler) ipamNetworkConfig(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine) (string, *ctrl.Result, error) {
	logger := log.FromContext(ctx)

	address, err := xomachine.ClaimedAddress(ctx, r.Client, xoMachine)
	if err != nil {
		logger.Error(err, "Failed to claim an address from the IPAM pool")
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionFalse, "AddressClaimFailed", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return "", nil, err
	}
	if address == nil {
		logger.Info("Waiting for the IPAM pool to answer the address claim", "machine", xoMachine.Name)
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionFalse, "WaitingForAddress",
			"the IPAM pool has not answered the address claim yet"); updateErr != nil {
			return "", nil, updateErr
		}
		return "", &ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	logger.Info("Claimed an address from the IPAM pool", "machine", xoMachine.Name, "address", address.IP)
	return xomachine.Netplan(address, nodeInterface), nil, nil
}

// nodeInterface is the interface the node's network configuration names. The
// node image brings up its first ethernet under this name, and the document has
// to name the interface the node will actually find.
const nodeInterface = "eth0"

func (r *XOMachineReconciler) reconcileNormal(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := r.ensureFinalizer(ctx, xoMachine); err != nil {
		return ctrl.Result{}, err
	}

	if result, err := r.tryFastPath(ctx, xoMachine); result != nil {
		return *result, err
	}

	bsResult, err := bootstrap.ResolveBootstrapData(ctx, r.Client, xoMachine)
	if err != nil {
		return ctrl.Result{}, err
	}
	if bsResult.Requeue {
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	xoCreds, err := r.resolveMachineCredentials(ctx, xoMachine)
	if err != nil {
		logger.Error(err, "Failed to resolve XO credentials")
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionFalse, "CredentialsResolutionFailed", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	xoClient, err := r.newXOClient(ctx, xoCreds)
	if err != nil {
		logger.Error(err, "Failed to create/connect XO client")
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionFalse, "XOConnectionFailed", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}
	if xoClient == nil {
		logger.Info("XO credentials not configured, skipping VM provisioning")
		return ctrl.Result{}, nil
	}

	deps := bootstrap.Dependencies{
		Client:        r.Client,
		XOClient:      xoClient,
		Machine:       bsResult.Machine,
		XOMachine:     xoMachine,
		BootstrapData: bsResult.Data,
	}

	result, err := bootstrap.Build(ctx, deps, bootstrapBehavior(xoMachine))
	if err != nil {
		logger.Error(err, "Failed to build bootstrap payload")
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionFalse, "CloudConfigBuildFailed", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	cloudConfig := result.CloudConfig
	networkConfig := result.NetworkConfig

	// An address claimed from an IPAM pool replaces whatever the machine's own
	// network configuration said. The pool is the answer to "which address", and
	// there is only one answer: the node boots on the address the provider chose
	// rather than on whatever a DHCP server hands out, which is what keeps a
	// control plane's identity -- and the peer URL etcd recorded for it -- stable
	// across a reboot.
	if xoMachine.Spec.NetworkConfig != nil && len(xoMachine.Spec.NetworkConfig.AddressesFromPools) > 0 {
		doc, wait, claimErr := r.ipamNetworkConfig(ctx, xoMachine)
		if claimErr != nil {
			return ctrl.Result{}, claimErr
		}
		if wait != nil {
			return *wait, nil
		}
		networkConfig = &doc
	}

	templateID, err := xomachine.ResolveTemplateID(ctx, xoClient, xoMachine.Spec.TemplateID, xoMachine.Spec.TemplateName)
	if err != nil {
		logger.Error(err, "Failed to find template", "templateID", xoMachine.Spec.TemplateID, "templateName", xoMachine.Spec.TemplateName)
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionFalse, "TemplateNotFound", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	poolID, err := xomachine.ResolvePoolID(ctx, xoClient, xoMachine.Spec.PoolID, xoMachine.Spec.PoolName)
	if err != nil {
		logger.Error(err, "Failed to find pool", "poolID", xoMachine.Spec.PoolID, "poolName", xoMachine.Spec.PoolName)
		if updateErr := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionFalse, "PoolNotFound", err.Error()); updateErr != nil {
			logger.Error(updateErr, "Failed to update condition")
		}
		return ctrl.Result{}, err
	}

	var vm *payloads.VM
	if xoMachine.Status.ProviderID != nil && *xoMachine.Status.ProviderID != "" {
		vmID, parseErr := xok8scommon.GetVMID(*xoMachine.Status.ProviderID)
		if parseErr != nil {
			logger.Error(parseErr, "Failed to parse existing providerID")
			if updateErr := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionFalse, "InvalidProviderID", parseErr.Error()); updateErr != nil {
				logger.Error(updateErr, "Failed to update condition")
			}
			return ctrl.Result{}, parseErr
		}
		vm, err = xomachine.LookupExistingVM(ctx, r.Client, xoClient, xoMachine, vmID)
	} else {
		vm, err = r.createVM(ctx, xoMachine, bsResult, xoClient, poolID, templateID, cloudConfig, networkConfig)
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	xomachine.SetVMTags(ctx, r.Client, xoMachine, vm.ID, xoClient, xoMachine.Spec.BootstrapProvider)

	if !xoMachine.Status.Ready {
		result, waitErr := xomachine.WaitForVMReady(ctx, r.Client, xoClient, xoMachine, vm)
		if waitErr != nil || !result.IsZero() {
			return result, waitErr
		}
	}

	if xoMachine.Status.ProviderID != nil && *xoMachine.Status.ProviderID != "" {
		logger.Info("XOMachine reconciled", "name", xoMachine.Name, "providerID", *xoMachine.Status.ProviderID)
	}
	return ctrl.Result{}, nil
}

func (r *XOMachineReconciler) ensureFinalizer(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine) error {
	if !controllerutil.AddFinalizer(xoMachine, xoMachineFinalizer) {
		return nil
	}
	logger := log.FromContext(ctx)
	if err := r.Update(ctx, xoMachine); err != nil {
		if !apierrors.IsConflict(err) {
			logger.Error(err, "Failed to add finalizer")
			return err
		}
		logger.Info("Conflict adding finalizer, retrying once")
		if err := r.Get(ctx, types.NamespacedName{Namespace: xoMachine.Namespace, Name: xoMachine.Name}, xoMachine); err != nil {
			return err
		}
		if controllerutil.AddFinalizer(xoMachine, xoMachineFinalizer) {
			if err := r.Update(ctx, xoMachine); err != nil {
				logger.Error(err, "Failed to add finalizer after retry")
				return err
			}
		}
	}
	return nil
}

// bootstrapBehavior maps the XOMachine's declared bootstrap behavior. What to
// do with the payload is declared, never inferred from a provider name.
func bootstrapBehavior(xoMachine *infrastructurev1beta2.XOMachine) bootstrap.Behavior {
	if xoMachine.Spec.Bootstrap == nil {
		return bootstrap.Behavior{}
	}
	format := xoMachine.Spec.Bootstrap.Format
	// Deprecated: derive the format from the legacy CloudInit flag when Format
	// is unset, so objects written before Format existed keep working.
	// TODO(v1.2): remove this fallback together with the CloudInit field.
	//nolint:staticcheck // Intentional use of the deprecated field, removed in v1.2.
	if format == "" && xoMachine.Spec.Bootstrap.CloudInit {
		format = infrastructurev1beta2.BootstrapFormatCloudInit
	}
	return bootstrap.Behavior{Format: format}
}

// createVM names the machine's VM and creates it. Another VM already carrying
// the name does not prevent the creation -- Xen Orchestra does not enforce
// unique names -- but is reported as a Warning event.
func (r *XOMachineReconciler) createVM(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine, bsResult bootstrap.ResolveBootstrapDataResult,
	xoClient *xok8scommon.XoClient, poolID, templateID uuid.UUID, cloudConfig string, networkConfig *string) (*payloads.VM, error) {
	vmName, err := r.resolveVMName(ctx, xoMachine, bsResult)
	if err != nil {
		return nil, err
	}
	return xomachine.CreateVM(ctx, r.Client, xoClient, xoMachine, poolID, templateID, cloudConfig, networkConfig, vmName, xoMachine.Spec.BootstrapProvider,
		func(vmName string, count int) {
			log.FromContext(ctx).Info("Creating VM although other VMs already have its name", "vmName", vmName, "count", count)
			r.warn(xoMachine, "DuplicateVMName", "CreateVM",
				"%d other VM(s) in Xen Orchestra are already named %q; the VM is created anyway", count, vmName)
		})
}

// resolveVMName returns the name of the VM to create.
//
// The name is computed once and recorded in status.vmName BEFORE the VM is
// created; every later attempt reuses it. The naming configuration lives in
// the XOCluster, which can change at any time: without a recorded name, a
// retry after a create whose result was lost could compute another name, miss
// the VM it created (the adoption lookup searches by name) and create a second
// one. Once the VM exists, the name is never looked at again.
func (r *XOMachineReconciler) resolveVMName(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine, bsResult bootstrap.ResolveBootstrapDataResult) (string, error) {
	if xoMachine.Status.VMName != "" {
		return xoMachine.Status.VMName, nil
	}

	vmName, err := r.computeVMName(ctx, xoMachine, bsResult)
	if err != nil {
		return "", xomachine.WithConditionUpdate(ctx, r.Client, xoMachine, err, metav1.ConditionFalse, "VMNameUnresolved")
	}

	xoMachine.Status.VMName = vmName
	if err := r.Status().Update(ctx, xoMachine); err != nil {
		log.FromContext(ctx).Error(err, "Failed to record the VM name", "vmName", vmName)
		return "", err
	}
	log.FromContext(ctx).Info("Recorded VM name", "vmName", vmName)
	return vmName, nil
}

// computeVMName computes the VM name from the XOCluster spec.vmNaming when it
// is set, and from the deprecated XOMachine spec.namePrefix otherwise.
//
// The XOCluster must be readable: falling back to the namePrefix naming when it
// is not would record a name the configuration did not ask for, for good.
func (r *XOMachineReconciler) computeVMName(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine, bsResult bootstrap.ResolveBootstrapDataResult) (string, error) {
	clusterName := xoMachine.Labels[clusterv1.ClusterNameLabel]
	if clusterName == "" && bsResult.Machine != nil {
		clusterName = bsResult.Machine.Labels[clusterv1.ClusterNameLabel]
	}

	var naming *infrastructurev1beta2.VMNamingSpec
	if clusterName != "" {
		xoCluster, err := xomachine.GetXOCluster(ctx, r.Client, xoMachine.Namespace, clusterName)
		if err != nil {
			return "", fmt.Errorf("failed to read the XOCluster to name the VM: %w", err)
		}
		if xoCluster != nil {
			naming = xoCluster.Spec.VMNaming
		}
	}

	//nolint:staticcheck // Intentional use of the deprecated field, removed in v1.2.
	namePrefix := xoMachine.Spec.NamePrefix

	if naming == nil {
		if namePrefix == "" {
			return "", errors.New("no VM name is configured: set the XOCluster spec.vmNaming")
		}
		return r.buildVMName(xoMachine, bsResult), nil
	}

	if bsResult.Machine == nil {
		return "", errors.New("the XOCluster spec.vmNaming names the VM after its owner Machine, and the XOMachine has none")
	}
	if namePrefix != "" {
		r.warn(xoMachine, "NamePrefixIgnored", "ResolveVMName",
			"spec.namePrefix %q is ignored: the XOCluster spec.vmNaming names the VM", namePrefix)
	}
	return xomachine.VMNameFromNaming(naming, isControlPlane(xoMachine, bsResult.Machine), bsResult.Machine.Name)
}

// isControlPlane reports whether the machine is a control plane node, from the
// Cluster API control plane label on the XOMachine or on its Machine.
func isControlPlane(xoMachine *infrastructurev1beta2.XOMachine, machine *clusterv1.Machine) bool {
	if _, ok := xoMachine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
		return true
	}
	if machine != nil {
		if _, ok := machine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
			return true
		}
	}
	return false
}

// warn emits a Warning event on the XOMachine. It is a no-op without a
// recorder, as in unit tests.
func (r *XOMachineReconciler) warn(xoMachine *infrastructurev1beta2.XOMachine, reason, action, note string, args ...any) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(xoMachine, nil, corev1.EventTypeWarning, reason, action, note, args...)
}

// buildVMName is the namePrefix naming, kept unchanged for configurations that
// set no XOCluster spec.vmNaming.
// TODO(v1.2): remove together with the NamePrefix field.
//
//nolint:staticcheck // Intentional use of the deprecated field, removed in v1.2.
func (r *XOMachineReconciler) buildVMName(xoMachine *infrastructurev1beta2.XOMachine, bsResult bootstrap.ResolveBootstrapDataResult) string {
	if bsResult.Machine == nil {
		return xoMachine.Spec.NamePrefix
	}
	cn := bsResult.Machine.Labels["cluster.x-k8s.io/cluster-name"]
	if cn == "" {
		return xoMachine.Spec.NamePrefix
	}
	role := ""
	np := xoMachine.Spec.NamePrefix
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
func (r *XOMachineReconciler) tryFastPath(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine) (*ctrl.Result, error) {
	if xoMachine.Status.ProviderID == nil || *xoMachine.Status.ProviderID == "" {
		return nil, nil
	}

	if xoMachine.Status.Ready {
		if !isConditionTrue(xoMachine, "VmReady") {
			if ue := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionTrue, "VmReady", "VM is created, running and has an IP address"); ue != nil {
				return &ctrl.Result{}, ue
			}
		}
		// Best-effort, on every reconcile: keep the VM tags current (also tags
		// VMs created before tagging existed) and re-read the machine's
		// address, which Xen Orchestra can have changed since provisioning.
		r.refreshReadyMachine(ctx, xoMachine)
		return &ctrl.Result{}, nil
	}

	vmID, parseErr := xok8scommon.GetVMID(*xoMachine.Status.ProviderID)
	if parseErr != nil {
		return nil, nil
	}

	xoCreds, err := r.resolveMachineCredentials(ctx, xoMachine)
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

	result, err := r.reconcileExistingVM(ctx, xoMachine, vm, xoClient)
	return &result, err
}

// refreshReadyMachine applies the two best-effort updates a Ready machine still
// needs on every reconcile: its VM tags, and its address.
//
// The address is not stable. Xen Orchestra reports the guest's own address, and
// a DHCP lease can move across a reboot; the Ready fast path is the only place a
// long-lived machine is looked at, so without this the address stays frozen at
// the one seen during provisioning. Cluster API publishes it as the workload
// Node's InternalIP and the cluster endpoint falls back to it, so a stale
// address is a wrong address for everything downstream.
//
// Both updates are best-effort by design: a Ready machine must not be requeued
// or marked unready because Xen Orchestra was briefly unreachable. The next
// reconcile retries.
func (r *XOMachineReconciler) refreshReadyMachine(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine) {
	logger := log.FromContext(ctx)

	xoCreds, err := r.resolveMachineCredentials(ctx, xoMachine)
	if err != nil {
		return
	}
	xoClient, err := r.newXOClient(ctx, xoCreds)
	if err != nil || xoClient == nil {
		return
	}
	vmID, err := xok8scommon.GetVMID(*xoMachine.Status.ProviderID)
	if err != nil {
		return
	}

	xomachine.SetVMTags(ctx, r.Client, xoMachine, vmID, xoClient, xoMachine.Spec.BootstrapProvider)

	vm, err := xoClient.Client.VM().GetByID(ctx, vmID)
	if err != nil {
		logger.Error(err, "Failed to re-read the VM to refresh its address", "id", vmID.String())
		return
	}
	if !xomachine.SetNodeAddress(xoMachine, xomachine.ResolveVMIPNow(xoClient, vm)) {
		return
	}
	if err := r.Status().Update(ctx, xoMachine); err != nil {
		logger.Error(err, "Failed to record the machine's refreshed address")
	}
}

// reconcileExistingVM handles the fast-path when the VM already exists.
// It skips bootstrap data assembly, cloud-config building, VM creation,
// CPU/boot-order configuration, and providerID assignment — all already done.
func (r *XOMachineReconciler) reconcileExistingVM(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine, vm *payloads.VM, xoClient *xok8scommon.XoClient) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if xoMachine.Status.Ready {
		if xoMachine.Status.ProviderID != nil && *xoMachine.Status.ProviderID != "" {
			logger.Info("XOMachine reconciled (fast path)", "name", xoMachine.Name, "providerID", *xoMachine.Status.ProviderID)
		}
		return ctrl.Result{}, nil
	}

	var err error
	vm, _, err = xomachine.StartVM(ctx, r.Client, xoClient, xoMachine, vm)
	if err != nil {
		return ctrl.Result{}, err
	}

	vmIP := xomachine.ResolveVMIP(ctx, xoClient, vm)

	if vmIP == "" {
		logger.Info("VM existing but IP not yet reported, requeuing", "id", vm.ID.String())
		if ue := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionFalse, "WaitingForIPAddress", "VM is running, waiting for IP address from Xen guest tools"); ue != nil {
			return ctrl.Result{}, ue
		}
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	if err := xomachine.ManageVIFs(ctx, xoClient, xoMachine, vm); err != nil {
		logger.Error(err, "Failed to manage VIFs")
		if ue := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionFalse, "VifManagementFailed", err.Error()); ue != nil {
			return ctrl.Result{}, ue
		}
		return ctrl.Result{}, err
	}

	xoMachine.Status.Addresses = []corev1.NodeAddress{
		{
			Type:    corev1.NodeInternalIP,
			Address: vmIP,
		},
	}
	xoMachine.Status.Ready = true
	xoMachine.Status.Initialization = &infrastructurev1beta2.InitializationStatus{Provisioned: true}
	if ue := xomachine.UpdateCondition(ctx, r.Client, xoMachine, metav1.ConditionTrue, "VmReady", "VM is created, running and has an IP address"); ue != nil {
		return ctrl.Result{}, ue
	}

	return ctrl.Result{}, nil
}

func (r *XOMachineReconciler) reconcileDelete(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(xoMachine, xoMachineFinalizer) {
		return ctrl.Result{}, nil
	}

	if xoMachine.Status.ProviderID != nil && *xoMachine.Status.ProviderID != "" {
		vmID, err := xok8scommon.GetVMID(*xoMachine.Status.ProviderID)
		if err != nil {
			logger.Error(err, "Failed to parse providerID, removing finalizer")
			return ctrl.Result{}, r.removeFinalizer(ctx, xoMachine)
		}

		xoCreds, err := r.resolveMachineCredentials(ctx, xoMachine)
		if err != nil {
			logger.Error(err, "Failed to resolve XO credentials for VM deletion, requeuing")
			return ctrl.Result{}, err
		}

		xoClient, err := r.newXOClient(ctx, xoCreds)
		if err != nil {
			logger.Error(err, "Failed to create XO client for deletion, requeuing")
			return ctrl.Result{}, err
		}
		if xoClient == nil {
			logger.Info("XO credentials not configured for deletion, removing finalizer")
			return ctrl.Result{}, r.removeFinalizer(ctx, xoMachine)
		}

		// A paused or suspended VM cannot release its disks (XCP-ng refuses the
		// VBD removal with OPERATION_NOT_ALLOWED). Stop it first: the detach
		// below would otherwise retry forever and the finalizer would never be
		// released.
		if err := xomachine.StopVMIfNotDetachable(ctx, xoClient, vmID); err != nil {
			logger.Error(err, "Failed to stop a non-detachable VM before detaching persistent volumes, requeuing", "id", vmID.String())
			return ctrl.Result{}, err
		}

		logger.Info("Detaching persistent volume disks before VM deletion", "id", vmID.String())
		if err := xomachine.DetachPersistentVolumes(ctx, xoClient, vmID); err != nil {
			logger.Error(err, "Failed to detach persistent volume disks, requeuing (VM deletion would destroy the PV data)", "id", vmID.String())
			return ctrl.Result{}, err
		}

		logger.Info("Stopping VM", "id", vmID.String())
		if err := xomachine.HardShutdownVM(ctx, xoClient, vmID); err != nil {
			logger.Info("HardShutdown did not complete, continuing cleanup", "id", vmID.String(), "error", err)
		}

		logger.Info("Deleting VM", "id", vmID.String())
		delErr := xoClient.Client.VM().Delete(ctx, vmID)
		if delErr == nil {
			logger.Info("VM deleted", "id", vmID.String())
		} else if xomachine.IsNotFoundError(delErr) {
			logger.Info("VM already deleted", "id", vmID.String(), "error", delErr)
		} else if strings.Contains(delErr.Error(), "unmarshal") {
			logger.Info("VM deleted (unmarshal from old API)", "id", vmID.String())
		} else {
			logger.Error(delErr, "Failed to delete VM, requeuing", "id", vmID.String())
			return ctrl.Result{}, delErr
		}
	}

	if err := r.removeFinalizer(ctx, xoMachine); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// removeFinalizer removes the XOMachine finalizer and persists the change.
// A NotFound error means the object was already deleted (e.g. another
// reconcile removed the finalizer concurrently), which is not an error.
func (r *XOMachineReconciler) removeFinalizer(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine) error {
	controllerutil.RemoveFinalizer(xoMachine, xoMachineFinalizer)
	if err := r.Update(ctx, xoMachine); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// resolveMachineCredentials resolves the XO credentials for a XOMachine.
// Priority order:
// 1. XOMachine's own identityRef
// 2. Owner XOCluster's identityRef (looked up via the owner Machine's cluster label)
// 3. Global controller credentials (r.XoCreds)
func (r *XOMachineReconciler) resolveMachineCredentials(ctx context.Context, xoMachine *infrastructurev1beta2.XOMachine) (*xok8scommon.XoConfig, error) {
	// 1. Machine-level identityRef
	if xoMachine.Spec.IdentityRef != nil {
		return ResolveXOConfig(ctx, r.Client, xoMachine.Namespace, xoMachine.Spec.IdentityRef, nil)
	}

	// 2. Try to look up the owner VatesCluster via the Machine's cluster label
	if ownerRef := metav1.GetControllerOf(xoMachine); ownerRef != nil && ownerRef.Kind == "Machine" {
		machine := &clusterv1.Machine{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: xoMachine.Namespace, Name: ownerRef.Name}, machine); err == nil {
			clusterName := machine.Labels[clusterv1.ClusterNameLabel]
			if clusterName != "" {
				xoCluster, err := xomachine.GetXOCluster(ctx, r.Client, xoMachine.Namespace, clusterName)
				if err == nil && xoCluster != nil && xoCluster.Spec.IdentityRef != nil {
					return ResolveXOConfig(ctx, r.Client, xoMachine.Namespace, xoCluster.Spec.IdentityRef, nil)
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
func isConditionTrue(xoMachine *infrastructurev1beta2.XOMachine, reason string) bool {
	for _, c := range xoMachine.Status.Conditions {
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
