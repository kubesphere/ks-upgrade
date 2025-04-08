## Post-upgrade Configuration for WhizardTelemetry Alerting

### Observability Center Not Enabled

> This applies only to scenarios where the Observability Center is not enabled.

After upgrading the host cluster, configure the __WhizardTelemetry Alerting__ extension as follows:

```yaml
agent:
  ruler:
    alertmanagersUrl:
    - 'http://<host>:<port>'
```

Set the above `agent.ruler.alertmanagersUrl` to the alertmanager-proxy service of the WhizardTelemetry Notification extension, which is deployed only in the host cluster and exposed by default as a NodePort (default 31093).


