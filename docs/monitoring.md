## 监控升级后配置

### 启用可观测中心场景

1. 如果您启用了可观测中心，请手动更新 **WhizardTelemetry 平台服务** 扩展组件配置，将 `whizard-telemetry.config.observability.enabled` 设置为 `true`

    ```yaml
    whizard-telemetry:
      config:
        observability:
          enabled: true
          endpoint: "http://query-frontend-whizard-operated.kubesphere-monitoring-system.svc:10902"
    ```

2. 如果您为可观测中心配置了长期存储, 请在 host 集群执行以下命令, 将配置关联同步

    ```sh
    kubectl patch services.monitoring.whizard.io whizard -n kubesphere-monitoring-system --type merge -p '{"spec": {"storage":{"name":"remote",     "namespace":"kubesphere-monitoring-system"}}}'
    ```
