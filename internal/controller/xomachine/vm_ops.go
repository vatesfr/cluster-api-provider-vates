package xomachine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

func CreateVM(ctx context.Context, c client.Client, xoClient *xok8scommon.XoClient, vatesMachine *infrastructurev1beta2.XOMachine, poolID uuid.UUID, templateID uuid.UUID, cloudConfig string, networkConfig *string, vmName string) (*payloads.VM, error) {
	logger := log.FromContext(ctx)

	createParams := buildCreateParams(templateID, vmName, cloudConfig, networkConfig)

	if vatesMachine.Spec.ResourceSet != nil && vatesMachine.Spec.ResourceSet.Memory != "" {
		memQty, err := resource.ParseQuantity(vatesMachine.Spec.ResourceSet.Memory)
		if err != nil {
			logger.Error(err, "Failed to parse memory quantity", "memory", vatesMachine.Spec.ResourceSet.Memory)
			return nil, WithConditionUpdate(ctx, c, vatesMachine, err, metav1.ConditionFalse, "InvalidMemoryQuantity")
		}
		memBytes := int(memQty.Value())
		createParams.Memory = &memBytes
		logger.Info("Set VM memory", "memory", vatesMachine.Spec.ResourceSet.Memory, "bytes", memBytes)
	}

	v1Client := xoClient.Client.V1Client()
	v1Concrete, v1Ok := v1Client.(*xoclient.Client)

	addVIFsToParams(ctx, vatesMachine, v1Client, createParams, poolID)

	vm, err := xoClient.Client.VM().Create(ctx, poolID, createParams)
	if err != nil {
		if strings.Contains(err.Error(), "unmarshal") {
			logger.Info("V2 API created VM but returned bare task ID, extracting", "error", err)
			vm, err = handleV2BareTaskResponse(ctx, xoClient, err)
		}
		if err != nil {
			logger.Error(err, "Failed to create VM", "name", vmName)
			return nil, WithConditionUpdate(ctx, c, vatesMachine, err, metav1.ConditionFalse, "VmCreationFailed")
		}
	}

	logger.Info("VM created", "name", vm.NameLabel, "id", vm.ID.String(), "pool", poolID.String())

	resizeDiskIfNeeded(ctx, vatesMachine, vm, v1Client, v1Concrete, v1Ok)
	setCPUsIfNeeded(ctx, vatesMachine, vm, v1Concrete, v1Ok)
	setBootOrder(ctx, vm, v1Concrete, v1Ok)

	providerID := xok8scommon.GetProviderID(poolID, vm)
	if err := saveProviderID(ctx, c, vatesMachine, providerID); err != nil {
		return nil, err
	}
	logger.Info("ProviderID saved", "providerID", providerID)

	SetVMTags(ctx, vatesMachine, vm.ID, xoClient)

	return vm, nil
}

// SetVMTags applies identifying tags to the VM so it can be recognized in XO
// (cluster name, CAPI machine name, role). Best-effort: errors are logged and
// the reconcile continues. Idempotent, safe to call on every reconcile.
func SetVMTags(ctx context.Context, vatesMachine *infrastructurev1beta2.XOMachine, vmID uuid.UUID, xoClient *xok8scommon.XoClient) {
	logger := log.FromContext(ctx)
	if xoClient == nil {
		return
	}
	v1Client := xoClient.Client.V1Client()
	v1Concrete, v1Ok := v1Client.(*xoclient.Client)
	if !v1Ok || v1Concrete == nil {
		return
	}

	tags := vmTags(vatesMachine)
	for _, tag := range tags {
		if err := v1Concrete.AddTag(vmID.String(), tag); err != nil {
			logger.Info("Failed to set VM tag (will continue)", "id", vmID.String(), "tag", tag, "error", err)
			return
		}
	}
	logger.Info("Set VM tags", "id", vmID.String(), "tags", tags)
}

// vmTags builds the identifying tags for a VM (cluster, machine name, role).
func vmTags(vatesMachine *infrastructurev1beta2.XOMachine) []string {
	role := "worker"
	if _, ok := vatesMachine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
		role = "control-plane"
	}
	return []string{
		"cluster-name:" + vatesMachine.Labels[clusterv1.ClusterNameLabel],
		"machine:" + vatesMachine.Name,
		"role:" + role,
	}
}

func buildCreateParams(templateID uuid.UUID, vmName string, cloudConfig string, networkConfig *string) *payloads.CreateVMParams {
	createParams := &payloads.CreateVMParams{
		NameLabel:     vmName,
		Template:      templateID,
		AutoPoweron:   ptr.To(false),
		Clone:         ptr.To(true),
		CloudConfig:   ptr.To(cloudConfig),
		NetworkConfig: networkConfig,
	}
	return createParams
}

