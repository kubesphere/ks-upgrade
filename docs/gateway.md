## 网关升级
为了降低升级过程出现异常时的影响范围，网关升级时升级了网关扩展组件后还需要对网关逐个进行升级。
网关升级需要按如下步骤进行：

### 1. 升级网关扩展组件
在 ks-upgrade 的升级配置中开启网关组件升级。
>这个步骤会备份网关相关数据，创建 InstallPlan，清理 Gateway v1 CRD 等操作。

### 2. 调度 agent
host 集群升级成功后不用做这个操作，member 集群升级成功后，需要在 host 集群上执行以下命令，移除 member 集群的污点，使扩展组件 agent 调度到 member 集群上。
```sh
kubectl get clusters.cluster.kubesphere.io {member-x} -o json | jq 'del(.status.conditions[] | select(.type=="Schedulable"))' | kubectl apply -f -
```

### 3. 升级单个网关
网关组件升级完成后，逐个升级各个网关。
**操作步骤**
根据获取到的 upgrade.sh 脚本执行：
`bash upgrade.sh  gateway <gateway_name>`：指定网关名称升级。
1. 若网关名称为空则会提示可升级的网关和对应序号，交互时输入网关序号进行选择
2. 若网关名称为 all，即升级所有网关

**说明**
这个步骤会根据备份的网关数据转化成新版本的网关数据，然后升级网关的 helm release；再生成新版网关对应的 Ingress 资源，这个 Ingress 资源会设置对应的 IngressClassName。

>另外由 3.4 版本升级的网关，Gateway CR 的名称会由之前的 `kubesphere-router-xxx` 变成 `kubesphere-router-xxx-ingress`，会多个 `-ingress` 后缀。这是因为之前网关是由 Nginx CR 直接控制的，Nginx CR 的命名相比 Gateway CR 会多个 `-ingress` 后缀，所以在此基础上进行 helm upgrade 也就只能加上此后缀。

网关升级后之前的 Ingress 将不能访问到，可以通过新生成的 Ingress 访问。根据不同网关新生成的 Ingress 如下：
- 集群网关：根据该集群内所有的 Ingress 来生成新的 Ingress，新的 Ingress 设置的 IngressClassName 关联到此集群网关。命名为：`{原始 Ingress 名字}-kubesphere-system`
- 企业空间网关：根据该企业空间内所有的项目下的所有 Ingress 来生成新的 Ingress，新的 Ingress 设置的 IngressClassName 关联到此企业空间网关。命名为：`{原始 Ingress 名字}-workspace-{workspaceName}`
- 项目网关：根据该项目下的所有 Ingress 来生成新的 Ingress，新的 Ingress 设置的 IngressClassName 关联到此项目网关。命名为：`{原始 Ingress 名字}-{projectName}`

>升级后之前的 Ingress 将不能访问是因为之前的 Ingress 没有指定 IngressClassName，新版的网关只会处理有设置 IngressClassName 的 Ingress 配置。

**例如**
在 3.4 环境中，cluster1 集群上有企业空间 workspace1，该企业空间下有 project1 项目，分别启用了项目网关 project1、企业空间网关 workspace1 和集群网关，在项目 project1 中有个名为 demo 的 Ingress。此时通过集群网关、workspace1 企业空间网关、project1 项目网关对应的访问入口都访问到 demo Ingress 配置的后端。
网关升级从 3.4 升级到 4.1 后，就会新生成三份 Ingress。分别是 demo-kubesphere-system、demo-workspace-workspace1、demo-project1，它们分别关联到对应的网关。

**检查**
- 检查网关 helm release 状态
```sh
# 查看所有网关 helm release
helm ls -Aa | grep "kubesphere-router"

# 如果出现 pending 状态的，需要回滚到上个正常版本，gateway-congtroler 会重新对它做升级处理
# 查看 release 的历史记录
helm history <release> --namespace <namespace>
# 回滚到上个正常的状态
helm rollback <release> <revision> --namespace <namespace>
```
- 检查新生成的 Ingress 资源是否都能正常访问

### 5. 清理资源
等所有网关升级完成，验证无误后，清理 3.5 版本的遗留资源。
```sh
kubectl get namespaces --no-headers=true | awk '{print $1}' | xargs -I {} sh -c 'kubectl get nginxes.gateway.kubesphere.io -n {} -o name | xargs -I % kubectl patch % -n {} --type=json -p="[{\"op\": \"remove\", \"path\": \"/metadata/finalizers\"}]";'
kubectl delete crd nginxes.gateway.kubesphere.io
```