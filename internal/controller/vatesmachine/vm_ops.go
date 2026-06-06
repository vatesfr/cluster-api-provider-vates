package vatesmachine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

// CreateVM creates a new VM from the template with the given parameters. It
// handles cloud-config, memory, VIFs, CPUs, boot order, and providerID.
func CreateVM(ctx context.Context, c client.Client, xoClient *xok8scommon.XoClient, vatesMachine *infrastructurev1beta2.VatesMachine, poolID uuid.UUID, templateID uuid.UUID, cloudConfig string, vmName string) (*payloads.VM, error) {
	logger := log.FromContext(ctx)

	createParams := &payloads.CreateVMParams{
		NameLabel:   vmName,
		Template:    templateID,
		AutoPoweron: ptr.To(false),
		Clone:       ptr.To(true),
		CloudConfig: ptr.To(cloudConfig),
	}

	if vatesMachine.Spec.NetworkConfig != nil && vatesMachine.Spec.NetworkConfig.GuestConfig != "" {
		createParams.NetworkConfig = ptr.To(vatesMachine.Spec.NetworkConfig.GuestConfig)
		logger.Info("Passing guest network config to VM creation", "networkConfig", vatesMachine.Spec.NetworkConfig.GuestConfig)
	}

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

	if vatesMachine.Spec.NetworkConfig != nil && len(vatesMachine.Spec.NetworkConfig.Networks) > 0 {
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

	vm, err := xoClient.Client.VM().Create(ctx, poolID, createParams)
	if err != nil {
		logger.Error(err, "Failed to create VM", "name", vmName)
		return nil, WithConditionUpdate(ctx, c, vatesMachine, err, metav1.ConditionFalse, "VmCreationFailed")
	}

	logger.Info("VM created", "name", vm.NameLabel, "id", vm.ID.String(), "pool", poolID.String())

	diskSize := ""
	if vatesMachine.Spec.ResourceSet != nil {
		diskSize = vatesMachine.Spec.ResourceSet.DiskSize
	}
	if diskSize != "" {
		diskQty, qErr := resource.ParseQuantity(diskSize)
		if qErr == nil {
			if v1Ok {
				disks, dErr := v1Client.GetDisks(&xoclient.Vm{Id: vm.ID.String()})
				if dErr != nil {
					logger.Error(dErr, "Failed to get disks for VM resize")
				} else if len(disks) > 0 {
					currentSize := int64(disks[0].Size)
					requestedSize := diskQty.Value()
					if currentSize >= requestedSize {
						logger.Info("VM disk already at or above requested size, skipping resize", "currentSize", currentSize, "requestedSize", requestedSize, "diskSize", diskSize)
					} else {
						var success bool
						if err := v1Concrete.Call("vdi.set", map[string]any{
							"id":   disks[0].VDIId,
							"size": requestedSize,
						}, &success); err != nil {
							logger.Info("Failed to set disk size (will continue)", "diskSize", diskSize, "requestedSize", requestedSize, "currentSize", currentSize, "error", err)
						} else {
							logger.Info("Set VM disk size", "diskSize", diskSize, "success", success)
						}
					}
				}
			}
		}
	}

	if vatesMachine.Spec.ResourceSet != nil && vatesMachine.Spec.ResourceSet.CPUs != nil {
		cpus := int(*vatesMachine.Spec.ResourceSet.CPUs)
		if v1Ok {
			var success bool
			if err := v1Concrete.Call("vm.set", map[string]any{
				"id":   vm.ID.String(),
				"CPUs": cpus,
			}, &success); err != nil {
				logger.Error(err, "Failed to set CPUs after VM creation", "cpus", cpus)
			} else {
				logger.Info("Set VM CPUs", "cpus", cpus)
			}
		}
	}

	// TODO: Change this as soon as API v2 allows the boot order configuration
	if v1Ok {
		var success bool
		if err := v1Concrete.Call("vm.setBootOrder", map[string]any{
			"vm":    vm.ID.String(),
			"order": "cd",
		}, &success); err != nil {
			logger.Error(err, "Failed to set boot order to disk", "id", vm.ID.String())
		} else {
			logger.Info("Set VM boot order to disk", "id", vm.ID.String(), "success", success)
		}
	} else {
		logger.Info("V1 client not available or type assertion failed, skipping boot order configuration")
	}

	providerID := xok8scommon.GetProviderID(poolID, vm)
	vatesMachine.Spec.ProviderID = &providerID
	if err := c.Update(ctx, vatesMachine); err != nil {
		logger.Error(err, "Failed to save providerID in spec after VM creation")
		return nil, err
	}
	vatesMachine.Status.ProviderID = &providerID
	if err := c.Status().Update(ctx, vatesMachine); err != nil {
		logger.Error(err, "Failed to save providerID in status after VM creation")
		return nil, err
	}
	logger.Info("ProviderID saved", "providerID", providerID)

	return vm, nil
}

// LookupExistingVM looks up an already-known VM by its ID.
func LookupExistingVM(ctx context.Context, c client.Client, xoClient *xok8scommon.XoClient, vatesMachine *infrastructurev1beta2.VatesMachine, vmID uuid.UUID) (*payloads.VM, error) {
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
func StartVM(ctx context.Context, c client.Client, xoClient *xok8scommon.XoClient, vatesMachine *infrastructurev1beta2.VatesMachine, vm *payloads.VM) (*payloads.VM, bool, error) {
	logger := log.FromContext(ctx)
	vmWasStarted := false

	if vm.PowerState != payloads.PowerStateRunning {
		taskID, err := xoClient.Client.VM().Start(ctx, vm.ID, nil)
		if err != nil {
			logger.Error(err, "Failed to start VM", "id", vm.ID.String())
			return nil, false, WithConditionUpdate(ctx, c, vatesMachine, err, metav1.ConditionFalse, "VmStartFailed")
		}

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
// When both are empty, it retries with exponential backoff for up to 2 minutes.
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

	for attempt := range 30 {
		select {
		case <-ctx.Done():
			logger.Info("Context cancelled while resolving VM IP", "id", vm.ID.String())
			return ""
		default:
		}

		v1VM, err := v1Client.GetVm(xoclient.Vm{Id: vm.ID.String()})
		if err != nil {
			logger.Error(err, "Failed to get VM from V1 API, retrying", "id", vm.ID.String(), "attempt", attempt)
			time.Sleep(4 * time.Second)
			continue
		}
		if v1VM == nil {
			logger.Info("V1 API returned nil VM, retrying", "id", vm.ID.String(), "attempt", attempt)
			time.Sleep(4 * time.Second)
			continue
		}

		if len(v1VM.Addresses) > 0 {
			for _, addr := range v1VM.Addresses {
				if addr != "" && !strings.HasPrefix(addr, "fe80:") && strings.Contains(addr, ".") {
					logger.Info("Resolved VM IP from V1 Addresses", "id", vm.ID.String(), "ip", addr, "attempt", attempt)
					return addr
				}
			}
		}

		logger.Info("VM IP not yet available, retrying", "id", vm.ID.String(), "attempt", attempt, "mainIp", vm.MainIpAddress, "v1Addresses", v1VM.Addresses)
		time.Sleep(4 * time.Second)
	}

	logger.Info("Failed to resolve VM IP after retries", "id", vm.ID.String())
	return ""
}

// ManageVIFs ensures all desired VIFs are connected and sets the allowed IP
// range if configured.
func ManageVIFs(ctx context.Context, xoClient *xok8scommon.XoClient, vatesMachine *infrastructurev1beta2.VatesMachine, vm *payloads.VM) error {
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
func WaitForVMReady(ctx context.Context, c client.Client, xoClient *xok8scommon.XoClient, vatesMachine *infrastructurev1beta2.VatesMachine, vm *payloads.VM) (reconcile.Result, error) {
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
