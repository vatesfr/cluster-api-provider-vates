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

	// Tags are additional Xen Orchestra tags applied to every VM of the
	// cluster, on top of the identifying tags set by the controller and of
	// the machine-level XOMachine spec.tags. Tags are only added: removing a
	// tag from this list does not remove it from existing VMs.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=255
	Tags []string `json:"tags,omitempty"`

	// Addons selects which workload cluster addons the controller installs
	// via ClusterResourceSet. When nil, defaults to CCM and CSI enabled and
	// no CNI.
	// +optional
	Addons *AddonsSpec `json:"addons,omitempty"`
}

// AddonsSpec selects which addons the controller installs in the workload
// cluster via ClusterResourceSet. Each addon is optional and independently
// controllable.
// +kubebuilder:validation:XValidation:rule="!(has(self.cilium) && has(self.cilium.kubeProxyReplacement) && self.cilium.kubeProxyReplacement) || (has(self.cni) && self.cni == 'cilium')",message="cilium.kubeProxyReplacement requires cni to be cilium"
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

	// Cilium configures the Cilium CNI installed when cni is "cilium". When
	// nil, Cilium runs alongside kube-proxy.
	// +optional
	Cilium *CiliumSpec `json:"cilium,omitempty"`

	// NodeOutOfService configures the CCM cloud-node-out-of-service controller
	// (Non-Graceful Node Shutdown). When enabled, the CCM applies the
	// node.kubernetes.io/out-of-service taint to nodes whose Xen Orchestra VM is
	// no longer running, so kube-controller-manager can force-detach their
	// volumes instead of waiting for its 6 minute maxWaitForUnmountDuration
	// timer. When nil, the controller is enabled with the CCM defaults.
	// +optional
	NodeOutOfService *NodeOutOfServiceSpec `json:"nodeOutOfService,omitempty"`
}

// CiliumSpec configures the Cilium CNI addon.
//
// With kube-proxy replacement, kube-proxy must not be deployed in the workload
// cluster: on Talos set cluster.proxy.disabled to true (together with
// cluster.network.cni.name: none) in the machine config of every node. The
// Cilium manifest is applied once (ClusterResourceSet ApplyOnce strategy), so
// changing these fields does not reconfigure an already provisioned cluster.
// +kubebuilder:validation:XValidation:rule="has(self.k8sServiceHost) == has(self.k8sServicePort)",message="k8sServiceHost and k8sServicePort must be set together"
// +kubebuilder:validation:XValidation:rule="!has(self.k8sServiceHost) || (has(self.kubeProxyReplacement) && self.kubeProxyReplacement)",message="k8sServiceHost and k8sServicePort require kubeProxyReplacement"
type CiliumSpec struct {
	// KubeProxyReplacement makes Cilium fully replace kube-proxy (Services,
	// NodePorts, ...). The Cilium agent and operator then reach the API server
	// directly through k8sServiceHost:k8sServicePort instead of the kubernetes
	// Service, which is not routed until Cilium is running. Defaults to false.
	// +optional
	// +kubebuilder:default=false
	KubeProxyReplacement *bool `json:"kubeProxyReplacement,omitempty"`

	// K8sServiceHost is the API server host used by Cilium when
	// kubeProxyReplacement is enabled. It must be reachable from every node
	// without a CNI. Defaults to "localhost" (Talos KubePrism) on Talos
	// clusters and to the control plane endpoint host otherwise.
	// +optional
	// +kubebuilder:validation:MinLength=1
	K8sServiceHost string `json:"k8sServiceHost,omitempty"`

	// K8sServicePort is the API server port used with K8sServiceHost.
	// Defaults to 7445 (Talos KubePrism) on Talos clusters and to the control
	// plane endpoint port otherwise.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	K8sServicePort int32 `json:"k8sServicePort,omitempty"`
}

// NodeOutOfServiceSpec configures the CCM out-of-service controller
// (Non-Graceful Node Shutdown). The controller is enabled by default; it can be
// turned off per cluster. The CCM applies the node.kubernetes.io/out-of-service
// taint to a node whose Xen Orchestra VM is deleted, halted, paused or suspended
// and whose kubelet reports NotReady, so kube-controller-manager force-detaches
// its volumes instead of waiting for its 6 minute maxWaitForUnmountDuration
// timer: only the Kubernetes VolumeAttachment is released, so the persistent
// volume can be reused. The periods are optional: when unset the corresponding
// flag is not rendered, so the CCM keeps its own default value.
type NodeOutOfServiceSpec struct {
	// Enabled toggles the out-of-service taint controller. Defaults to true.
	// +optional
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`

	// SyncPeriod overrides how often the controller reconciles the nodes.
	// +optional
	SyncPeriod *metav1.Duration `json:"syncPeriod,omitempty"`

	// GracePeriod overrides how long a powered-off VM must stay down while its
	// node is NotReady before the out-of-service taint is applied. A VM deleted
	// from Xen Orchestra is tainted immediately.
	// +optional
	GracePeriod *metav1.Duration `json:"gracePeriod,omitempty"`
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