func addVIFsToParams(ctx context.Context, vatesMachine *infrastructurev1beta2.XOMachine, v1Client xoclient.XOClient, createParams *payloads.CreateVMParams, poolID uuid.UUID) {
	logger := log.FromContext(ctx)
	if vatesMachine.Spec.NetworkConfig == nil || len(vatesMachine.Spec.NetworkConfig.Networks) == 0 {
		return
	}
	for i, netConfig := range vatesMachine.Spec.NetworkConfig.Networks {
		networkID, err := ResolveNetworkID(v1Client, netConfig, poolID)
		if err != nil {
			logger.Error(err, "Failed to resolve network for VM creation", "network", netConfig.Name)
			continue
		}
		device := fmt.Sprintf("%d", i)
		createParams.VIFs = append(createParams.VIFs, payloads.VIFParams{
			Device:  &device,
			Network: &networkID,
		})
		logger.Info("Adding VIF to VM creation params", "network", networkID, "device", device)
	}
}

func resizeDiskIfNeeded(ctx context.Context, vatesMachine *infrastructurev1beta2.XOMachine, vm *payloads.VM, v1Client xoclient.XOClient, v1Concrete *xoclient.Client, v1Ok bool) {
	logger := log.FromContext(ctx)
	diskSize := ""
	if vatesMachine.Spec.ResourceSet != nil {
		diskSize = vatesMachine.Spec.ResourceSet.DiskSize
	}
	if diskSize == "" || !v1Ok {
		return
	}
	diskQty, qErr := resource.ParseQuantity(diskSize)
	if qErr != nil {
		return
	}
	disks, dErr := v1Client.GetDisks(&xoclient.Vm{Id: vm.ID.String()})
	if dErr != nil {
		logger.Error(dErr, "Failed to get disks for VM resize")
		return
	}
	mainDisk := findMainDisk(disks)
	if mainDisk == nil {
		logger.Info("No main disk found for VM resize", "diskCount", len(disks))
		return
	}
	requestedSize := diskQty.Value()
	if int64(mainDisk.Size) >= requestedSize {
		logger.Info("VM disk already at or above requested size, skipping resize", "currentSize", mainDisk.Size, "requestedSize", requestedSize, "diskSize", diskSize, "vdi", mainDisk.VDIId)
		return
	}
	var success bool
	if err := v1Concrete.Call("vdi.set", map[string]any{
		"id":   mainDisk.VDIId,
		"size": requestedSize,
	}, &success); err != nil {
		logger.Info("Failed to set disk size (will continue)", "diskSize", diskSize, "requestedSize", requestedSize, "currentSize", mainDisk.Size, "vdi", mainDisk.VDIId, "error", err)
		return
	}
	logger.Info("Set VM disk size", "diskSize", diskSize, "vdi", mainDisk.VDIId, "name", mainDisk.NameLabel, "success", success)
}

// findMainDisk selects the OS disk of a VM among the disks returned by the
// XO API. The API returns disks in non-deterministic order, so the first
// element cannot be trusted. The OS disk is identified by the bootable VBD;
// when none is bootable, the disk with the lowest position is used, ignoring
// the cloud-init config drive. A single-disk VM is always treated as having
// its only disk as the main disk.
func findMainDisk(disks []xoclient.Disk) *xoclient.Disk {
	if len(disks) == 0 {
		return nil
	}
	if len(disks) == 1 {
		return &disks[0]
	}
	for i := range disks {
		if disks[i].IsCdDrive || isCloudConfigDrive(disks[i]) {
			continue
		}
		if disks[i].Bootable {
			return &disks[i]
		}
	}
	var best *xoclient.Disk
	for i := range disks {
		if disks[i].IsCdDrive || isCloudConfigDrive(disks[i]) {
			continue
		}
		if best == nil || diskOrder(disks[i], *best) < 0 {
			disk := disks[i]
			best = &disk
		}
	}
	return best
}

// isCloudConfigDrive reports whether the disk is the cloud-init config drive,
// identified by its name label. The match is specific to config drives (e.g.
// XO's "XO CloudConfigDrive") so that Talos disks named "*-nocloud" are not
// mistaken for config drives, and OS disks whose name merely mentions
// cloud-init (e.g. "Ubuntu 24.04 Cloud-Init (Hub)") are not skipped.
func isCloudConfigDrive(d xoclient.Disk) bool {
	name := strings.ToLower(d.NameLabel)
	return strings.Contains(name, "cloudconfigdrive") ||
		strings.Contains(name, "cloud config drive")
}

