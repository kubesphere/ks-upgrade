package users

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type User struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec UserSpec `json:"spec"`

	// +optional
	Status UserStatus `json:"status,omitempty"`
}

// UserStatus defines the observed state of User
type UserStatus struct {
	// The user status
	// +optional
	State UserState `json:"state,omitempty"`
	// +optional
	Reason string `json:"reason,omitempty"`
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
	// Last login attempt timestamp
	// +optional
	LastLoginTime *metav1.Time `json:"lastLoginTime,omitempty"`
}

type UserState string

type UserSpec struct {
	// Unique email address(https://www.ietf.org/rfc/rfc5322.txt).
	Email string `json:"email"`
	// The preferred written or spoken language for the user.
	// +optional
	Lang string `json:"lang,omitempty"`
	// Description of the user.
	// +optional
	Description string `json:"description,omitempty"`
	// +optional
	DisplayName string `json:"displayName,omitempty"`
	// +optional
	Groups []string `json:"groups,omitempty"`
	// password will be encrypted by mutating admission webhook
	EncryptedPassword string `json:"password,omitempty"`
}
