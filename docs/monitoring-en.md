## Post-upgrade Configuration for WhizardTelemetry Monitoring

### Observability Center Enabled

1. If you have enabled the Observability Center, manually update the configuration of **WhizardTelemetry Platform Service** by setting `whizard-telemetry.config.observability.enabled` to `true`.

    ```yaml
    whizard-telemetry:
      config:
        observability:
          enabled: true
          endpoint: "http://query-frontend-whizard-operated.kubesphere-monitoring-system.svc:10902"
    ```

2. If you have configured long-term storage for the Observability Center, execute the following command on the host cluster to synchronize the configuration.

    ```sh
    kubectl patch services.monitoring.whizard.io whizard -n kubesphere-monitoring-system --type merge -p '{"spec": {"storage":{"name":"remote", "namespace":"kubesphere-monitoring-system"}}}'
    ```