// diskOrder returns a negative number when a is ordered before b, zero when
// they are equivalent, and a positive number otherwise. Disks are ordered by
// their position first, then by their device name.
func diskOrder(a, b xoclient.Disk) int {
	pa, aOk := parseDiskPosition(a.Position)
	pb, bOk := parseDiskPosition(b.Position)
	switch {
	case aOk && bOk:
		if pa != pb {
			return pa - pb
		}
	case aOk != bOk:
		if aOk {
			return -1
		}
		return 1
	}
	return strings.Compare(a.Device, b.Device)
}

// parseDiskPosition parses a VBD position string (e.g. "0", "1") into an int.
func parseDiskPosition(position string) (int, bool) {
	if position == "" {
		return 0, false
	}
	n, err := strconv.Atoi(position)
	if err != nil {
		return 0, false
	}
	return n, true
}

func setCPUsIfNeeded(ctx context.Context, vatesMachine *infrastructurev1beta2.XOMachine, vm *payloads.VM, v1Concrete *xoclient.Client, v1Ok bool) {
	logger := log.FromContext(ctx)
	if vatesMachine.Spec.ResourceSet == nil || vatesMachine.Spec.ResourceSet.CPUs == nil || !v1Ok {
		return
	}
	cpus := int(*vatesMachine.Spec.ResourceSet.CPUs)
	var success bool
	if err := v1Concrete.Call("vm.set", map[string]any{
		"id":   vm.ID.String(),
		"CPUs": cpus,
	}, &success); err != nil {
		logger.Error(err, "Failed to set CPUs after VM creation", "cpus", cpus)
		return
	}
	logger.Info("Set VM CPUs", "cpus", cpus)
}

func setBootOrder(ctx context.Context, vm *payloads.VM, v1Concrete *xoclient.Client, v1Ok bool) {
	logger := log.FromContext(ctx)
	if !v1Ok {
		logger.Info("V1 client not available or type assertion failed, skipping boot order configuration")
		return
	}
	var success bool
	if err := v1Concrete.Call("vm.setBootOrder", map[string]any{
		"vm":    vm.ID.String(),
		"order": "cd",
	}, &success); err != nil {
		logger.Error(err, "Failed to set boot order to disk", "id", vm.ID.String())
		return
	}
	logger.Info("Set VM boot order to disk", "id", vm.ID.String(), "success", success)
}

func saveProviderID(ctx context.Context, c client.Client, vatesMachine *infrastructurev1beta2.XOMachine, providerID string) error {
	logger := log.FromContext(ctx)
	vatesMachine.Spec.ProviderID = &providerID
	if err := c.Update(ctx, vatesMachine); err != nil {
		if !apierrors.IsConflict(err) {
			logger.Error(err, "Failed to save providerID in spec after VM creation")
			return err
		}
		logger.Info("Conflict saving providerID in spec, retrying")
		if err := c.Get(ctx, types.NamespacedName{Namespace: vatesMachine.Namespace, Name: vatesMachine.Name}, vatesMachine); err != nil {
			return err
		}
		vatesMachine.Spec.ProviderID = &providerID
		if err := c.Update(ctx, vatesMachine); err != nil {
			logger.Error(err, "Failed to save providerID in spec after retry")
			return err
		}
	}
	vatesMachine.Status.ProviderID = &providerID
	if err := c.Status().Update(ctx, vatesMachine); err != nil {
		if !apierrors.IsConflict(err) {
			logger.Error(err, "Failed to save providerID in status after VM creation")
			return err
		}
		logger.Info("Conflict saving providerID in status, retrying")
		if err := c.Get(ctx, types.NamespacedName{Namespace: vatesMachine.Namespace, Name: vatesMachine.Name}, vatesMachine); err != nil {
			return err
		}
		vatesMachine.Status.ProviderID = &providerID
		if err := c.Status().Update(ctx, vatesMachine); err != nil {
			logger.Error(err, "Failed to save providerID in status after retry")
			return err
		}
	}
	return nil
}

// LookupExistingVM looks up an already-known VM by its ID.
func LookupExistingVM(ctx context.Context, c client.Client, xoClient *xok8scommon.XoClient, vatesMachine *infrastructurev1beta2.XOMachine, vmID uuid.UUID) (*payloads.VM, error) {
	logger := log.FromContext(ctx)

	vm, err := xoClient.Client.VM().GetByID(ctx, vmID)
	if err != nil {
		logger.Error(err, "Failed to get existing VM by providerID", "vmID", vmID.String())
		return nil, WithConditionUpdate(ctx, c, vatesMachine, err, metav1.ConditionFalse, "VmNotFound")
	}
	logger.Info("Found existing VM from providerID", "id", vm.ID.String(), "name", vm.NameLabel)
	return vm, nil
}

