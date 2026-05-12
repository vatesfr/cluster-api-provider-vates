package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VatesMachineTemplateResource describes the data needed to create a VatesMachine from a template.
type VatesMachineTemplateResource struct {
	// Spec is the specification of the desired behavior of the machine.
	Spec VatesMachineSpec `json:"spec"`
}

// VatesMachineTemplateSpec defines the desired state of VatesMachineTemplate.
type VatesMachineTemplateSpec struct {
	Template VatesMachineTemplateResource `json:"template"`
}

// VatesMachineTemplateStatus defines the observed state of VatesMachineTemplate.
type VatesMachineTemplateStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=vatesmachinetemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// VatesMachineTemplate is the Schema for the vatesmachinetemplates API.
type VatesMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec   VatesMachineTemplateSpec   `json:"spec,omitempty"`
	Status VatesMachineTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VatesMachineTemplateList contains a list of VatesMachineTemplate.
type VatesMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []VatesMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VatesMachineTemplate{}, &VatesMachineTemplateList{})
}
