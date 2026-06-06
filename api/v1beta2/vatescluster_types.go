package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VatesClusterSpec defines the desired state of a Xen Orchestra cluster.
type VatesClusterSpec struct {
	// ControlPlaneEndpoint is the endpoint for the control plane.
	// +optional
	ControlPlaneEndpoint *APIEndpoint `json:"controlPlaneEndpoint,omitempty"`

	// ControlPlaneLB selects the control plane load balancer implementation.
	// Supported values: "kube-vip". When set, the controller injects setup
	// scripts into the cloud-init data of control plane machines.
	// +optional
	// +kubebuilder:validation:Enum=kube-vip
	ControlPlaneLB *string `json:"controlPlaneLB,omitempty"`
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

// VatesClusterStatus defines the observed state of VatesCluster.
type VatesClusterStatus struct {
	// Ready indicates the cluster infrastructure is ready.
	// +optional
	Ready bool `json:"ready"`

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint *APIEndpoint `json:"controlPlaneEndpoint,omitempty"`

	// Initialization provides observations of the VatesCluster initialization process.
	// NOTE: This field is part of the Cluster API contract and is used to orchestrate provisioning.
	// +optional
	Initialization *InitializationStatus `json:"initialization,omitempty"`

	// FailureReason is the reason for the failure (if any).
	// +optional
	FailureReason *string `json:"failureReason,omitempty"`

	// FailureMessage is the detailed error message (if any).
	// +optional
	FailureMessage *string `json:"failureMessage,omitempty"`

	// Conditions represent the current state of the VatesCluster resource.
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
// +kubebuilder:resource:path=vatesclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=".spec.controlPlaneEndpoint.host",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// VatesCluster is the Schema for the vatesclusters API.
type VatesCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec   VatesClusterSpec   `json:"spec,omitempty"`
	Status VatesClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VatesClusterList contains a list of VatesCluster.
type VatesClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []VatesCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VatesCluster{}, &VatesClusterList{})
}
