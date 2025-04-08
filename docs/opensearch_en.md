## Upgrade Configuration for OpenSearch 

For OpenSearch upgrade, currently, the built-in OpenSearch is only upgraded on the host cluster. Regarding the enabling and disabling of OpenSearch Curator and OpenSearch Dashboard, if you do not configure the `config`, `ks-upgrade` will automatically integrate the configuration before the upgrade. If you customize the configurations, your configuration will take precedence.

```yaml
  opensearch:
    enabled: true
    priority: 100
    extensionRef:
      config: |
        opensearch-data:
          service:
            type: NodePort
            nodePort: 30920
      installationMode: Multicluster
```