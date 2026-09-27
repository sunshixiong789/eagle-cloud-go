SHELL := /bin/bash
VERSION := $(shell git describe --tags --always 2>/dev/null || echo "dev")
LDFLAGS := -X main.Version=$(VERSION)

SERVICES := admin product order
MODULES := api pkg app/admin app/product app/order tests tools
SERVICE ?= admin
REGISTRY ?= eagle
BUF := go tool buf
GOOSE := go tool goose
GOLANGCI_LINT := go tool golangci-lint
# 每个服务拥有独立 database；可用 EAGLE_DSN 覆盖。
EAGLE_DSN ?= postgres://eagle:eagle@127.0.0.1:5432/eagle_$(SERVICE)?sslmode=disable
MIGRATION_DIR := app/$(SERVICE)/migrations

.PHONY: init
# 下载并验证项目锁定的开发期工具链
init:
	$(BUF) --version
	$(GOOSE) -version
	$(GOLANGCI_LINT) --version

.PHONY: api
# 生成对外契约代码（api/ 下的 proto）
api:
	$(BUF) generate --template buf.gen.yaml

.PHONY: config
# 生成共享进程配置代码（pkg/platform/config）
config:
	$(BUF) generate --template buf.gen.config.yaml

.PHONY: lint-proto
# proto 风格检查 + 兼容性检查（against master）
lint-proto:
	$(BUF) lint
	$(BUF) breaking api --against '.git#branch=master,subdir=api'

.PHONY: ent
# 为每个服务生成独占的 Ent 数据访问代码
ent:
	@for service in $(SERVICES); do \
		go generate ./app/$$service/internal/platform/database/ent || exit 1; \
	done

.PHONY: tidy
# 分别整理每个 Go 模块的依赖
tidy:
	@for module in $(MODULES); do \
		go -C $$module mod tidy || exit 1; \
	done

.PHONY: migrate-up
# 执行数据库迁移
migrate-up:
	$(GOOSE) -dir $(MIGRATION_DIR) postgres "$(EAGLE_DSN)" up

.PHONY: migrate-down
# 回滚一个版本
migrate-down:
	$(GOOSE) -dir $(MIGRATION_DIR) postgres "$(EAGLE_DSN)" down

.PHONY: migrate-status
migrate-status:
	$(GOOSE) -dir $(MIGRATION_DIR) postgres "$(EAGLE_DSN)" status

.PHONY: wire
# 为每个服务组合根生成 Wire 注入代码
wire:
	@for service in $(SERVICES); do \
		go -C app/$$service run github.com/google/wire/cmd/wire ./cmd/$$service || exit 1; \
	done

.PHONY: generate
# 全量生成：对外契约 + 内部配置 + Ent + Wire
generate: api config ent wire tidy

.PHONY: build
# 编译全部可部署服务到 bin/
build:
	mkdir -p bin/
	@for service in $(SERVICES); do \
		go build -ldflags "$(LDFLAGS)" -o ./bin/$$service ./app/$$service/cmd/$$service || exit 1; \
	done
	go build -o ./bin/migrate ./tools/migrate
	go build -o ./bin/outboxctl ./tools/outboxctl
	go build -o ./bin/mqctl ./tools/mqctl

.PHONY: image
# 构建一个服务镜像，例如 make image SERVICE=product VERSION=v1.2.0 REGISTRY=registry.example.com/eagle
image:
	@case " $(SERVICES) " in *" $(SERVICE) "*) ;; *) echo "unknown SERVICE=$(SERVICE), choose: $(SERVICES)"; exit 2;; esac
	docker build --build-arg SERVICE=$(SERVICE) --build-arg VERSION=$(VERSION) \
		-t $(REGISTRY)/$(SERVICE):$(VERSION) .

.PHONY: images
# 分别构建三个可独立发布的服务镜像
images:
	@for service in $(SERVICES); do \
		docker build --build-arg SERVICE=$$service --build-arg VERSION=$(VERSION) \
			-t $(REGISTRY)/$$service:$(VERSION) . || exit 1; \
	done

.PHONY: push-images
# 推送三个服务镜像；生产发布时显式执行，不绑定到 build
push-images:
	@for service in $(SERVICES); do \
		docker push $(REGISTRY)/$$service:$(VERSION) || exit 1; \
	done

.PHONY: run
# 启动一个服务，例如 make run SERVICE=product
run:
	EAGLE_DATABASE_DSN="$(EAGLE_DSN)" go run -ldflags "$(LDFLAGS)" ./app/$(SERVICE)/cmd/$(SERVICE) -conf app/$(SERVICE)/configs

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run ./api/... ./pkg/... ./app/admin/... ./app/product/... ./app/order/... ./tests/... ./tools/...

.PHONY: test
# 单测 + 集成测试（embedded-postgres，不需要 Docker）
test:
	@for module in $(MODULES); do \
		go test -race -cover ./$$module/... || exit 1; \
	done

.PHONY: up
# 构建并启动三服务开发环境
up:
	docker compose -f deploy/docker-compose.yml up -d --build

.PHONY: up-deps
# 只启动本地基础依赖，服务由 make run 单独启动
up-deps:
	docker compose -f deploy/docker-compose.yml up -d postgres keycloak redis rabbitmq minio minio-init

.PHONY: validate-deploy
# 校验 Compose 与所有可部署 Kubernetes 组合能被正确解析
validate-deploy:
	docker compose -f deploy/docker-compose.yml config --quiet
	@for manifest in \
		deploy/kubernetes/base \
		deploy/kubernetes/migrations \
		deploy/kubernetes/gateway \
		deploy/kubernetes/overlays/staging \
		deploy/kubernetes/overlays/production \
		deploy/kubernetes/overlays/production-k3s \
		deploy/kubernetes/overlays/production-mtls \
		deploy/kubernetes/observability \
		deploy/kubernetes/backup; do \
		kubectl kustomize $$manifest >/dev/null || exit 1; \
	done

.PHONY: render-prod-k3s
# 渲染三节点 K3s 生产应用清单，不连接集群、不执行发布
render-prod-k3s:
	kubectl kustomize deploy/kubernetes/overlays/production-k3s

.PHONY: down
down:
	docker compose -f deploy/docker-compose.yml down

.PHONY: all
all: generate build

.PHONY: help
help:
	@awk '/^[a-zA-Z\-\_0-9]+:/ { \
		helpMessage = match(lastLine, /^# (.*)/); \
		if (helpMessage) { \
			helpCommand = substr($$1, 0, index($$1, ":")); \
			helpMessage = substr(lastLine, RSTART + 2, RLENGTH); \
			printf "\033[36m%-18s\033[0m %s\n", helpCommand, helpMessage; \
		} \
	} { lastLine = $$0 }' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help
