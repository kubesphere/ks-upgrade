package executor

import (
	"context"

	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/store"
)

type PrepareUpgrade interface {
	PrepareUpgrade(ctx context.Context) error
}

type PreUpgrade interface {
	PreUpgrade(ctx context.Context) error
}

type PostUpgrade interface {
	PostUpgrade(ctx context.Context) error
}

type DynamicUpgrade interface {
	DynamicUpgrade(ctx context.Context) error
}

// RollBack
// todo: implement to support rollback when upgrade failed
type RollBack interface {
	RollBack(ctx context.Context) error
}

type Component interface {
	Name() string
}

type UpgradeJob interface {
	PreUpgrade
	PostUpgrade
}

type EnabledCondition interface {
	IsEnabled(ctx context.Context) (bool, error)
}

type InjectClientV3 interface {
	InjectClientV3(client runtimeclient.Client)
}

type InjectClientV4 interface {
	InjectClientV4(client runtimeclient.Client)
}

type InjectModelHelperFactory interface {
	InjectModelHelperFactory(factory helper.ModelHelperFactory)
}

type InjectHelmClient interface {
	InjectHelmClient(client helm.HelmClient)
}

type InjectResourceStore interface {
	InjectResourceStore(store store.ResourceStore)
}

type JobFactory interface {
	Component
	Create(options DynamicOptions, extensionRef *model.ExtensionRef) (UpgradeJob, error)
}
