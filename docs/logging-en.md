## Upgrade Configuration for WhizardTelemetry Logging

You need to customize the configuration before the upgrade, such as modifying the OpenSearch configuration for a multi-cluster environment. For other configurations, please refer to `values.yaml` of WhizardTelemetry Logging.

1. If the configurations of all member clusters are the same, you can modify the config. For example, specify the OpenSearch address for log output.

```yaml
  whizard-logging:
    enabled: true
    priority: 100
    extensionRef:
      config: |
        vector-logging:
          sinks:
            opensearch:
              # Create opensearch sink or not
              enabled: true
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
                  - https://172.31.73.147:30920
                tls:
                  verify_certificate: false
      clusterScheduling:
        placement:
          clusters:
            - host
            - member
```
2. If the configurations of each cluster are different, they need to be defined separately.

```yaml
  whizard-logging:
    enabled: true
    priority: 100
    extensionRef:
      clusterScheduling:
        overrides:
          host: |
            vector-logging:
              sinks:
                opensearch:
                  # Create opensearch sink or not
                  enabled: true
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
                      - https://172.31.73.147:30920
                    tls:
                      verify_certificate: false
          member: |
            vector-logging:
              sinks:
                opensearch:
                  # Create opensearch sink or not
                  enabled: true
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
                      - https://172.31.73.147:30920
                    tls:
                      verify_certificate: false

        placement:
          clusters:
            - host
            - member
```