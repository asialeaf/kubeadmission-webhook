# kubeadmission-webhook

K8S 准入控制器，控制Deployment类型资源，实现指定namespace和name的动态准入控制。

#### 实现需求

##### 应用列表配置文件

```json
[
    {
        "namespace": "default",
        "name": "deployname1",
        "mixed": false,   #混部开关状态，true代表打开，false代表不打开
        "priority": 100   #优先级
    },
    {
        "namespace": "business-system",
        "name": "deployname2",
        "mixed": true,
        "priority": 89
    }
  	...
]
```

##### 准入控制器变更逻辑

1. 拦截到deployment的创建或者更新，通过namespace和name两个字段的值与应用列表配置文件中的应用列表进行匹配，如果不存在于应用列表中，则直接跳过；

2. 如果存在于应用列表中，并根据配置文件中对应应用的mixed: true值更新对应deployment中定义pod的lables值；如果不存在该label，则增加label，hc/mixed-pod=`${mixed}`的值；如果存在该label，则修改label，hc/mixed-pod=`${mixed}`后的值

3. 如果存在于应用列表中，并根据配置文件中对应应用的priority: `${priority}`值更新对应deployment中定义pod的annotations值；如果不存在该annotations，则增加label，hc/priority=`${priority}`的值；如果存在该annotations，则修改label，hc/priority=`${priority}`后的值

4. 如果${mixed}的值为true时，按照json patch规范对Pod进行如下变更：

- 所有容器新增request.cmos.mixed/cpu和request.cmos.mixed/memoryu扩展资源，值与原容器的request.cpu和request.memory相同，limit上设置同样的扩展资源

- 新增request.cmos.mixed/podcount资源，该值等于1

- 替换所有容器的request.cpu=0 request.memory=0

- 为Pod增加NodeSelector，值为cmos/mixed-schedule=true

#### 部署步骤

1. ##### 生成自签证书及创建证书secret

   ```shell
   ./hack/gen-admission-secret.sh
   ```
   
