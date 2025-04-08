package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/executor"
	_ "kubesphere.io/ks-upgrade/pkg/jobs"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	gitHash   string
	buildTime string
	goVersion string
)

func init() {
	rootCmd.PersistentFlags().StringArrayVar(&configFiles, "config", []string{}, "Config file. Supports specifying multiple configurations at the same time")
	fs := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(fs)
	rootCmd.Flags().AddGoFlagSet(fs)
	prepareUpgradeCmd.Flags().AddGoFlagSet(fs)
	preUpgradeCmd.Flags().AddGoFlagSet(fs)
	postUpgradeCmd.Flags().AddGoFlagSet(fs)
	dynamicUpgradeCmd.Flags().AddGoFlagSet(fs)
	rollbackUpgradeCmd.Flags().AddGoFlagSet(fs)
	rootCmd.AddCommand(prepareUpgradeCmd)
	rootCmd.AddCommand(preUpgradeCmd)
	rootCmd.AddCommand(postUpgradeCmd)
	rootCmd.AddCommand(dynamicUpgradeCmd)
	rootCmd.AddCommand(rollbackUpgradeCmd)
	rootCmd.AddCommand(versionCmd)
}

var configFiles []string

var rootCmd = &cobra.Command{
	Use:           "ks-upgrade",
	Long:          `Data backup and upgrade tool for KubeSphere API server`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var rollbackUpgradeCmd = &cobra.Command{
	Use:  "rollback",
	Long: `Data rollback tool for KubeSphere API server`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := newExecutor().RollBack(context.Background()); err != nil {
			return errors.Wrap(err, "failed to execute rollback jobs")
		}
		return nil
	},
	SilenceUsage: true,
}

var dynamicUpgradeCmd = &cobra.Command{
	Use:  "dynamic-upgrade",
	Long: `Data dynamic-upgrade tool for KubeSphere API server`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := newExecutor().DynamicUpgrade(context.Background()); err != nil {
			return fmt.Errorf("failed to execute dynamic-upgrade jobs: %v", err)
		}
		return nil
	},
	SilenceUsage: true,
}

var postUpgradeCmd = &cobra.Command{
	Use:  "post-upgrade",
	Long: `Data post-upgrade tool for KubeSphere API server`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := newExecutor().PostUpgrade(context.Background()); err != nil {
			return errors.Wrap(err, "failed to execute post-upgrade jobs")
		}
		return nil
	},
	SilenceUsage: true,
}

var preUpgradeCmd = &cobra.Command{
	Use:  "pre-upgrade",
	Long: `Data pre-upgrade for KubeSphere API server`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := newExecutor().PreUpgrade(context.Background()); err != nil {
			return errors.Wrap(err, "failed to execute pre-upgrade jobs")
		}
		return nil
	},
	SilenceUsage: true,
}

var prepareUpgradeCmd = &cobra.Command{
	Use:  "prepare-upgrade",
	Long: `Data prepare-upgrade for KubeSphere API server`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := newExecutor().PrepareUpgrade(context.Background()); err != nil {
			return errors.Wrap(err, "failed to execute prepare-upgrade jobs")
		}
		return nil
	},
	SilenceUsage: true,
}

var versionCmd = &cobra.Command{
	Use:  "version",
	Long: `Print the version number of ks-upgrade`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Git Commit Hash: %s \n", gitHash)
		fmt.Printf("Build TimeStamp: %s \n", buildTime)
		fmt.Printf("GoLang Version: %s \n", goVersion)
		return
	},
}

func newExecutor() *executor.Executor {
	config, err := executor.LoadConfig(configFiles)
	if err != nil {
		klog.Fatalf("failed to load config: %s", err)
	}

	e, err := executor.NewExecutor(config)
	if err != nil {
		klog.Fatalf("failed to create executor: %s", err)
	}
	return e
}

func main() {
	ctrl.SetLogger(klog.NewKlogr())
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("%+v", err)
		os.Exit(1)
	}
}
