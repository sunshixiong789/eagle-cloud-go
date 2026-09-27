# 一个 Dockerfile 是构建模板；SERVICE 决定本次只编译并打包哪个服务。
FROM golang:1.27-alpine AS builder

ARG GOPROXY=https://goproxy.cn,direct
ARG VERSION=dev
ARG SERVICE=admin
ENV GOPROXY=${GOPROXY} CGO_ENABLED=0 GOOS=linux

WORKDIR /src
COPY . .

# BuildKit cache 保留模块与编译缓存；go build 只拉取当前目标真实使用的依赖，
# 不会为了构建 product 再编译 admin/order 或开发期 lint 工具。
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    case "${SERVICE}" in admin|product|order) ;; *) exit 2 ;; esac && \
    go build \
      -trimpath \
      -ldflags "-w -s -X main.Version=${VERSION}" \
      -o /out/service \
      ./app/${SERVICE}/cmd/${SERVICE} && \
    go build -trimpath -ldflags "-w -s" -o /out/migrate ./tools/migrate && \
    go build -trimpath -ldflags "-w -s" -o /out/healthcheck ./tools/healthcheck && \
    mkdir -p /out/migrations && \
    cp -R "app/${SERVICE}/migrations/." /out/migrations/ && \
    cp -R "app/${SERVICE}/configs" /out/configs

FROM gcr.io/distroless/static-debian12:nonroot

ARG SERVICE
ARG VERSION
LABEL org.opencontainers.image.title="eagle-${SERVICE}" \
      org.opencontainers.image.version="${VERSION}"

COPY --from=builder /out/service /app/service
COPY --from=builder /out/migrate /app/migrate
COPY --from=builder /out/healthcheck /app/healthcheck
COPY --from=builder /out/migrations /app/migrations
COPY --from=builder /out/configs /app/configs

WORKDIR /app

# 容器内统一使用固定端口；宿主机端口由 Compose/Kubernetes 映射。
ENV EAGLE_SERVER_HTTP_ADDR=0.0.0.0:8000 \
    EAGLE_SERVER_GRPC_ADDR=0.0.0.0:9000 \
    EAGLE_OBSERVABILITY_METRICS_ADDR=0.0.0.0:9100
EXPOSE 8000 9000 9100

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=12 \
  CMD ["/app/healthcheck", "-url", "http://127.0.0.1:9100/readyz"]

USER nonroot:nonroot
ENTRYPOINT ["/app/service"]
CMD ["-conf", "/app/configs"]
