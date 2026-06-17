IMAGE        ?= resource-webhook:latest
KIND_CLUSTER ?= resource-webhook
NAMESPACE    ?= webhook-system
KUBECONFIG   ?= /root/.kube/config
KUBECTL      := kubectl --kubeconfig $(KUBECONFIG)

# Detect whether we are already inside the dev container
IN_CONTAINER := $(shell test -f /.dockerenv && echo yes || echo no)

# Run a target inside the dev container when called from the host
define in_container
	@if [ "$(IN_CONTAINER)" = "no" ]; then \
	  docker compose exec dev make $1; \
	else \
	  $(MAKE) _$1; \
	fi
endef

.PHONY: help dev shell clean \
        test build kind-up \
        test-integration test-clean \
        _test _build _kind-up _kind-connect-net _certs-cluster \
        _test-integration _test-clean

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?##' $(MAKEFILE_LIST) | \
	  awk 'BEGIN{FS=":.*?## "}{printf "  %-20s %s\n",$$1,$$2}'

# ── Host-only targets ─────────────────────────────────────────────────────────

dev: ## Build dev image and start webhook with hot reload
	docker compose up --build

shell: ## Open a shell in the running dev container
	docker compose exec dev sh

clean: ## Stop containers, delete kind cluster, remove certs and build artifacts
	docker compose down -v
	kind delete cluster --name $(KIND_CLUSTER) 2>/dev/null || true
	sudo rm -rf certs/ tmp/

# ── Delegating targets (work from host OR inside container) ───────────────────

test: ## Run unit tests
	$(call in_container,test)

build: ## Build production Docker image
	$(call in_container,build)

kind-up: ## Create cluster, build & load image, install certs and webhook (full setup)
	@if [ "$(IN_CONTAINER)" = "no" ]; then \
	  docker compose up -d --build; \
	fi
	$(call in_container,kind-up)

test-integration: ## Apply test manifests and verify allow/deny behavior
	$(call in_container,test-integration)

test-clean: ## Remove integration test resources
	$(call in_container,test-clean)

# ── Real implementations (run inside container) ───────────────────────────────

_test:
	go test ./... -v

_build:
	docker build -t $(IMAGE) /app

_kind-up:
	@echo "==> [1/6] Creating kind cluster..."
	@kind get clusters | grep -q $(KIND_CLUSTER) \
	  && echo "    Cluster '$(KIND_CLUSTER)' already exists, skipping." \
	  || kind create cluster --name $(KIND_CLUSTER) --config /app/kind-config.yaml
	@echo "==> [2/6] Connecting dev container to kind network..."
	@docker network connect kind $$(hostname) 2>/dev/null || true
	@KIND_IP=$$(docker inspect $(KIND_CLUSTER)-control-plane \
	  --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}') && \
	sed -i "s|server: https://127.0.0.1:[0-9]*|server: https://$$KIND_IP:6443|g" $(KUBECONFIG) && \
	echo "    API server → https://$$KIND_IP:6443"
	@echo "==> [3/6] Building and loading image into kind..."
	@docker build -t $(IMAGE) /app
	@kind load docker-image $(IMAGE) --name $(KIND_CLUSTER)
	@echo "==> [4/6] Generating TLS certs for webhook-service.$(NAMESPACE).svc..."
	@mkdir -p /app/certs
	@openssl req -x509 -newkey rsa:4096 \
	  -keyout /app/certs/tls.key -out /app/certs/tls.crt \
	  -days 365 -nodes \
	  -subj "/CN=webhook-service.$(NAMESPACE).svc" \
	  -addext "subjectAltName=DNS:webhook-service.$(NAMESPACE).svc,DNS:webhook-service.$(NAMESPACE).svc.cluster.local" \
	  2>/dev/null
	@echo "==> [5/6] Applying manifests..."
	@$(KUBECTL) apply -f /app/manifests/namespace.yaml
	@$(KUBECTL) -n $(NAMESPACE) create secret tls webhook-tls \
	  --cert=/app/certs/tls.crt --key=/app/certs/tls.key \
	  --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@$(KUBECTL) apply -f /app/manifests/serviceaccount.yaml
	@$(KUBECTL) apply -f /app/manifests/deployment.yaml
	@$(KUBECTL) apply -f /app/manifests/service.yaml
	@CA_BUNDLE=$$(base64 -w0 /app/certs/tls.crt) && \
	sed "s|REPLACE_CA_BUNDLE|$$CA_BUNDLE|g" /app/manifests/validatingwebhookconfiguration.yaml | \
	$(KUBECTL) apply -f -
	@echo "==> [6/6] Waiting for webhook pod to be ready..."
	@$(KUBECTL) -n $(NAMESPACE) rollout status deployment/resource-webhook --timeout=120s
	@echo ""
	@echo "Webhook is live. Run 'make test-integration' to verify."

_test-integration:
	@echo "--- ALLOWED (should succeed) ---"
	$(KUBECTL) apply -f /app/test/good-deployment.yaml
	$(KUBECTL) apply -f /app/test/good-statefulset.yaml
	$(KUBECTL) apply -f /app/test/skipped-deployment.yaml
	@echo ""
	@echo "--- DENIED (should fail) ---"
	$(KUBECTL) apply -f /app/test/bad-deployment.yaml  && echo "ERROR: should have been denied" || echo "OK: denied as expected"
	$(KUBECTL) apply -f /app/test/bad-statefulset.yaml && echo "ERROR: should have been denied" || echo "OK: denied as expected"

_test-clean:
	$(KUBECTL) delete -f /app/test/ --ignore-not-found
