package executor

import (
	"encoding/json"
	"k8s.io/apimachinery/pkg/util/yaml"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func readConfig(file string) (map[string]interface{}, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	conf := make(map[string]interface{})
	if err = yaml.NewYAMLOrJSONDecoder(f, 1024).Decode(&conf); err != nil {
		return nil, err
	}
	return conf, nil
}

func writeToFile(config interface{}, file string) error {
	originData, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	_, err = f.Write(originData)
	if err != nil {
		return err
	}
	return nil
}

func TestConfigMerge(t *testing.T) {
	origin, err := readConfig("../../bin/config-merge.yaml")
	assert.Equal(t, err, nil)
	err = writeToFile(origin, "../../bin/origin.json")
	assert.Equal(t, err, nil)

	override, err := readConfig("../../bin/config.yaml")
	assert.Equal(t, err, nil)
	err = writeToFile(override, "../../bin/override.json")
	assert.Equal(t, err, nil)

	merge := Merge(origin, override)
	err = writeToFile(merge, "../../bin/merge.json")
	assert.Equal(t, err, nil)
}

func TestLoadFromFiles(t *testing.T) {
	defaultConfig := `
jobs:
  application:
    enabled: false
    extensionRef:
      name: openpitrix
      version: 2.0.1
    priority: 100
  core:
    enabled: false
    priority: 10000
  devops:
    dynamicOptions: {}
    enabled: false
    extensionRef:
      clusterScheduling: null
      config: |
        agent:
          jenkins:
            kubeconfigEnabled: true
            securityRealm:
              openIdConnect:
                # The kubesphere-core api used for jenkins OIDC
                # If want to access to jenkinsWebUI, the kubesphereCoreApi must be specified and browser-accessible
                # Modifying this configuration will take effect only during installation
                # If you wish for changes to take effect after installation, you need to update the jenkins-casc-config ConfigMap，copy the securityRealm configuration from jenkins.yaml to jenkins_user.yaml, save, and wait for approximately 70 seconds for the changes to take effect.
                kubesphereCoreApi: "http://ks-apiserver:30880"
                # The jenkins web URL used for OIDC redirect
                # Modifying this configuration will take effect only during installation
                # If you wish for changes to take effect after installation, you need to update the devops-jenkins-oauthclient Secret
                jenkinsURL: "http://devops-jenkins:30180"
      version: 1.1.1
    priority: 800
  gateway:
    enabled: false
    extensionRef:
      name: gateway
      version: 1.0.2
    priority: 90
  iam:
    enabled: false
    priority: 999
  ks-autoscaling:
    dynamicOptions:
      rerun: false
      updateCrds: true
    enabled: false
    extensionRef:
      name: ks-autoscaling
      version: 1.0.0
    priority: 80
  kubeedge:
    dynamicOptions:
      rerun: "false"
    enabled: false
    extensionRef:
      name: kubeedge
      version: 1.13.1
    priority: 100
  kubefed:
    enabled: false
    extensionRef:
      name: kubefed
      version: 1.0.0
    priority: 100
  metrics-server:
    dynamicOptions:
      rerun: "false"
    enabled: false
    extensionRef:
      name: metrics-server
      version: 0.7.0
    priority: 80
  network:
    dynamicOptions:
      rerun: "false"
    enabled: false
    extensionRef:
      name: network
      version: 1.1.0
    priority: 100
  opensearch:
    enabled: false
    extensionRef:
      name: opensearch
      version: 2.11.1
    priority: 100
  servicemesh:
    enabled: false
    extensionRef:
      name: servicemesh
      version: 1.0.0
    priority: 90
  storage-utils:
    dynamicOptions:
      rerun: "false"
    enabled: false
    extensionRef:
      clusterScheduling: null
      config: ""
      name: storage-utils
      version: 1.0.0
    priority: 100
  tower:
    enabled: false
    extensionRef:
      config: |
        tower:
          service:
            create: false
      name: tower
      version: 1.0.0
    priority: 100
  vector:
    enabled: false
    extensionRef:
      name: vector
      version: 1.0.4
    priority: 101
  whizard-alerting:
    dynamicOptions:
      rerun: false
      updateCrds: true
    enabled: false
    extensionRef:
      name: whizard-alerting
      version: 1.0.3
    priority: 10
  whizard-auditing:
    enabled: false
    extensionRef:
      config: |
        kube-auditing:
          sinks:
            opensearch:
              # Create opensearch sink or not
              enabled: false
              # The index to store the logs, will be {{ prefix }}-{{ timestring }}
              index:
                # The prefix of index, supports template syntax.
                prefix: "{{ .cluster }}-auditing"
                # Timestring is parsed from strftime patterns, like %Y.%m.%d. Used to distribute logs into different indexes according to time.
                timestring: "%Y.%m.%d"
              # Configurations for the opensearch sink, more info for https://vector.dev/docs/reference/configuration/sinks/elasticsearch/
              # Usually users needn't change the following OpenSearch sink config, and the default sinks in secret "kubesphere-logging-system/vector-sinks" created by the WhizardTelemetry Data Pipeline extension will be used.
              metadata:
                api_version: v8
                auth:
                  strategy: basic
                  user: admin
                  password: admin
                batch:
                  timeout_secs: 5
                buffer:
                  max_events: 10000
                endpoints:
                  - https://opensearch-cluster-data.kubesphere-logging-system.svc:9200
                tls:
                  verify_certificate: false
      name: whizard-auditing
      version: 1.2.0
    priority: 100
  whizard-events:
    enabled: false
    extensionRef:
      config: |
        kube-events-exporter:
          sinks:
            opensearch:
              # Create opensearch sink or not
              enabled: false
              # The index to store the logs, will be {{ prefix }}-{{ timestring }}
              index:
                # The prefix of index, supports template syntax.
                prefix: "{{ .cluster }}-events"
                # Timestring is parsed from strftime patterns, like %Y.%m.%d. Used to distribute logs into different indexes according to time.
                timestring: "%Y.%m.%d"
              # Configurations for the opensearch sink, more info for https://vector.dev/docs/reference/configuration/sinks/elasticsearch/
              # Usually users needn't change the following OpenSearch sink config, and the default sinks in secret "kubesphere-logging-system/vector-sinks" created by the WhizardTelemetry Data Pipeline extension will be used.
              metadata:
                api_version: v8
                auth:
                  strategy: basic
                  user: admin
                  password: admin
                batch:
                  timeout_secs: 5
                buffer:
                  max_events: 10000
                endpoints:
                  - https://opensearch-cluster-data.kubesphere-logging-system.svc:9200
                tls:
                  verify_certificate: false
      name: whizard-events
      version: 1.2.0
    priority: 100
  whizard-logging:
    enabled: false
    extensionRef:
      config: "vector-logging:        \n  sinks:\n    opensearch:\n      # Create
        opensearch sink or not\n      enabled: false\n      # The index to store the
        logs, will be {{ prefix }}-{{ timestring }}\n      index:\n        # The prefix
        of index, supports template syntax.\n        prefix: \"{{ .cluster }}-logs\"\n
        \       # Timestring is parsed from strftime patterns, like %Y.%m.%d. Used
        to distribute logs into different indexes according to time.\n        timestring:
        \"%Y.%m.%d\"\n      # Configurations for the opensearch sink, more info for
        https://vector.dev/docs/reference/configuration/sinks/elasticsearch/\n      #
        Usually users needn't change the following OpenSearch sink config, and the
        default sinks in secret \"kubesphere-logging-system/vector-sinks\" created
        by the WhizardTelemetry Data Pipeline extension will be used.\n      metadata:\n
        \       api_version: v8\n        auth:\n          strategy: basic\n          user:
        admin\n          password: admin\n        batch:\n          timeout_secs:
        5\n        buffer:\n          max_events: 10000\n        endpoints:\n          -
        https://opensearch-cluster-data.kubesphere-logging-system.svc:9200\n        tls:\n
        \         verify_certificate: false\n"
      name: whizard-logging
      version: 1.2.2
    priority: 100
  whizard-monitoring:
    dynamicOptions:
      rerun: false
      updateCrds: true
    enabled: false
    extensionRef:
      name: whizard-monitoring
      version: 1.1.0
    priority: 100
  whizard-notification:
    enabled: false
    extensionRef:
      config: |
        notification-history:
          enabled: true
          vectorNamespace: kubesphere-logging-system

          sinks:
            opensearch:
              # Create opensearch sink or not
              enabled: false
              # The index to store the logs, will be {{ prefix }}-{{ timestring }}
              index:
                # The prefix of index, supports template syntax".
                prefix: "{{ .cluster }}-notification-history"
                # Timestring is parsed from strftime patterns, like %Y.%m.%d. Used to distribute logs into different indexes according to time.
                timestring: "%Y.%m.%d"
              # Configurations for the opensearch sink, more info for https://vector.dev/docs/reference/configuration/sinks/elasticsearch/
              # Usually users needn't change the following OpenSearch sink config, and the default sinks in secret "kubesphere-logging-system/vector-sinks" created by the WhizardTelemetry Data Pipeline extension will be used.
              metadata:
                api_version: v8
                auth:
                  strategy: basic
                  user: admin
                  password: admin
                batch:
                  timeout_secs: 5
                buffer:
                  max_events: 10000
                endpoints:
                  - https://opensearch-cluster-data.kubesphere-logging-system.svc:9200
                tls:
                  verify_certificate: false
      name: whizard-notification
      version: 2.5.9
    priority: 100
  whizard-telemetry:
    enabled: false
    extensionRef:
      name: whizard-telemetry
      version: 1.2.2
    priority: 100
  whizard-telemetry-ruler:
    enabled: false
    extensionRef:
      name: whizard-telemetry-ruler
      version: 1.2.0
    priority: 100
storage:
  local:
    path: /tmp/ks-upgrade
validator:
  extensionsMuseum:
    enabled: true
    name: extensions-museum
    namespace: kubesphere-system
    syncInterval: 0
    watchTimeout: 30m
  ksVersion:
    enabled: true`

	customConfig := `storage:
  local:
    path: /ks-upgrade
jobs:
  devops:
    extensionRef:
      name: devops
      version: 1.0.0
    enabled: true`

	config := &Config{}

	if err := os.WriteFile("default-config.yaml", []byte(defaultConfig), 0644); err != nil {
		t.Errorf("failed to write config.yaml: %v", err)
	}
	if err := os.WriteFile("custom-config.yaml", []byte(customConfig), 0644); err != nil {
		t.Errorf("failed to write config.yaml: %v", err)
	}

	if err := config.LoadFromFile([]string{"default-config.yaml", "custom-config.yaml"}); err != nil {
		t.Errorf("failed to load config.yaml: %v", err)
	}

	assert.Equal(t, "devops", config.JobOptions["devops"].ExtensionRef.Name)
	assert.Equal(t, "1.0.0", config.JobOptions["devops"].ExtensionRef.Version)
	assert.Equal(t, true, config.JobOptions["devops"].Enabled)
	assert.Equal(t, "/ks-upgrade", config.StorageOptions.FileStorageOptions.StoragePath)
}
