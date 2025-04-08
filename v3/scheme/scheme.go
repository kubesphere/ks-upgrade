package scheme

import (
	notificationv2beta2 "github.com/kubesphere/notification-manager/apis/v2beta2"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	loggingv1alpha2 "kubesphere.io/fluentbit-operator/api/fluentbitoperator/v1alpha2"
	alertingv2beta1 "kubesphere.io/ks-upgrade/v3/api/alerting/v2beta1"
	applicationv1alpha1 "kubesphere.io/ks-upgrade/v3/api/application/v1alpha1"
	auditingv1alpha1 "kubesphere.io/ks-upgrade/v3/api/auditing/v1alpha1"
	clusterv1alpha1 "kubesphere.io/ks-upgrade/v3/api/cluster/v1alpha1"
	devopsv1alpha3 "kubesphere.io/ks-upgrade/v3/api/devops/v1alpha3"
	gatewayv1alpha1 "kubesphere.io/ks-upgrade/v3/api/gateway/v1alpha1"
	iamv1alpha2 "kubesphere.io/ks-upgrade/v3/api/iam/v1alpha2"
	eventsv1alpha1 "kubesphere.io/ks-upgrade/v3/api/kube-events/v1alpha1"
	calicov3 "kubesphere.io/ks-upgrade/v3/api/network/calicov3"
	networkv1alpha1 "kubesphere.io/ks-upgrade/v3/api/network/v1alpha1"
	notificationv2beta1 "kubesphere.io/ks-upgrade/v3/api/notification/v2beta1"
	quotav1alpha2 "kubesphere.io/ks-upgrade/v3/api/quota/v1alpha2"
	servicemeshv1alpha2 "kubesphere.io/ks-upgrade/v3/api/servicemesh/v1alpha2"
	storagev1alpha1 "kubesphere.io/ks-upgrade/v3/api/storage/v1alpha1"
	tenantv1alpha1 "kubesphere.io/ks-upgrade/v3/api/tenant/v1alpha1"
	tenantv1alpha2 "kubesphere.io/ks-upgrade/v3/api/tenant/v1alpha2"
	typesv1beta1 "kubesphere.io/ks-upgrade/v3/api/types/v1beta1"
	typesv1beta2 "kubesphere.io/ks-upgrade/v3/api/types/v1beta2"
)

func AddToScheme(scheme *runtime.Scheme) error {
	var v3SchemeBuilder = runtime.SchemeBuilder{
		apiextensions.AddToScheme,
		clientgoscheme.AddToScheme,
		apiext.AddToScheme,
		apiregistrationv1.AddToScheme,
		alertingv2beta1.AddToScheme,
		applicationv1alpha1.AddToScheme,
		auditingv1alpha1.AddToScheme,
		clusterv1alpha1.AddToScheme,
		devopsv1alpha3.AddToScheme,
		gatewayv1alpha1.AddToScheme,
		iamv1alpha2.AddToScheme,
		networkv1alpha1.AddToScheme,
		calicov3.AddToScheme,
		notificationv2beta1.AddToScheme,
		notificationv2beta2.AddToScheme,
		quotav1alpha2.AddToScheme,
		servicemeshv1alpha2.AddToScheme,
		storagev1alpha1.AddToScheme,
		tenantv1alpha1.AddToScheme,
		tenantv1alpha2.AddToScheme,
		typesv1beta1.AddToScheme,
		typesv1beta2.AddToScheme,
		monitoringv1.AddToScheme,
		loggingv1alpha2.AddToScheme,
		eventsv1alpha1.AddToScheme,
		calicov3.AddToScheme,
	}
	return v3SchemeBuilder.AddToScheme(scheme)
}
