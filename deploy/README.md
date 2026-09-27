# 部署文件索引

本目录只保存部署资产，不承载完整教程：

- `docker-compose.yml`：本地依赖、三个服务、一次性迁移任务和 nginx 开发网关；
- `gateway/`：仅供 Compose 使用的本地 nginx 路由；
- `keycloak/`：本地 realm 与 Keycloak 说明；
- `observability/`：本地 Prometheus、Loki、Tempo、Alloy、Grafana 配置；
- `kubernetes/`：三节点 K3s 默认生产模式，以及通用 Kubernetes 基线、网关、告警和备份资源。

使用说明按环境拆分：

- [开发环境部署](../docs/development-deployment.md)
- [生产环境部署](../docs/deployment.md)
- [生产运行手册](../docs/operations.md)
- [Kubernetes 清单说明](kubernetes/README.md)
- [三节点 K3s 生产集群](kubernetes/k3s/README.md)

当前发布单元只有 `admin`、`product`、`order`。file、notification 是 admin 拥有的业务模块，
不是独立容器。`*-migrate` 是发布前的一次性数据库迁移任务，也不是额外的常驻服务。
