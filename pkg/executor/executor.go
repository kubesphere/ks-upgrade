package executor

import (
	"context"
	"sort"

	"github.com/pkg/errors"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	restconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	"kubesphere.io/ks-upgrade/pkg/download"
	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	schemev3 "kubesphere.io/ks-upgrade/v3/scheme"
	schemev4 "kubesphere.io/ks-upgrade/v4/scheme"
)

// JobOptions job options
type JobOptions struct {
	DynamicOptions DynamicOptions     `json:"dynamicOptions"`
	ExtensionRef   model.ExtensionRef `json:"extensionRef"`
	Enabled        bool               `json:"enabled"`
	Priority       int                `json:"priority"`
}

type DynamicOptions map[string]interface{}

type Executor struct {
	sortedJobs    []NamedUpgradeJob
	clientV3      runtimeclient.Client
	clientV4      runtimeclient.Client
	watchClient   runtimeclient.WithWatch
	resourceStore store.ResourceStore
	helmClient    helm.HelmClient
	validators    []Validator
	config        *Config
}

type NamedUpgradeJob struct {
	Name     string
	Priority int
	UpgradeJob
}

func (n *NamedUpgradeJob) IsEnabled(ctx context.Context) (bool, error) {
	if condition, ok := n.UpgradeJob.(EnabledCondition); ok {
		enabled, err := condition.IsEnabled(ctx)
		if err != nil {
			return false, err
		}
		klog.Infof("[Job] Detected that the plugin %s is %v", n.Name, enabled)
		return enabled, nil
	}
	klog.Infof("[Job] Detected that the plugin %s does not implement the enabled condition interface", n.Name)
	return true, nil
}

var jobFactories = make(map[string]JobFactory)