// StartVM starts the VM if it is not already running. It waits for the start
// task to complete and returns the updated VM, a boolean indicating whether
// the VM was started by this call, and any error.
func StartVM(ctx context.Context, c client.Client, xoClient *xok8scommon.XoClient, vatesMachine *infrastructurev1beta2.XOMachine, vm *payloads.VM) (*payloads.VM, bool, error) {
	logger := log.FromContext(ctx)
	vmWasStarted := false

	if vm.PowerState != payloads.PowerStateRunning {
		taskID, err := xoClient.Client.VM().Start(ctx, vm.ID, nil)
		if err != nil {
			if isUnmarshalError(err) {
				logger.Info("VM start issued (unmarshal from old API), checking state", "id", vm.ID.String())
				if freshVM, checkErr := xoClient.Client.VM().GetByID(ctx, vm.ID); checkErr == nil {
					vm = freshVM
				}
				if vm.PowerState == payloads.PowerStateRunning {
					vmWasStarted = true
				}
			} else {
				logger.Error(err, "Failed to start VM", "id", vm.ID.String())
				return nil, false, WithConditionUpdate(ctx, c, vatesMachine, err, metav1.ConditionFalse, "VmStartFailed")
			}
		} else {
			logger.Info("Waiting for VM start task", "task", taskID)

			task, err := xoClient.Client.Task().Wait(ctx, taskID)
			if err != nil {
				logger.Error(err, "Failed to wait for VM start", "task", taskID)
				return nil, false, WithConditionUpdate(ctx, c, vatesMachine, err, metav1.ConditionFalse, "VmStartFailed")
			}
			if task.Status != payloads.Success {
				err := fmt.Errorf("VM start task %s status: %s", taskID, task.Status)
				logger.Info("VM start task did not succeed", "task", taskID, "status", task.Status)
				return nil, false, WithConditionUpdate(ctx, c, vatesMachine, err, metav1.ConditionFalse, "VmStartFailed")
			}

			vmWasStarted = true
			logger.Info("VM started", "name", vm.NameLabel, "id", vm.ID.String())
		}
	} else {
		logger.Info("VM already running, skipping start", "id", vm.ID.String())
	}

	vm, err := xoClient.Client.VM().GetByID(ctx, vm.ID)
	if err != nil {
		logger.Error(err, "Failed to re-fetch VM after start", "id", vm.ID.String())
		return nil, false, WithConditionUpdate(ctx, c, vatesMachine, err, metav1.ConditionFalse, "VmRefetchFailed")
	}

	return vm, vmWasStarted, nil
}

// ResolveVMIP returns the IPv4 address of the VM, falling back from the V2
// MainIpAddress (which can be an IPv6 link-local) to the V1 Addresses field.
// When both are empty, it retries with exponential backoff capped at 2 minutes.
func ResolveVMIP(ctx context.Context, xoClient *xok8scommon.XoClient, vm *payloads.VM) string {
	logger := log.FromContext(ctx)

	vmIP := vm.MainIpAddress
	if vmIP != "" && !strings.HasPrefix(vmIP, "fe80:") {
		return vmIP
	}

	v1Client := xoClient.Client.V1Client()
	if v1Client == nil {
		logger.Info("V1 client not available, cannot resolve IP via V1 API", "id", vm.ID.String())
		return vmIP
	}

	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	backoff := 1 * time.Second

	for {
		v1VM, err := v1Client.GetVm(xoclient.Vm{Id: vm.ID.String()})
		if err != nil {
			logger.Error(err, "Failed to get VM from V1 API, retrying", "id", vm.ID.String())
		} else if v1VM != nil {
			for _, addr := range v1VM.Addresses {
				if addr != "" && !strings.HasPrefix(addr, "fe80:") && strings.Contains(addr, ".") {
					logger.Info("Resolved VM IP from V1 Addresses", "id", vm.ID.String(), "ip", addr)
					return addr
				}
			}
			logger.Info("VM IP not yet available, retrying", "id", vm.ID.String(), "mainIp", vm.MainIpAddress, "v1Addresses", v1VM.Addresses)
		} else {
			logger.Info("V1 API returned nil VM, retrying", "id", vm.ID.String())
		}

		select {
		case <-ctx.Done():
			logger.Info("Context cancelled while resolving VM IP", "id", vm.ID.String())
			return ""
		case <-deadline.C:
			logger.Info("Timeout resolving VM IP after 2m", "id", vm.ID.String())
			return ""
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 8*time.Second)
	}
}

