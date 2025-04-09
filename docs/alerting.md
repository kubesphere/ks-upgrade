## 告警升级后配置

### 未启用可观测中心场景

> 仅适用于未启用可观测中心的场景。

请在 host 集群升级完成后对 __WhizardTelemetry 告警管理__ 扩展组件进行如下配置:

```yaml
agent:
  ruler:
    alertmanagersUrl:
    - 'http://<host>:<port>'
```

将上述 `agent.ruler.alertmanagersUrl` 配置为 __WhizardTelemetry 通知管理__ 扩展组件的 alertmanager-proxy 服务，其仅部署在 host 集群，默认以 NodePort 形式(默认 31093)暴露。 
