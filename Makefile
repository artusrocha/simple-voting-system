SHELL := /usr/bin/env bash

# ===========================================
# Auto-detecção de container engine
# ===========================================

DETECTED_RUNTIME := $(shell (command -v docker >/dev/null 2>&1 && echo "docker") || (command -v podman >/dev/null 2>&1 && echo "podman") || echo "none")

ifeq ($(origin RUNTIME),undefined)
  RUNTIME := $(DETECTED_RUNTIME)
endif

ifeq ($(origin COMPOSE_CMD),undefined)
  COMPOSE_CMD := $(if $(filter docker,$(RUNTIME)),docker compose,podman-compose)
endif

ifeq ($(RUNTIME),none)
  $(error Nenhum container engine encontrado. Instale docker ou podman.)
endif

CONFIG ?= configs/dev.env
ENV_FILE ?= $(CONFIG)
COMPOSE_FILE ?= deploy/compose/docker-compose.yml
GO_IMAGE ?= docker.io/library/golang:1.23-bookworm
K6_IMAGE ?= docker.io/grafana/k6:latest
PLAYWRIGHT_IMAGE ?= mcr.microsoft.com/playwright:v1.58.2-jammy

GO_CACHE_DIR ?= $(PWD)/._/go
GO_MODCACHE_DIR ?= $(GO_CACHE_DIR)/mod
GO_BUILDCACHE_DIR ?= $(GO_CACHE_DIR)/build
GO_TEST_FLAGS ?=

.PHONY: help \
	config config-dev config-prod config-benchmark print-env \
	fmt test verify contracts-check \
	build build-no-cache up down restart rebuild ps logs logs-api logs-projector logs-frontend logs-grafana \
	status health urls \
	load-create-voting load-smoke load-sustained load-spike load-stress load-consistency load-consistency-topic \
	load-smoke-multi-api load-stress-multi-api \
	performance-index \
	clean-app-images clean-runtime clean-go-cache reset-runtime \
	k8s-validate k8s-apply k8s-delete \
	test-integration test-ui test-integration-full \
	dev prod benchmark

BENCHMARK_MULTI_API_BASE_URL ?= http://localhost:3002
BENCHMARK_CONTROL_API_BASE_URL ?= http://localhost:8080
BENCHMARK_SYNC_API_URLS ?= http://localhost:8080,http://localhost:8082

help: ## Shows available Makefile commands in a list
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## " }; {printf "make \033[36m%-30s\033[0m %s\n", $$1, $$2}'


config: ## Validate config file exists
	@test -f "$(CONFIG)" || (echo "Config file not found: $(CONFIG)" && exit 1)
	@printf '[make] using config %s\n' "$(CONFIG)"

config-dev: CONFIG=configs/dev.env
config-dev: config ## Use dev env file

config-prod: CONFIG=configs/prod.env
config-prod: config ## Use prod env file

config-benchmark: CONFIG=configs/benchmark.env
config-benchmark: config ## Use benchmark env file

print-env: config ## Print rendered env file path and contents
	@printf '[make] env file: %s\n' "$(ENV_FILE)"
	@cat "$(ENV_FILE)"

fmt: ## Format Go code via container
	@GO_IMAGE="$(GO_IMAGE)" RUNTIME="$(RUNTIME)" bash scripts/go-fmt-container.sh

test: ## Run Go tests via container
	@GO_IMAGE="$(GO_IMAGE)" RUNTIME="$(RUNTIME)" bash scripts/go-test-container.sh

contracts-check: ## Validate YAML and JSON contract/config files
	@python3 -c "import json,yaml; from pathlib import Path; files=[Path('.github/workflows/ci.yml'), Path('deploy/compose/docker-compose.yml'), Path('deploy/observability/prometheus/prometheus.yml'), Path('contracts/events/asyncapi/voting-events.yaml')]; [yaml.safe_load(path.read_text(encoding='utf-8')) or print('yaml ok', path) for path in files]; [json.load(path.open()) for path in Path('contracts/events/schemas/jsonschema').glob('*.json')]; print('json schemas ok')"

verify: fmt test contracts-check ## Run local verification suite

build: config ## Build application images
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" build api api-b projector frontend

build-no-cache: config ## Build application images without cache
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" build --no-cache api api-b projector frontend

up: config ## Start stack with current CONFIG
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" up -d

down: config ## Stop stack with current CONFIG
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" down --remove-orphans || true
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" rm -f || true

restart: down up ## Restart stack

