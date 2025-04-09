## 事件升级配置

用户升级时需要自定义配置，如多集群环境下需要修改 opensearch 的配置，其他配置请参考事件扩展组件的 values.yaml。
1. 如果所有member集群的配置相同，可以修改config的配置。例如指定日志输出的 opensearch 地址。
```yaml
  whizard-events:
    enabled: true
    priority: 100
    extensionRef:
      config: |
        kube-events-exporter:
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
2. 如果每个集群的配置不同，需要单独定义。
```yaml
  whizard-events:
    enabled: true
    priority: 100
    extensionRef:
      clusterScheduling:
        overrides:
          host: |
            kube-events-exporter:
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
            kube-events-exporter:
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