// ManageVIFs ensures all desired VIFs are connected and sets the allowed IP
// range if configured.
func ManageVIFs(ctx context.Context, xoClient *xok8scommon.XoClient, vatesMachine *infrastructurev1beta2.XOMachine, vm *payloads.VM) error {
	if vatesMachine.Spec.NetworkConfig == nil || len(vatesMachine.Spec.NetworkConfig.Networks) == 0 {
		return nil
	}

	logger := log.FromContext(ctx)
	v1Client := xoClient.Client.V1Client()
	if v1Client == nil {
		return fmt.Errorf("v1 client not available (V1Client returned nil)")
	}
	v1VM := &xoclient.Vm{Id: vm.ID.String()}

	logger.Info("Checking existing VIFs")

	existingVIFs, err := v1Client.GetVIFs(v1VM)
	if err != nil {
		return fmt.Errorf("failed to get existing VIFs: %w", err)
	}

	for _, vif := range existingVIFs {
		if !vif.Attached {
			logger.Info("VIF is disconnected, connecting", "vifId", vif.Id, "network", vif.Network)
			if err := v1Client.ConnectVIF(&vif); err != nil {
				logger.Error(err, "Failed to connect VIF, will retry", "vifId", vif.Id)
				return fmt.Errorf("failed to connect VIF %s: %w", vif.Id, err)
			}
			logger.Info("Connected VIF", "vifId", vif.Id)
		}
	}

	if vatesMachine.Spec.NetworkConfig.AllowedIPRange != "" {
		for _, vif := range existingVIFs {
			if rawClient, ok := v1Client.(*xoclient.Client); ok {
				var success bool
				err := rawClient.Call("vif.set", map[string]any{
					"id":           vif.Id,
					"ipv4_allowed": vatesMachine.Spec.NetworkConfig.AllowedIPRange,
				}, &success)
				if err != nil {
					logger.Error(err, "Failed to set allowed IP range on VIF (may not be supported by this XO version), continuing", "vifId", vif.Id)
				} else {
					logger.Info("Set allowed IP range on VIF", "vifId", vif.Id, "range", vatesMachine.Spec.NetworkConfig.AllowedIPRange)
				}
			} else {
				logger.Info("Cannot set allowed IP range: raw client access not available")
			}
		}
	}

	return nil
}

// WaitForVMReady waits for a recently-created or -started VM to become ready.
// It starts the VM if needed, resolves the IP, applies a stabilization delay
// when appropriate, manages VIFs, and marks the machine as Ready.
func WaitForVMReady(ctx context.Context, c client.Client, xoClient *xok8scommon.XoClient, vatesMachine *infrastructurev1beta2.XOMachine, vm *payloads.VM) (reconcile.Result, error) {
	logger := log.FromContext(ctx)

	vm, _, err := StartVM(ctx, c, xoClient, vatesMachine, vm)
	if err != nil {
		return reconcile.Result{}, err
	}

	logger.Info("Re-fetched VM state after start", "id", vm.ID.String(), "powerState", vm.PowerState, "mainIp", vm.MainIpAddress)

	vmIP := ResolveVMIP(ctx, xoClient, vm)

	if vmIP == "" {
		logger.Info("VM started but IP not yet reported by guest tools, requeuing", "id", vm.ID.String())
		if ue := UpdateCondition(ctx, c, vatesMachine, metav1.ConditionFalse, "WaitingForIPAddress", "VM is running, waiting for IP address from Xen guest tools"); ue != nil {
			return reconcile.Result{}, ue
		}
		return reconcile.Result{RequeueAfter: 3 * time.Second}, nil
	}

	if err := ManageVIFs(ctx, xoClient, vatesMachine, vm); err != nil {
		logger.Error(err, "Failed to manage VIFs")
		return reconcile.Result{}, WithConditionUpdate(ctx, c, vatesMachine, err, metav1.ConditionFalse, "VifManagementFailed")
	}

	vatesMachine.Status.Addresses = []corev1.NodeAddress{
		{
			Type:    corev1.NodeInternalIP,
			Address: vmIP,
		},
	}

	vatesMachine.Status.Ready = true
	vatesMachine.Status.Initialization = &infrastructurev1beta2.InitializationStatus{Provisioned: true}
	if ue := UpdateCondition(ctx, c, vatesMachine, metav1.ConditionTrue, "VmReady", "VM is created, running and has an IP address"); ue != nil {
		return reconcile.Result{}, ue
	}

	return reconcile.Result{}, nil
}
