package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// XOClusterSpec defines the desired state of a Xen Orchestra cluster.
type XOClusterSpec struct {
	// ControlPlaneEndpoint is the endpoint for the control plane.
	// +optional
	ControlPlaneEndpoint *APIEndpoint `json:"controlPlaneEndpoint,omitempty"`

	// ControlPlaneLB selects the control plane load balancer implementation.
	// Supported values: "kube-vip". When set, the controller injects setup
	// scripts into the cloud-init data of control plane machines.
	// +optional
	// +kubebuilder:validation:Enum=kube-vip
	ControlPlaneLB *string `json:"controlPlaneLB,omitempty"`

	// InjectSSHKeys enables injection of the XO user's SSH public keys into
	// the cloud-init userdata of all VMs in this cluster. When enabled, the
	// controller fetches the SSH keys from the authenticated XO user profile
	// and merges them into the ssh_authorized_keys section of each VM's
	// cloud-config. Disabled by default to avoid modifying user-provided
	// bootstrap data without explicit opt-in.
	// +optional
	// +kubebuilder:default=false
	InjectSSHKeys bool `json:"injectSSHKeys,omitempty"`

	// IdentityRef is a reference to a Secret containing XO credentials.
	// The Secret must contain 'url' and 'token' data keys, and optionally
	// 'insecure'. The Secret must be in the same namespace as the XOCluster.
	// If not set, falls back to the controller's global credentials.
	// +optional
	IdentityRef *corev1.LocalObjectReference `json:"identityRef,omitempty"`

	// Addons selects which workload cluster addons the controller installs
	// via ClusterResourceSet. When nil, defaults to CCM and CSI enabled and
	// no CNI.
	// +optional
	Addons *AddonsSpec `json:"addons,omitempty"`
}

// AddonsSpec selects which addons the controller installs in the workload
// cluster via ClusterResourceSet. Each addon is optional and independently
// controllable.
type AddonsSpec struct {
	// CCM installs the Xen Orchestra cloud-controller-manager (node addresses,
	// service load balancers). Defaults to true.
	// +optional
	// +kubebuilder:default=true
	CCM *bool `json:"ccm,omitempty"`

	// CSI installs the Xen Orchestra CSI driver. Defaults to true.
	// +optional
	// +kubebuilder:default=true
	CSI *bool `json:"csi,omitempty"`

	// CNI selects the Container Network Interface installed via
	// ClusterResourceSet. Supported values: "none", "cilium". When unset
	// (or "none"), no CNI is installed by the controller.
	// +optional
	// +kubebuilder:validation:Enum=none;cilium
	CNI *string `json:"cni,omitempty"`
}

// APIEndpoint represents a reachable Kubernetes API endpoint.
type APIEndpoint struct {
	// Host is the DNS name or IP of the API server.
	// +kubebuilder:validation:Required
	Host string `json:"host"`

	// Port is the port of the API server.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`

	// Subnet is the CIDR subnet mask used by the control plane load balancer.
	// If not set, the load balancer implementation will auto-detect.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=32
	Subnet *int32 `json:"subnet,omitempty"`
}

// XOClusterStatus defines the observed state of XOCluster.
type XOClusterStatus struct {
	// Ready indicates the cluster infrastructure is ready.
	// +optional
	Ready bool `json:"ready"`

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint *APIEndpoint `json:"controlPlaneEndpoint,omitempty"`

	// Initialization provides observations of the XOCluster initialization process.
	// NOTE: This field is part of the Cluster API contract and is used to orchestrate provisioning.
	// +optional
	Initialization *InitializationStatus `json:"initialization,omitempty"`

	// FailureReason is the reason for the failure (if any).
	// +optional
	FailureReason *string `json:"failureReason,omitempty"`

	// FailureMessage is the detailed error message (if any).
	// +optional
	FailureMessage *string `json:"failureMessage,omitempty"`

	// Conditions represent the current state of the XOCluster resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// InitializationStatus provides observations of the resource initialization process.
// NOTE: Fields in this struct are part of the Cluster API contract and are used to orchestrate provisioning.
type InitializationStatus struct {
	// provisioned is true when the infrastructure provider reports that the resource is fully provisioned.
	// NOTE: This field is part of the Cluster API contract.
	// +optional
	Provisioned bool `json:"provisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=xoclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=".spec.controlPlaneEndpoint.host",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// XOCluster is the Schema for the xoclusters API.
type XOCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec   XOClusterSpec   `json:"spec,omitempty"`
	Status XOClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// XOClusterList contains a list of XOCluster.
type XOClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []XOCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&XOCluster{}, &XOClusterList{})
}
