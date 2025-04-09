package jobs

import (
	_ "kubesphere.io/ks-upgrade/pkg/jobs/alerting"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/application"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/core"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/devops"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/events"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/gateway"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/iam"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/kubeedge"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/kubefed"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/logging"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/metricsserver"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/monitoring"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/network"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/notification"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/opensearch"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/servicemesh"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/storageutils"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/tower"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/vector"
	_ "kubesphere.io/ks-upgrade/pkg/jobs/whizard_telemetry"
)