1. ##### 部署应用程序

   ###### 使用[ko](https://github.com/google/ko)一键部署(推荐)
   
   提前安装ko
   
   ```shell
   go install github.com/google/ko@latest
   ```
   
   ```shell
   #设置镜像仓库地址，ko会把制作好的镜像push该仓库。
   export KO_DOCKER_REPO=docker.io/asialeaf
   #部署
   ko apply -Rf deploy/deployment_ko.yaml
   ```
   
   deployment_ko.yaml
   ```yaml
   apiVersion: v1
   kind: ServiceAccount
   metadata:
     name: admission-registry-sa
   
   ---
   
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: admission-registry-clusterrole
   rules:
     - apiGroups: ["admissionregistration.k8s.io"]
       resources: ["mutatingwebhookconfigurations", "validatingwebhookconfigurations"]
       verbs: ["get", "list", "watch", "create", "update"]
     # Rules below is used generate admission service secret
     - apiGroups: ["certificates.k8s.io"]
       resources: ["certificatesigningrequests"]
       verbs: ["get", "list", "create", "delete"]
     - apiGroups: ["certificates.k8s.io"]
       resources: ["certificatesigningrequests/approval"]
       verbs: ["create", "update"]
     - apiGroups: [""]
       resources: ["secrets"]
       verbs: ["create", "get", "patch"]
     - apiGroups: [""]
       resources: ["services"]
       verbs: ["get"]
   
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRoleBinding
   metadata:
     name: admission-registry-clusterrolebinding
   roleRef:
     apiGroup: rbac.authorization.k8s.io
     kind: ClusterRole
     name: admission-registry-clusterrole
   subjects:
     - kind: ServiceAccount
       name: admission-registry-sa
       namespace: default
   
   ---
   apiVersion: v1
   data:
     config.json: |
       [
           {
               "namespace": "default",
               "name": "nginx-test",
               "mixed": true,
               "priority": 102
           },
           {
               "namespace": "devops",
               "name": "busybox-test",
               "mixed": false,
               "priority": 89
           }
       ]
   kind: ConfigMap
   metadata:
     name: admission-registry-config
     namespace: default
   
   ---
   apiVersion: apps/v1
   kind: Deployment
   metadata:
     name: admission-registry
     labels:
       app: admission-registry
   spec:
     selector:
       matchLabels:
         app: admission-registry
     template:
       metadata:
         labels:
           app: admission-registry
       spec:
         serviceAccountName: admission-registry-sa
         containers:
         - args:
           - --web.listen-address=:8443
           - --config.file=/etc/webhook/config/config.json
           - --tls.cert-file=/etc/webhook/certs/tls.crt
           - --tls.private-key-file=/etc/webhook/certs/tls.key
           - --web.enable-lifecycle
           - --log.level=debug
           name: kubeadmission-webhook
           image: ko://git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/cmd/kubeadmission-webhook
           imagePullPolicy: IfNotPresent
           ports:
           - name: webhook-https
             protocol: TCP
             containerPort: 8443
           livenessProbe:
             failureThreshold: 3
             httpGet:
               path: /-/healthy
               port: 8443
               scheme: HTTPS
             initialDelaySeconds: 5
             periodSeconds: 10
             successThreshold: 1
             timeoutSeconds: 5
           readinessProbe:
             failureThreshold: 3
             httpGet:
               path: /-/ready
               port: 8443
               scheme: HTTPS
             initialDelaySeconds: 5
             periodSeconds: 10
             successThreshold: 1
             timeoutSeconds: 5
           resources:
             limits:
               cpu: 2
               memory: 1Gi      
             requests:
               cpu: 1
               memory: 500Mi 
           volumeMounts:
           - name: admission-certs
             mountPath: /etc/webhook/certs
             readOnly: true
           - mountPath: /etc/webhook/config
             name: config
             readOnly: true
         restartPolicy: Always
         nodeSelector: 
           kubernetes.io/hostname: k8s-master
         tolerations:
         - effect: NoSchedule
           operator: Exists
         volumes:
         - name: admission-certs
           secret:
             defaultMode: 420
             secretName: admission-registry-secret
         - configMap:
             defaultMode: 420
             name: admission-registry-config
           name: config
   ---
   apiVersion: v1
   kind: Service
   metadata:
     name: admission-registry
     labels:
       app: admission-registry
   spec:
     ports:
       - port: 443
         targetPort: 8443
     selector:
       app: admission-registry
   ```

   检查日志输出

   ```shell
   [root@k8s-master kubeadmission-webhook]# kubectl logs -f admission-registry-8674b56cc4-qqtwm
   ts=2022-07-29T01:51:18.652Z caller=main.go:58 level=info msg="Starting kubeadmission-webhook" version="(version=v1.0, branch=main, revision=)"
   ts=2022-07-29T01:51:18.652Z caller=main.go:59 level=info msg="Build context" (gogo1.17.3,useryeyazhou@harmonycloud.cn,date2022-07-2901:51:18UTCFri)=(MISSING)
   ts=2022-07-29T01:51:18.652Z caller=coordinator.go:83 level=info component=configuration file=/etc/webhook/config/config.json msg="Loading configuration file"
   ts=2022-07-29T01:51:18.652Z caller=coordinator.go:91 level=info component=configuration file=/etc/webhook/config/config.json msg="Completed loading of configuration file"
   ts=2022-07-29T01:51:18.654Z caller=web.go:81 level=info component=web msg="Start listening for connections" address=:8443
   ...
   
   ```
   
   ###### 使用Dockerfile部署
   
   构建镜像并上传至镜像仓库
   
   ```shell
   docker build -t local.harbor.io/kubeadmission-webhook:v1.0 .
   docker push local.harbor.io/kubeadmission-webhook:v1.0
   ```
   
   修改deployment.yaml内镜像地址
   
   ```shell
   vim deploy/deployment.yaml
   ```
   
   ```yaml
   ...
       spec:
         serviceAccountName: admission-registry-sa
         containers:
         - args:
           - --web.listen-address=:8443
           - --config.file=/etc/webhook/config/config.json
           - --tls.cert-file=/etc/webhook/certs/tls.crt
           - --tls.private-key-file=/etc/webhook/certs/tls.key
           - --web.enable-lifecycle
           name: kubeadmission-webhook
           image: local.harbor.io/kubeadmission-webhook:v1.0  #镜像地址
           imagePullPolicy: IfNotPresent
           ports:
           - name: webhook-https
             protocol: TCP
             containerPort: 8443
   ...
   ```
   
   创建部署
   
   ```
   kubectl apply -f deploy/deployment.yaml
   ```

3. ##### 创建准入控制规则

   ###### 获取bundle证书

   ```shell
   ./hack/webhook-gen-ca-bundle.sh
   ```

   ###### 应用规则

   ```
   kubectl apply -f deploy/webhookconfiguration/mutatingwebhookconfiguration.yaml
   ```

   mutatingwebhookconfiguration.yaml

   ```yaml
   apiVersion: admissionregistration.k8s.io/v1
   kind: MutatingWebhookConfiguration
   metadata:
     name: admission-registry
   webhooks:
   - name: cn.harmonycloud.admission-registry
     rules:
     - apiGroups:   ["apps",""]
       apiVersions: ["v1"]
       operations:  ["CREATE","UPDATE"]
       resources:   ["deployments"]
     clientConfig:
       service:
         namespace: default
         name: admission-registry
         path: "/admission/mutate"
       caBundle: ${CABUNDLE}
     admissionReviewVersions: ["v1", "v1beta1"]
     sideEffects: None
     timeoutSeconds: 5
     namespaceSelector:
       matchLabels:
         admission-webhook: enabled
   ```