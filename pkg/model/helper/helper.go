package helper

import (
	"k8s.io/client-go/dynamic"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"kubesphere.io/ks-upgrade/pkg/download"
	"kubesphere.io/ks-upgrade/pkg/model/core"
)

// ModelHelperFactory extension helper factory
type ModelHelperFactory interface {
	CoreHelper() core.Helper
}

type helper struct {
	coreHelper core.Helper
}

func (h *helper) CoreHelper() core.Helper {
	return h.coreHelper
}

func NewModelHelperFactory(clientV3 runtimeclient.Client, clientV4 runtimeclient.Client, dynamicClient *dynamic.DynamicClient, chartDownloader *download.ChartDownloader) ModelHelperFactory {
	return &helper{
		coreHelper: core.NewCoreHelper(clientV3, clientV4, dynamicClient, chartDownloader),
	}
}
