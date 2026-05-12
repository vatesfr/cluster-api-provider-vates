package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VatesMachineSpec defines the desired state of a Xen Orchestra VM.
type VatesMachineSpec struct {
	// ProviderID is the Xen Orchestra provider ID (e.g. xenorchestra://pool-uuid/vm-uuid).
	// Set by the controller after the VM is created.
	// +optional
	ProviderID *string `json:"providerID,omitempty"`

	// PoolID is the Xen Orchestra pool UUID where the VM will be created.
	// If empty, the controller will use the template's pool.
	// +optional
	PoolID string `json:"poolID,omitempty"`

	// PoolName is the Xen Orchestra pool name where the VM will be created
	// (alternative to PoolID).
	// +optional
	PoolName string `json:"poolName,omitempty"`

	// TemplateID is the XO VM template UUID to clone.
	// +optional
	TemplateID string `json:"templateID,omitempty"`

	// TemplateName is the XO VM template name to clone (alternative to TemplateID).
	// +optional
	TemplateName string `json:"templateName,omitempty"`

	// NamePrefix is the prefix for the VM name.
	// +kubebuilder:validation:Required
	NamePrefix string `json:"namePrefix"`

	// NetworkConfig specifies the network configuration for the VM.
	// +optional
	NetworkConfig *NetworkConfig `json:"networkConfig,omitempty"`

	// ResourceSet is the resource set configuration for the VM (CPU, RAM, disk).
	// +optional
	ResourceSet *ResourceSet `json:"resourceSet,omitempty"`

	// FailureDomain is the failure domain unique identifier this Machine should be attached to, as defined in ClusterStatus.FailureDomains.
	// +optional
	FailureDomain *string `json:"failureDomain,omitempty"`

	// BootstrapData is the bootstrap data (cloud-init) to inject into the VM.
	// If empty, the controller will try to read it from the owner Machine's bootstrap secret.
	// +optional
	BootstrapData string `json:"bootstrapData,omitempty"`
}

type NetworkConfig struct {
	// AllowedIPRange is a CIDR range of IPs allowed to access the VM (e.g. "10.30.139.0/24").
	// Applied to all VIFs on the VM. If empty, no IP restriction is set.
	// +optional
	AllowedIPRange string `json:"allowedIPRange,omitempty"`

	// Networks is a list of networks to attach to the VM.
	// +optional
	Networks []Network `json:"networks,omitempty"`

	// GuestConfig is the cloud-init network configuration (netplan format) to apply inside the VM.
	// This is passed directly to XO via the NetworkConfig field of CreateVM, allowing the guest
	// OS to receive a static IP or custom network setup before the first boot.
	// Example (netplan v2):
	//   version: 2
	//   ethernets:
	//     eth0:
	//       dhcp4: false
	//       addresses:
	//         - 10.30.139.10/16
	//       gateway4: 10.30.0.1
	// +optional
	GuestConfig string `json:"guestConfig,omitempty"`
}

type Network struct {
	// NetworkID is the XO network UUID to attach.
	// +optional
	NetworkID string `json:"networkID,omitempty"`

	// Name is the XO network name to attach (alternative to NetworkID).
	// +optional
	Name string `json:"name,omitempty"`
}

type ResourceSet struct {
	// CPUs is the number of vCPUs.
	// +optional
	// +kubebuilder:validation:Minimum=1
	CPUs *int32 `json:"cpus,omitempty"`

	// Memory is the memory allocated to the VM in Kubernetes resource format (e.g. "8Gi", "8192Mi").
	// +optional
	Memory string `json:"memory,omitempty"`

	// DiskGB is the disk size in GB.
	// +optional
	// +kubebuilder:validation:Minimum=10
	DiskGB *int64 `json:"diskGB,omitempty"`
}

// VatesMachineStatus defines the observed state of VatesMachine.
type VatesMachineStatus struct {
	// Ready indicates the VM is ready and the Kubernetes node has joined the cluster.
	// +optional
	Ready bool `json:"ready"`

	// ProviderID is the Xen Orchestra provider ID (e.g. xenorchestra://pool-uuid/vm-uuid).
	// +optional
	ProviderID *string `json:"providerID,omitempty"`

	// Addresses contains the VM's IP addresses.
	// +optional
	Addresses []corev1.NodeAddress `json:"addresses,omitempty"`

	// Initialization provides observations of the VatesMachine initialization process.
	// NOTE: This field is part of the Cluster API contract and is used to orchestrate provisioning.
	// +optional
	Initialization *InitializationStatus `json:"initialization,omitempty"`

	// FailureReason is the reason for the failure (if any).
	// +optional
	FailureReason *string `json:"failureReason,omitempty"`

	// FailureMessage is the detailed error message (if any).
	// +optional
	FailureMessage *string `json:"failureMessage,omitempty"`

	// Conditions represent the current state of the VatesMachine resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vatesmachines,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="ProviderID",type=string,JSONPath=".status.providerID",priority=1
// +kubebuilder:printcolumn:name="TemplateID",type=string,JSONPath=".spec.templateID"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// VatesMachine is the Schema for the vatesmachines API.
type VatesMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec   VatesMachineSpec   `json:"spec,omitempty"`
	Status VatesMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VatesMachineList contains a list of VatesMachine.
type VatesMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []VatesMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VatesMachine{}, &VatesMachineList{})
}
