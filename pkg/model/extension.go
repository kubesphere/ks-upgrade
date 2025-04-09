package model

import corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"

type ExtensionRef struct {
	Name              string                          `json:"name"`
	Version           string                          `json:"version"`
	Config            string                          `json:"config"`
	ClusterScheduling *corev1alpha1.ClusterScheduling `json:"clusterScheduling,omitempty"`
}
