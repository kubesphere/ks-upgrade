package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Phase represents the upgrade phase
type Phase string

const (
	PhasePrepare  Phase = "prepare"  // Prepare upgrade phase
	PhasePre      Phase = "pre"      // Pre-upgrade phase
	PhasePost     Phase = "post"     // Post-upgrade phase
	PhaseDynamic  Phase = "dynamic"  // Dynamic upgrade phase
	PhaseRollback Phase = "rollback" // Rollback phase
)

// Validator represents a validation function with its execution phases
type Validator struct {
	Func   validatorFunc
	Phases []Phase
}

type validatorFunc func(ctx context.Context, e *Executor) (bool, error)

// Only runs under version 3.x
func validateKsVersion(ctx context.Context, e *Executor) (bool, error) {
	config := e.config.Validator.KsVersion
	if !config.Enabled {
		klog.Info("[Validator] ks-version validator is disabled")
		return true, nil
	}

	r, err := e.helmClient.LastReleaseDeployed("kubesphere-system", "ks-core")
	if err != nil {
		if errors.Is(err, driver.ErrNoDeployedReleases) {
			klog.Info("[Validator] Skip upgrade beaseuse of no deployed release history.")
			return true, nil
		}
		if errors.Is(err, driver.ErrReleaseNotFound) {
			klog.Info("[Validator] Skip upgrade beaseuse of no release history.")
			return true, nil
		}
		return false, err
	}
	if strings.HasPrefix(r.Chart.Metadata.AppVersion, "v3.") {
		klog.Infof("[Validator] Current release's version is %s", r.Chart.Metadata.AppVersion)
		return true, nil
	}
	klog.Infof("[Validator] Skip upgrade because of release's version is %s", r.Chart.Metadata.AppVersion)
	return false, nil
}

// validateExtensionsMuseum checks if the extensions-museum repository has been synced
func validateExtensionsMuseum(ctx context.Context, e *Executor) (bool, error) {
	config := e.config.Validator.ExtensionsMuseum
	if !config.Enabled {
		klog.Info("[Validator] extensions-museum validator is disabled")
		return true, nil
	}

	repo := &v1alpha1.Repository{}
	err := e.clientV4.Get(ctx, runtimeclient.ObjectKey{
		Name:      config.Name,
		Namespace: config.Namespace,
	}, repo)
	if err != nil {
		klog.Errorf("[Validator] Failed to get extensions-museum repository: %v", err)
		return false, err
	}

	// If syncInterval is 0, only check if LastSyncTime exists
	if config.SyncInterval == 0 {
		if repo.Status.LastSyncTime == nil {
			klog.Info("[Validator] extensions-museum repository has not been synced yet, waiting for sync...")
			return waitForRepositorySync(ctx, e, config)
		}
		klog.Info("[Validator] extensions-museum repository is synced (no time interval check)")
		return true, nil
	}

	// Check if the last sync was within the configured interval
	if repo.Status.LastSyncTime == nil || time.Since(repo.Status.LastSyncTime.Time) > config.SyncInterval {
		klog.Infof("[Validator] extensions-museum repository last sync is too old or not synced yet (threshold: %v), waiting for sync...", config.SyncInterval)
		return waitForRepositorySync(ctx, e, config)
	}

	klog.Info("[Validator] extensions-museum repository is synced")
	return true, nil
}

// waitForRepositorySync waits for the repository to be synced using watch
func waitForRepositorySync(ctx context.Context, e *Executor, config ExtensionsMuseumConfig) (bool, error) {
	repo := &v1alpha1.Repository{}
	watchCtx := ctx
	if config.WatchTimeout > 0 {
		var cancel context.CancelFunc
		watchCtx, cancel = context.WithTimeout(ctx, config.WatchTimeout)
		defer cancel()
	}

	watch, err := e.watchClient.Watch(watchCtx, &v1alpha1.RepositoryList{}, &runtimeclient.ListOptions{
		Namespace:     config.Namespace,
		FieldSelector: fields.OneTermEqualSelector("metadata.name", config.Name),
	})
	if err != nil {
		return false, fmt.Errorf("failed to watch repository: %v", err)
	}
	defer watch.Stop()

	for {
		select {
		case <-watchCtx.Done():
			if watchCtx.Err() == context.DeadlineExceeded {
				return false, fmt.Errorf("watch timeout after %v waiting for repository sync", config.WatchTimeout)
			}
			return false, watchCtx.Err()
		case event, ok := <-watch.ResultChan():
			if !ok {
				return false, fmt.Errorf("watch channel closed")
			}

			if repoObj, ok := event.Object.(*v1alpha1.Repository); ok {
				repo = repoObj
				if repo.Status.LastSyncTime == nil {
					klog.V(4).Infof("[Validator] Repository %s/%s not synced yet, waiting...", config.Namespace, config.Name)
					continue
				}

				if config.SyncInterval == 0 {
					klog.Info("[Validator] extensions-museum repository is now synced")
					return true, nil
				}

				if time.Since(repo.Status.LastSyncTime.Time) <= config.SyncInterval {
					klog.Info("[Validator] extensions-museum repository is now synced within interval")
					return true, nil
				}
				klog.V(4).Infof("[Validator] Repository %s/%s last sync time %v is outside interval %v, waiting...",
					config.Namespace, config.Name, repo.Status.LastSyncTime.Time, config.SyncInterval)
			}
		}
	}
}

// NewValidator creates a new validator with specified phases
func NewValidator(f validatorFunc, phases ...Phase) Validator {
	return Validator{
		Func:   f,
		Phases: phases,
	}
}
