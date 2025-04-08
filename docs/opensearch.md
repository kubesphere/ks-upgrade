## opensearch 升级配置

opensearch 升级, 目前内置 opensearch 只对 host 集群进行升级处理。对于 opensearch-curator 与 opensearch-dashboard 的开启与关闭，如果用户不对 config 进行配置，ks-upgrade 会自动集成升级前的配置，如果用户自定义配置了相关字段，那么会以用户配置的为准。
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