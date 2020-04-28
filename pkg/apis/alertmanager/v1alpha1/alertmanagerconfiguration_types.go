package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AlertManagerConfigurationSpec defines the desired state of AlertManagerConfiguration
type AlertManagerConfigurationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// AlertManagerConfigurationStatus defines the observed state of AlertManagerConfiguration
type AlertManagerConfigurationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AlertManagerConfiguration is the Schema for the alertmanagerconfigurations API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=alertmanagerconfigurations,scope=Namespaced
type AlertManagerConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AlertManagerConfigurationSpec   `json:"spec,omitempty"`
	Status AlertManagerConfigurationStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AlertManagerConfigurationList contains a list of AlertManagerConfiguration
type AlertManagerConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AlertManagerConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AlertManagerConfiguration{}, &AlertManagerConfigurationList{})
}
