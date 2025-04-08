## Gateway Upgrade

To minimize the impact of potential issues during the upgrade process, after upgrading the gateway extension, each gateway needs to be upgraded individually. Follow these steps to upgrade the gateway:

### 1. Upgrade the Gateway Extension

Enable the gateway upgrade in the `ks-upgrade` configuration.

> This step will back up gateway-related data, create an InstallPlan, clean up Gateway v1 CRD, and perform other operations.

### 2. Schedule the Agent

This step is not required for the host cluster upgrade. After the member cluster is successfully upgraded, execute the following command on the host cluster to remove the taint from the member cluster and schedule the extension agent to the member cluster.

```sh
kubectl get clusters.cluster.kubesphere.io {member-x} -o json | jq 'del(.status.conditions[] | select(.type=="Schedulable"))' | kubectl apply -f -
```

### 3. Upgrade Individual Gateways

After the upgrade of the gateway extension is complete, upgrade each gateway individually.

**Steps**

Get the `upgrade.sh` script and execute `bash upgrade.sh gateway <gateway_name>`. Please specify the gateway name to upgrade.

1. If the gateway name is empty, the command will prompt for upgradable gateways and their corresponding serial numbers. Input the gateway serial number to proceed.
2. If the gateway name is "all", all gateways will be upgraded.

**Note**

This step will convert the backed-up gateway data into new version gateway data, upgrade the gateway's Helm release, and then generate a new Ingress resource corresponding to the new gateway. This Ingress resource will set the corresponding IngressClassName.

> Additionally, for gateways upgraded from KubeSphere 3.4, the name of the Gateway CR will change from `kubesphere-router-xxx` to `kubesphere-router-xxx-ingress`, adding the `-ingress` suffix. This is because the previous gateway was directly controlled by the Nginx CR, and the Nginx CR name had an `-ingress` suffix compared with the Gateway CR name. Therefore, when performing the Helm upgrade, this suffix must be added.

After the gateway upgrade, the previous Ingress will no longer be able to access its service. However, the service can be accessed through the newly generated Ingress. The new Ingress generated for different gateways are as follows:

- Cluster Gateway: Generates a new Ingress based on all Ingresses in the cluster, with the IngressClassName of new Ingress associated with this cluster gateway. Named: `{original Ingress name}-kubesphere-system`
- Workspace Gateway: Generates a new Ingress based on all Ingresses in all projects within the workspace, with the IngressClassName of new Ingress associated with this workspace gateway. Named: `{original Ingress name}-workspace-{workspaceName}`
- Project Gateway: Generates a new Ingress based on all Ingresses in the project, with the IngressClassName of new Ingress associated with this project gateway. Named: `{original Ingress name}-{projectName}`

> The previous Ingress will not be able to access its service after the upgrade because it did not specify an IngressClassName, and the new gateway will only handle Ingress configurations that has set the IngressClassName.

**Example**

In a KubeSphere 3.4 environment, on the `cluster1` cluster, there is a workspace `workspace1`, which has a project `project1`. The project, workspace, and cluster have enabled project 1, workspace 1, and cluster gateways respectively. In project `project1`, there is an Ingress named `demo`. At this time, the backend of `demo` can be accessed through the entry points corresponding to the cluster gateway, workspace 1 gateway, and project 1 gateway.

After upgrading the gateway from version 3.4 to 4.1, three new Ingresses will be generated: `demo-kubesphere-system`, `demo-workspace-workspace1`, `demo-project1`, each associated with their respective gateways.

**Check**

- Check the status of the gateway Helm release

```sh
# View all gateway Helm releases
helm ls -Aa | grep "kubesphere-router"

# If there is a pending status, roll back to the previous normal version, and the gateway-controller will re-upgrade it
# View the release history
helm history <release> --namespace <namespace>
# Roll back to the last normal state
helm rollback <release> <revision> --namespace <namespace>
```
- Check if the newly generated Ingress resources are accessible

### 5. Clean Up Resources

After all gateways have been upgraded and verified, clean up the legacy resources from version 3.5.

```sh
kubectl get namespaces --no-headers=true | awk '{print $1}' | xargs -I {} sh -c 'kubectl get nginxes.gateway.kubesphere.io -n {} -o name | xargs -I % kubectl patch % -n {} --type=json -p="[{\"op\": \"remove\", \"path\": \"/metadata/finalizers\"}]";'
kubectl delete crd nginxes.gateway.kubesphere.io
```