func NewExecutor(config *Config) (*Executor, error) {
	restConfig, err := restconfig.GetConfig()
	if err != nil {
		return nil, errors.Errorf("failed to get rest config: %s", err)
	}

	v3Schema := runtime.NewScheme()
	if err = schemev3.AddToScheme(v3Schema); err != nil {
		return nil, errors.Errorf("failed to register scheme v3: %s", err)
	}
	runtimeClientV3, err := runtimeclient.New(restConfig, runtimeclient.Options{
		Scheme: v3Schema,
	})

	if err != nil {
		return nil, errors.Errorf("failed to create runtime client v3: %s", err)
	}

	v4Schema := runtime.NewScheme()
	if err = schemev4.AddToScheme(v4Schema); err != nil {
		return nil, errors.Errorf("failed to register scheme v4: %s", err)
	}

	runtimeClientV4, err := runtimeclient.New(restConfig, runtimeclient.Options{
		Scheme: v4Schema,
	})
	if err != nil {
		return nil, errors.Errorf("failed to create runtime client v3: %s", err)
	}

	watchClient, err := runtimeclient.NewWithWatch(restConfig, runtimeclient.Options{
		Scheme: v4Schema,
	})
	if err != nil {
		return nil, errors.Errorf("failed to create watch client: %s", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, errors.Errorf("failed to create dynamic client: %s", err)
	}

	chartDownloader, err := download.NewChartDownloader()
	if err != nil {
		return nil, errors.Errorf("failed to create chartDownloader client: %s", err)
	}

	helperFactory := helper.NewModelHelperFactory(runtimeClientV3, runtimeClientV4, dynamicClient, chartDownloader)

	storageClient, err := storage.NewStorage(config.StorageOptions)
	if err != nil {
		return nil, errors.Errorf("failed to create storage client: %s", err)
	}

	resourceStore := store.NewKubernetesResourceStore(storageClient)

	helmClient, err := helm.NewClient("")
	if err != nil {
		return nil, errors.Errorf("failed to create helm client: %s", err)
	}

	sortedJobs := make([]NamedUpgradeJob, 0)
	for jobName, factory := range jobFactories {
		jobOptions, ok := config.JobOptions[jobName]
		if !ok { // job not found in config should be ignored
			klog.Infof("[Job] %s is disabled", jobName)
			continue
		}
		if jobOptions.Enabled {
			job, err := factory.Create(jobOptions.DynamicOptions, &jobOptions.ExtensionRef)
			if err != nil {
				return nil, errors.Errorf("failed to create upgrade job %s: %s", jobName, err)
			}
			if injector, ok := job.(InjectClientV3); ok {
				injector.InjectClientV3(runtimeClientV3)
			}
			if injector, ok := job.(InjectClientV4); ok {
				injector.InjectClientV4(runtimeClientV4)
			}
			if injector, ok := job.(InjectModelHelperFactory); ok {
				injector.InjectModelHelperFactory(helperFactory)
			}
			if injector, ok := job.(InjectHelmClient); ok {
				injector.InjectHelmClient(helmClient)
			}
			if injector, ok := job.(InjectResourceStore); ok {
				injector.InjectResourceStore(resourceStore)
			}
			klog.Infof("[Job] %s is enabled, priority %d", jobName, jobOptions.Priority)
			sortedJobs = append(sortedJobs, NamedUpgradeJob{Name: jobName, UpgradeJob: job, Priority: jobOptions.Priority})
		} else {
			klog.Infof("[Job] %s is disabled", jobName)
		}
	}

	sort.SliceStable(sortedJobs, func(i, j int) bool {
		job1 := sortedJobs[i]
		job2 := sortedJobs[j]
		if job1.Priority == job2.Priority {
			return job1.Name < job2.Name
		}
		return job1.Priority > job2.Priority
	})

	// Support validator extension
	validators := make([]Validator, 0, 2)
	if config.Validator.KsVersion.Enabled {
		validators = append(validators, NewValidator(validateKsVersion, PhasePrepare, PhasePre, PhasePost))
	}
	if config.Validator.ExtensionsMuseum.Enabled {
		validators = append(validators, NewValidator(validateExtensionsMuseum, PhasePost))
	}
	return &Executor{
		clientV3:      runtimeClientV3,
		clientV4:      runtimeClientV4,
		watchClient:   watchClient,
		sortedJobs:    sortedJobs,
		resourceStore: resourceStore,
		helmClient:    helmClient,
		validators:    validators,
		config:        config,
	}, nil
}

func (e *Executor) runValidators(ctx context.Context, phase Phase) (bool, error) {
	for _, validator := range e.validators {
		// Check if validator should run in this phase
		shouldRun := false
		for _, p := range validator.Phases {
			if p == phase {
				shouldRun = true
				break
			}
		}
		if !shouldRun {
			continue
		}

		if valid, err := validator.Func(ctx, e); !valid {
			return false, err
		}
	}
	return true, nil
}

func (e *Executor) PrepareUpgrade(ctx context.Context) error {
	if ok, err := e.runValidators(ctx, PhasePrepare); err != nil {
		return err
	} else if !ok {
		klog.Info("[Validator] Prepare upgrade skipping because of failed validators")
		return nil
	}
	for _, job := range e.sortedJobs {
		klog.Infof("[Job] %s prepare-upgrade start", job.Name)
		if ok, err := job.IsEnabled(ctx); err != nil {
			return err
		} else if !ok {
			continue
		}
		if prepareUpgrade, ok := job.UpgradeJob.(PrepareUpgrade); ok {
			if err := prepareUpgrade.PrepareUpgrade(ctx); err != nil {
				return err
			}
		}
		klog.Infof("[Job] %s prepare-upgrade finished", job.Name)
	}
	return nil
}

func (e *Executor) PreUpgrade(ctx context.Context) error {
	if ok, err := e.runValidators(ctx, PhasePre); err != nil {
		return err
	} else if !ok {
		klog.Info("[Validator] Pre-upgrade skipping because of failed validators")
		return nil
	}
	for _, job := range e.sortedJobs {
		klog.Infof("[Job] %s pre-upgrade start", job.Name)
		if ok, err := job.IsEnabled(ctx); err != nil {
			return err
		} else if !ok {
			klog.Infof("[Job] %s pre-upgrade skipping. Detected that the plugin is not enabled", job.Name)
			continue
		}
		if err := job.PreUpgrade(ctx); err != nil {
			klog.Errorf("[Job] %s pre-upgrade failed. If you need to forcibly continue the upgrade, please disable the %s upgrade module with command `helm upgrade -n kubesphere-system ks-core $chart --set=upgrade.config.jobs.%s.enabled=false --debug --wait`.", job.Name, job.Name, job.Name)
			return err
		}
		klog.Infof("[Job] %s pre-upgrade finished", job.Name)
	}
	return nil
}

func (e *Executor) PostUpgrade(ctx context.Context) error {
	if ok, err := e.runValidators(ctx, PhasePost); err != nil {
		return err
	} else if !ok {
		klog.Info("[Validator] Post-upgrade skipping because of failed validators")
		return nil
	}
	for _, job := range e.sortedJobs {
		klog.Infof("[Job] %s post-upgrade start", job.Name)
		if ok, err := job.IsEnabled(ctx); err != nil {
			return err
		} else if !ok {
			klog.Infof("[Job] %s post-upgrade skipping. Detected that the plugin is not enabled", job.Name)
			continue
		}
		if err := job.PostUpgrade(ctx); err != nil {
			klog.Errorf("[Job] %s post-upgrade failed. If you need to forcibly continue the upgrade, please disable the %s upgrade module with command `helm upgrade -n kubesphere-system ks-core $chart --set=upgrade.config.jobs.%s.enabled=false --debug --wait`.", job.Name, job.Name, job.Name)
			return err
		}
		klog.Infof("[Job] %s post-upgrade finished", job.Name)
	}
	return nil
}

func (e *Executor) DynamicUpgrade(ctx context.Context) error {
	if ok, err := e.runValidators(ctx, PhaseDynamic); err != nil {
		return err
	} else if !ok {
		klog.Info("[Validator] Dynamic upgrade skipping because of failed validators")
		return nil
	}
	for _, job := range e.sortedJobs {
		klog.Infof("[Job] %s dynamic-upgrade start", job.Name)
		if dynamicUpgrade, ok := job.UpgradeJob.(DynamicUpgrade); ok {
			if err := dynamicUpgrade.DynamicUpgrade(ctx); err != nil {
				return err
			}
		}
		klog.Infof("[Job] %s dynamic-upgrade finished", job.Name)
	}
	return nil
}

// RollBack
// todo: implement to support rollback when upgrade failed
func (e *Executor) RollBack(ctx context.Context) error {
	if ok, err := e.runValidators(ctx, PhaseRollback); err != nil {
		return err
	} else if !ok {
		klog.Info("[Validator] Rollback skipping because of failed validators")
		return nil
	}
	for _, job := range e.sortedJobs {
		klog.Infof("[Job] %s rollback start", job.Name)
		if rollBack, ok := job.UpgradeJob.(RollBack); ok {
			if err := rollBack.RollBack(ctx); err != nil {
				return err
			}
		}
		klog.Infof("[Job] %s rollback finished", job.Name)
	}
	return nil
}

func Register(factory JobFactory) error {
	if jobFactories[factory.Name()] != nil {
		return errors.Errorf("job factory is already registered: %s", factory.Name())
	}
	jobFactories[factory.Name()] = factory
	return nil
}