rebuild: clean-app-images build-no-cache up ## Rebuild app images and start stack

ps: config ## Show compose service status
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" ps

logs: config ## Tail all compose logs
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" logs -f

logs-api: config ## Tail API logs
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" logs -f api api-b

logs-projector: config ## Tail projector logs
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" logs -f projector

logs-frontend: config ## Tail frontend logs
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" logs -f frontend

logs-grafana: config ## Tail Grafana logs
	@$(COMPOSE_CMD) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)" logs -f grafana

status: ps ## Alias for ps

health: ## Check key service health endpoints
	@curl -fsS http://localhost:8080/healthz && printf '\n---\n'
	@curl -fsS http://localhost:8081/healthz && printf '\n---\n'
	@curl -fsS http://localhost:3001/api/health && printf '\n---\n'
	@curl -fsS http://localhost:19090/-/healthy && printf '\n'

urls: ## Print common local URLs
	@printf 'vote UI:      http://localhost:3000/vote.html\n'
	@printf 'admin UI:     http://localhost:3000/admin.html\n'
	@printf 'api:          http://localhost:8080\n'
	@printf 'api-b:        http://localhost:8082\n'
	@printf 'benchmark lb: http://localhost:3002\n'
	@printf 'projector:    http://localhost:8081\n'
	@printf 'grafana:      http://localhost:3001\n'
	@printf 'prometheus:   http://localhost:19090\n'
	@printf 'scalar api:   http://localhost:3004\n'
	@printf 'kafka ui:     http://localhost:8085\n'

load-create-voting: ## Create a load-test voting
	@K6_IMAGE="$(K6_IMAGE)" bash scripts/run-loadtest.sh create-voting

load-smoke: ## Run smoke load test
	@K6_IMAGE="$(K6_IMAGE)" bash scripts/run-loadtest.sh smoke

load-smoke-multi-api: ## Run smoke load test through benchmark LB
	@API_BASE_URL="$(BENCHMARK_MULTI_API_BASE_URL)" CONTROL_API_BASE_URL="$(BENCHMARK_CONTROL_API_BASE_URL)" SYNC_API_URLS="$(BENCHMARK_SYNC_API_URLS)" K6_IMAGE="$(K6_IMAGE)" bash scripts/run-loadtest.sh smoke

load-sustained: ## Run sustained load test
	@K6_IMAGE="$(K6_IMAGE)" bash scripts/run-loadtest.sh sustained

load-spike: ## Run spike load test
	@K6_IMAGE="$(K6_IMAGE)" bash scripts/run-loadtest.sh spike

load-stress: ## Run stress load test (900 VUs, 3min plateau)
	@K6_IMAGE="$(K6_IMAGE)" bash scripts/run-loadtest.sh stress

load-stress-multi-api: ## Run stress load test through benchmark LB
	@API_BASE_URL="$(BENCHMARK_MULTI_API_BASE_URL)" CONTROL_API_BASE_URL="$(BENCHMARK_CONTROL_API_BASE_URL)" SYNC_API_URLS="$(BENCHMARK_SYNC_API_URLS)" K6_IMAGE="$(K6_IMAGE)" bash scripts/run-loadtest.sh stress

load-consistency: ## Run consistency load test
	@K6_IMAGE="$(K6_IMAGE)" bash scripts/run-loadtest.sh consistency

load-consistency-topic: ## Run consistency + topic verification load test
	@K6_IMAGE="$(K6_IMAGE)" bash scripts/run-loadtest.sh consistency-topic

performance-index: ## Render latest performance index from k6 summaries
	@python3 scripts/render-performance-index.py

clean-app-images: ## Remove locally built application images
	@$(RUNTIME) images --format '{{.Repository}}:{{.Tag}}' | grep -E '^localhost/voting-platform-(api|projector|frontend):latest$$' | xargs -r $(RUNTIME) rmi -f

clean-runtime: ## Remove platform containers from old and current prefixes
	@names=$$($(RUNTIME) ps -a --format '{{.Names}}' | grep -E '^(laager-|voting-platform-)' || true); \
	if [[ -n "$$names" ]]; then $(RUNTIME) rm -f $$names; fi; \
	printf '[make] removed platform containers if present\n'

clean-go-cache: ## Remove Go module and build cache
	@rm -rf "$(GO_CACHE_DIR)" && printf '[make] removed Go cache at %s\n' "$(GO_CACHE_DIR)"

reset-runtime: down clean-runtime clean-app-images ## Destroy local runtime artifacts

