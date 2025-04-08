## 通知升级配置

通知升级,如果用户使用的 opensearch 不在 host 集群上，需要指定 opensearch endpoint 地址。其他配置请参考通知扩展组件的 values.yaml。
```yaml
  whizard-notification:
    enabled: true
    priority: 100
    extensionRef:
      config: |
        notification-history:
          enabled: true
          vectorNamespace: kubesphere-logging-system
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
                  - https://opensearch-cluster-data.kubesphere-logging-system.svc:9200
                tls:
                  verify_certificate: false
```