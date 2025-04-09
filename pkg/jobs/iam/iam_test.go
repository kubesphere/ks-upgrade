package iam

import (
	"context"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"testing"
)

func Test_upgradeJob_PostUpgrade(t *testing.T) {
	conf := config.GetConfigOrDie()

	localScheme := runtime.NewScheme()
	scheme.AddToScheme(localScheme)
	apiextensionsv1.AddToScheme(localScheme)

	client, err := runtimeclient.New(conf, runtimeclient.Options{
		Scheme: localScheme,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	userCRD := &apiextensionsv1.CustomResourceDefinition{}
	if err := client.Get(context.Background(), runtimeclient.ObjectKey{Name: "users.iam.kubesphere.io"}, userCRD); err != nil {
		t.Fatalf("failed to get user crd: %v", err)
	}
	for i := 0; i < len(userCRD.Spec.Versions); i++ {
		version := userCRD.Spec.Versions[i]
		if version.Name == "v1alpha2" {
			userCRD.Spec.Versions = append(userCRD.Spec.Versions[:i], userCRD.Spec.Versions[i+1:]...)
			if err := client.Update(context.Background(), userCRD); err != nil {
				t.Fatalf("failed to update user crd: %v", err)
			}
			break
		}
	}
	userCRD.Status.StoredVersions = []string{"v1beta1"}
	if err := client.Status().Update(context.Background(), userCRD); err != nil {
		t.Fatalf("failed to update user crd: %v", err)
	}
}
