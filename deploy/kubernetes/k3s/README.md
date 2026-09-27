# 三节点 K3s 生产集群

本目录定义 `eagle-go` 的默认生产运行平台：三台 Linux 服务器组成 K3s HA 集群，三个节点同时
承担 control-plane、embedded etcd 和应用工作负载。应用清单位于
`../overlays/production-k3s`；这里保存集群级配置和 Envoy Gateway Helm values。

## 目标拓扑

```text
运维端 ── K3S_API_ENDPOINT:6443 ──┬─ server-1:6443
                                  ├─ server-2:6443
                                  └─ server-3:6443

客户端 ── api.example.com:443 ────┬─ server-1:443 ─┐
                                  ├─ server-2:443 ─┼─ Envoy Gateway → eagle Services
                                  └─ server-3:443 ─┘
```

两个入口都必须是健康检查后的负载均衡地址。云环境优先使用云 LB；自建网络可使用独立
HAProxy/Keepalived VIP。不要只把 API 或业务域名指向某一台服务器。

## 前置条件

- 三台服务器使用唯一 hostname、固定私网地址和 SSD，并位于低延迟互通网络；
- 任意两台容量足以承载核心工作负载，避免丢失一台后资源耗尽；
- 节点间开放 K3s 所需的 `6443/tcp`、`2379-2380/tcp`、`8472/udp` 和 `10250/tcp`，这些端口
  只能在可信私网或安全组内开放；
- 外部 LB 到三台节点开放 `80/tcp`、`443/tcp`；
- 已准备镜像仓库、OIDC、三个 PostgreSQL database、Redis、RabbitMQ、S3 和 OTLP 地址；
- 生产数据库和中间件不使用 K3s local-path 卷伪装高可用。

## 1. 初始化 K3s

把 `config.yaml.example` 复制到每台机器的 `/etc/rancher/k3s/config.yaml`，将
`K3S_API_ENDPOINT` 替换为控制面 LB 的固定域名或 VIP。在每台机器创建权限为 `0600` 的
`/etc/rancher/k3s/token`，内容必须相同并由 Secret 管理系统生成，不要提交到仓库。

在 server-1 初始化 embedded etcd。生产必须把 `K3S_VERSION` 替换为经过验证的固定版本，禁止
直接跟随 latest：

```bash
curl -sfL https://get.k3s.io | sudo env INSTALL_K3S_VERSION='K3S_VERSION' sh -s - server --cluster-init
```

控制面 LB 确认能访问 server-1 后，在 server-2 和 server-3 依次加入：

```bash
curl -sfL https://get.k3s.io | sudo env INSTALL_K3S_VERSION='K3S_VERSION' sh -s - server --server https://K3S_API_ENDPOINT:6443
```

每加入一台都先确认节点和 etcd 健康，再继续下一台：

```bash
sudo k3s kubectl get nodes -o wide
sudo k3s etcd-snapshot ls
```

`config.yaml.example` 禁用 K3s 默认 Traefik，但保留 ServiceLB。ServiceLB 会在三个节点为 Envoy
Gateway 占用 80/443，外部业务 LB 应对三个节点做 TCP 健康检查。不要再安装第二个 Ingress
Controller 或另一个占用 80/443 的 LoadBalancer Service。

## 2. 安装 Envoy Gateway

固定 Envoy Gateway chart 版本，并使用本目录 values 安装三副本控制面。Helm 默认同时安装
Gateway API 和 Envoy Gateway CRD；升级时必须先按该版本升级说明处理 CRD：

```bash
kubectl apply -f deploy/kubernetes/base/namespace.yaml
```

```bash
helm upgrade --install eg oci://docker.io/envoyproxy/gateway-helm \
  --version v1.9.0 \
  --namespace envoy-gateway-system \
  --create-namespace \
  --values deploy/kubernetes/k3s/envoy-gateway-values.yaml
```

```bash
kubectl wait --timeout=5m --namespace envoy-gateway-system \
  deployment/envoy-gateway --for=condition=Available
```

values 将 Envoy 数据面部署到 `eagle` namespace，使已有 NetworkPolicy 能识别网关流量；
`production-k3s` overlay 还会创建 `GatewayClass/envoy` 和三副本 `EnvoyProxy`。

## 3. 准备生产配置

不要修改并应用 `../secret.example.yaml`。由 External Secrets、Vault 或云 Secret Manager 创建
`eagle/eagle-runtime` 和 `eagle/eagle-tls`。至少替换以下非 Secret 示例值：

- `api.example.com`、`app.example.com`；
- 三个镜像为本次发布的不可变 digest；
- OIDC issuer、JWKS、token URL；
- Redis、S3、OTLP 地址和采样率；
- requests/limits、HPA、网关限流与熔断阈值。

先渲染检查：

```bash
make render-prod-k3s >/tmp/eagle-production-k3s.yaml
```

确认环境仓库已经完成替换后再应用：

```bash
kubectl apply -k deploy/kubernetes/overlays/production-k3s
```

## 4. 迁移与发布

应用 overlay 不包含 migration Job。每个发生变化的服务仍必须按以下顺序发布：

1. 选择不可变镜像 digest；
2. 以同一 digest 和唯一 Job 名创建该服务 migration Job；
3. 等待 Job `Complete`；
4. 更新该服务 Deployment，并等待 rollout 与 readiness；
5. 观察错误率、p99、Outbox、DLQ 和资源水位。

不要同时升级三个 K3s server。节点维护时一次只处理一台，先 `cordon/drain`，完成升级并确认
节点、etcd 和核心 Pod 恢复后再继续下一台。三个节点只能容忍一个节点故障。

## 5. 上线验证

```bash
kubectl get nodes
kubectl get gatewayclass envoy
kubectl get gateway -n eagle
kubectl get pods -n envoy-gateway-system -o wide
kubectl get pods -n eagle -o wide
kubectl get hpa,pdb -n eagle
```

必须确认：

- 三个节点均为 `Ready`，Envoy 控制面和数据面分布在不同 hostname；
- `Gateway/eagle` 为 `Programmed`，外部 LB 只转发到健康节点；
- admin、product、order 各至少三个 ready Pod；
- 拔掉任意一台服务器后 API、业务入口和 etcd quorum 仍可用；
- K3s etcd snapshot 已复制到集群之外，并完成过实际恢复演练；
- PostgreSQL、RabbitMQ、Redis 和对象存储具有各自的 HA 与异地备份，不依赖 Pod 重调度保护数据。