dev: CONFIG=configs/dev.env
dev: config up ## Use dev env and start stack

prod: CONFIG=configs/prod.env
prod: config up ## Use prod env and start stack

benchmark: CONFIG=configs/benchmark.env
benchmark: config up ## Use benchmark env and start stack

KUSTOMIZE := $(HOME)/bin/kustomize
KUBECONFIG ?= /etc/rancher/k3s/k3s.yaml
K8S_OVERLAY ?= dev
export KUBECONFIG

k8s-validate: ## Validate K8s manifests (requires kustomize installed)
	@if [ ! -x "$(KUSTOMIZE)" ]; then \
		echo "Error: kustomize not found at $(KUSTOMIZE)"; \
		echo "Please install kustomize first"; \
		exit 1; \
		fi
	@echo "Validating dev overlay..."
	@$(KUSTOMIZE) build deploy/kubernetes/overlays/dev > /dev/null
	@echo "Validating prod overlay..."
	@$(KUSTOMIZE) build deploy/kubernetes/overlays/prod > /dev/null
	@echo "✓ K8s manifests valid"

k8s-apply: ## Apply K8s manifests to cluster (requires cluster running and KUBECONFIG set)
	@if [ ! -x "$(KUSTOMIZE)" ]; then \
		echo "Error: kustomize not found at $(KUSTOMIZE)"; \
		echo "Please install kustomize first"; \
		exit 1; \
		fi
	@if ! kubectl cluster-info &> /dev/null; then \
		echo "Error: Cluster not reachable"; \
		echo "Ensure KUBECONFIG is set and cluster is running"; \
		exit 1; \
		fi
	@echo "Applying $(K8S_OVERLAY) overlay..."
	@$(KUSTOMIZE) build deploy/kubernetes/overlays/$(K8S_OVERLAY) | kubectl apply -f -
	@echo "✓ K8s manifests applied"
	@kubectl get all -A

k8s-delete: ## Delete K8s manifests from cluster (requires cluster running and KUBECONFIG set)
	@if [ ! -x "$(KUSTOMIZE)" ]; then \
		echo "Error: kustomize not found at $(KUSTOMIZE)"; \
		echo "Please install kustomize first"; \
		exit 1; \
		fi
	@if ! kubectl cluster-info &> /dev/null; then \
		echo "Error: Cluster not reachable"; \
		echo "Ensure KUBECONFIG is set and cluster is running"; \
		exit 1; \
		fi
	@echo "Deleting $(K8S_OVERLAY) overlay..."
	@$(KUSTOMIZE) build deploy/kubernetes/overlays/$(K8S_OVERLAY) | kubectl delete -f - --ignore-not-found
	@echo "✓ K8s manifests deleted"

test-integration: ## Run Go integration tests via container (excludes integration tag tests)
	@mkdir -p "$(GO_MODCACHE_DIR)" "$(GO_BUILDCACHE_DIR)"
	@$(RUNTIME) run --rm --network host \
		-v "$(PWD)/tests/integration:/src:Z" \
		-v "$(GO_MODCACHE_DIR):/go/pkg/mod:Z" \
		-v "$(GO_BUILDCACHE_DIR):/root/.cache/go-build:Z" \
		-w /src \
		"$(GO_IMAGE)" \
		sh -lc '/usr/local/go/bin/go mod download && /usr/local/go/bin/go test -v -tags='"'"'!integration'"'"' $(GO_TEST_FLAGS) ./...'

test-integration-full: ## Run all Go integration tests via container including container restart tests
	@mkdir -p "$(GO_MODCACHE_DIR)" "$(GO_BUILDCACHE_DIR)"
	@$(RUNTIME) run --rm --network host \
		-v "$(PWD)/tests/integration:/src:Z" \
		-v "$(GO_MODCACHE_DIR):/go/pkg/mod:Z" \
		-v "$(GO_BUILDCACHE_DIR):/root/.cache/go-build:Z" \
		-w /src \
		"$(GO_IMAGE)" \
		sh -lc '/usr/local/go/bin/go mod download && /usr/local/go/bin/go test -v $(GO_TEST_FLAGS) ./...'

test-ui: ## Run Playwright UI tests via container
	@$(RUNTIME) run --rm --network host \
		-v "$(PWD)/tests-ui:/work:Z" \
		-w /work \
		--entrypoint sh \
		"$(PLAYWRIGHT_IMAGE)" \
		-lc 'npm ci && npx playwright test'
