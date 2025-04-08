package executor

import (
	"errors"
	"io/fs"
	"os"
	"reflect"
	"time"

	"github.com/mitchellh/mapstructure"
	"k8s.io/apimachinery/pkg/util/yaml"
	"kubesphere.io/ks-upgrade/pkg/storage"
)

// ExtensionsMuseumConfig defines the configuration for extensions-museum validator
type ExtensionsMuseumConfig struct {
	Enabled      bool          `json:"enabled,omitempty"`
	Namespace    string        `json:"namespace,omitempty"`
	Name         string        `json:"name,omitempty"`
	SyncInterval time.Duration `json:"syncInterval,omitempty" yaml:"syncInterval,omitempty"`
	WatchTimeout time.Duration `json:"watchTimeout,omitempty" yaml:"watchTimeout,omitempty"`
}

// Validate ensures all fields have valid values, using defaults when needed
func (c *ExtensionsMuseumConfig) Validate() {
	if c.Namespace == "" {
		c.Namespace = "kubesphere-system"
	}
	if c.Name == "" {
		c.Name = "extensions-museum"
	}
	if c.SyncInterval == 0 {
		c.SyncInterval = 0
	}
	if c.WatchTimeout == 0 {
		c.WatchTimeout = 5 * time.Minute
	}
}

// KsVersionConfig defines the configuration for ks-version validator
type KsVersionConfig struct {
	Enabled bool `json:"enabled,omitempty"`
}

// Validate ensures all fields have valid values, using defaults when needed
func (c *KsVersionConfig) Validate() {}

// ValidatorConfig defines the configuration for all validators
type ValidatorConfig struct {
	ExtensionsMuseum ExtensionsMuseumConfig `json:"extensionsMuseum,omitempty" yaml:"extensionsMuseum,omitempty"`
	KsVersion        KsVersionConfig        `json:"ksVersion,omitempty" yaml:"ksVersion,omitempty"`
}

// Validate ensures all fields have valid values, using defaults when needed
func (c *ValidatorConfig) Validate() {
	c.ExtensionsMuseum.Validate()
	c.KsVersion.Validate()
}

type Config struct {
	StorageOptions *storage.Options      `json:"storage,omitempty" yaml:"storage,omitempty"`
	JobOptions     map[string]JobOptions `json:"jobs,omitempty" yaml:"jobs,omitempty"`
	Validator      *ValidatorConfig      `json:"validator,omitempty" yaml:"validator,omitempty"`
}

func LoadConfig(files []string) (*Config, error) {
	config := NewDefaultConfig()
	if err := config.LoadFromFile(files); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return config, nil
		}
		return nil, err
	}
	return config, nil
}

func NewDefaultConfig() *Config {
	return &Config{
		StorageOptions: &storage.Options{
			FileStorageOptions: &storage.LocalFileStorageOptions{StoragePath: "/tmp/ks-upgrade"},
		},
		Validator: &ValidatorConfig{},
	}
}

func (c *Config) LoadFromFile(files []string) error {
	var merged map[string]interface{}
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		var override map[string]interface{}
		if err = yaml.NewYAMLOrJSONDecoder(f, 1024).Decode(&override); err != nil {
			return err
		}
		merged = Merge(merged, override)
	}

	stringToDurationHook := func(
		f reflect.Type,
		t reflect.Type,
		data interface{}) (interface{}, error) {
		if t == reflect.TypeOf(time.Second) && f == reflect.TypeOf("") {
			return time.ParseDuration(data.(string))
		}
		return data, nil
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: stringToDurationHook,
		TagName:    "json",
		Result:     c,
	})

	if err != nil {
		return err
	}

	if err := decoder.Decode(merged); err != nil {
		return err
	}

	// If validator config is not set in the file, create a new one
	if c.Validator == nil {
		c.Validator = &ValidatorConfig{}
	}
	c.Validator.Validate()
	return nil
}

func Merge(base, override map[string]interface{}) map[string]interface{} {
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	merged := make(map[string]interface{})

	for k, v := range base {
		merged[k] = v
	}

	for k, v := range override {
		if vMap, ok := v.(map[string]interface{}); ok {
			if baseMap, ok := base[k].(map[string]interface{}); ok {
				merged[k] = Merge(baseMap, vMap)
			} else {
				merged[k] = vMap
			}
		} else {
			merged[k] = v
		}
	}

	return merged
}
