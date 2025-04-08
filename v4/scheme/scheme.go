package scheme

import (
	notificationv2beta2 "github.com/kubesphere/notification-manager/apis/v2beta2"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	devopsv1alpha3 "kubesphere.io/ks-upgrade/v3/api/devops/v1alpha3"
	applicationv2 "kubesphere.io/ks-upgrade/v4/api/application/v2"
	clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	extensionsv1alpha1 "kubesphere.io/ks-upgrade/v4/api/extensions/v1alpha1"
	gatewayv2alpha1 "kubesphere.io/ks-upgrade/v4/api/gateway/v2alpha1"
	iamv1beta1 "kubesphere.io/ks-upgrade/v4/api/iam/v1beta1"
	oauthv1alpha1 "kubesphere.io/ks-upgrade/v4/api/oauth/v1alpha1"
	quotav1alpha2 "kubesphere.io/ks-upgrade/v4/api/quota/v1alpha2"
	storagev1alpha1 "kubesphere.io/ks-upgrade/v4/api/storage/v1alpha1"
	tenantv1beta1 "kubesphere.io/ks-upgrade/v4/api/tenant/v1beta1"
)

func AddToScheme(scheme *runtime.Scheme) error {
	var v4SchemeBuilder = runtime.SchemeBuilder{
		apiextensions.AddToScheme,
		clientgoscheme.AddToScheme,
		apiext.AddToScheme,
		apiregistrationv1.AddToScheme,
		applicationv2.AddToScheme,
		clusterv1alpha1.AddToScheme,
		corev1alpha1.AddToScheme,
		devopsv1alpha3.AddToScheme,
		extensionsv1alpha1.AddToScheme,
		gatewayv2alpha1.AddToScheme,
		iamv1beta1.AddToScheme,
		oauthv1alpha1.AddToScheme,
		quotav1alpha2.AddToScheme,
		storagev1alpha1.AddToScheme,
		tenantv1beta1.AddToScheme,
		monitoringv1.AddToScheme,
		notificationv2beta2.AddToScheme,
	}
	return v4SchemeBuilder.AddToScheme(scheme)
}
