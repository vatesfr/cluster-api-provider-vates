package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VatesClusterTemplateResource describes the data needed to create a VatesCluster from a template.
type VatesClusterTemplateResource struct {
	// Spec is the specification of the desired behavior of the cluster.
	Spec VatesClusterSpec `json:"spec"`
}

// VatesClusterTemplateSpec defines the desired state of VatesClusterTemplate.
type VatesClusterTemplateSpec struct {
	Template VatesClusterTemplateResource `json:"template"`
}

// VatesClusterTemplateStatus defines the observed state of VatesClusterTemplate.
type VatesClusterTemplateStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=vatesclustertemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// VatesClusterTemplate is the Schema for the vatesclustertemplates API.
type VatesClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec   VatesClusterTemplateSpec   `json:"spec,omitempty"`
	Status VatesClusterTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VatesClusterTemplateList contains a list of VatesClusterTemplate.
type VatesClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []VatesClusterTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VatesClusterTemplate{}, &VatesClusterTemplateList{})
}
