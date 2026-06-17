# k8s Resource Validation Webhook

A Kubernetes **Validating Admission Webhook** written in Go that enforces two rules on every `Deployment` and `StatefulSet` in labeled namespaces:

1. Every container and initContainer must declare `requests` and `limits` for **cpu** and **memory**.
2. Every resource type used in a container (cpu, memory, GPU, custom devices, etc.) must be covered by a **ResourceQuota** in the target namespace.

Enforcement is opt-in: only namespaces labeled `resource-validation=enabled` are checked.

## How It Works

```
kubectl apply → API Server → Webhook (/validate, TLS)
                                │
                                ├─ validatePodSpec   → all containers have cpu/memory requests+limits?
                                ├─ fetch ResourceQuota from namespace
                                └─ validateQuota     → every resource type appears in the quota?
                                        │
                                   allow / deny (with violation list)
```

The `ValidatingWebhookConfiguration` uses a `namespaceSelector` to limit scope — unlabeled namespaces never reach the webhook. `failurePolicy: Fail` means if the webhook is unreachable, admission is denied in enforced namespaces.

## Validation Rules

| Scenario | Decision |
|---|---|
| All cpu/memory requests and limits set, all quota'd | Allow |
| Any container missing cpu or memory request/limit | Deny |
| Container uses a resource not in the namespace's ResourceQuota | Deny |
| No ResourceQuota exists in the namespace | Deny |
| Namespace without `resource-validation=enabled` label | Allow (skipped) |
| Webhook unreachable | Deny (`failurePolicy: Fail`) |

### ResourceQuota matching

A quota entry covers a resource if it matches any of:
- bare name: `cpu` (covers both requests and limits)
- `requests.cpu`
- `limits.cpu`

Extended resources (e.g. `nvidia.com/gpu`) require a `requests.nvidia.com/gpu` entry in the quota.

## Quick Start

```bash
# 1. Start dev container + create kind cluster + deploy webhook (one command)
make kind-up

# 2. Run integration tests
make test-integration

# 3. Clean up
make clean
```

`make kind-up` handles everything: creates the kind cluster, generates TLS certs, builds and loads the image, applies all manifests, and waits for the webhook pod to be ready.

## Enforcing a Namespace

Apply the label to any namespace you want to enforce:

```bash
kubectl label namespace my-namespace resource-validation=enabled
```

Create a `ResourceQuota` in that namespace declaring which resource types are permitted:

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: default-quota
  namespace: my-namespace
spec:
  hard:
    requests.cpu: "4"
    requests.memory: "8Gi"
    limits.cpu: "8"
    limits.memory: "16Gi"
    # Uncomment to allow GPU workloads:
    # requests.nvidia.com/gpu: "4"
```

Any workload requesting a resource type not listed here will be denied at admission time.

## Development

All `make` targets auto-detect whether they are running inside the dev container and delegate accordingly.

```bash
make dev              # start dev container with Air hot-reload (host)
make shell            # open shell in the running dev container
make test             # run unit tests  (go test ./... -v)
make build            # build production Docker image
make kind-up          # full cluster setup (starts dev container if needed)
make test-integration # apply test fixtures, verify allow/deny behavior
make test-clean       # remove integration test resources
make clean            # stop containers, delete kind cluster, remove certs
```

Run a single unit test from inside the container:

```bash
go test ./webhook/... -run TestQuotaUncoveredResource -v
```

## Project Structure

```
main.go                  # TLS server (:8443) + health server (:8080)
webhook/
  handler.go             # AdmissionReview HTTP handler (Handler struct)
  validator.go           # validatePodSpec + validateQuota logic
  quota.go               # QuotaFetcher interface + Kubernetes API implementation
  validator_test.go      # unit tests for both validators
manifests/
  namespace.yaml         # webhook-system + example test-enforced namespace
  serviceaccount.yaml
  clusterrole.yaml       # resourcequotas get/list
  clusterrolebinding.yaml
  deployment.yaml
  service.yaml
  resource-quota.yaml    # example ResourceQuota for test-enforced namespace
  validatingwebhookconfiguration.yaml
test/
  good-deployment.yaml       # all resources set + quota'd → allowed
  good-statefulset.yaml
  skipped-deployment.yaml    # unlabeled namespace → skipped
  bad-deployment.yaml        # missing limits → denied
  bad-statefulset.yaml       # missing requests+limits → denied
  bad-quota-deployment.yaml  # unquota'd resource → denied
```

## Configuration

The webhook binary reads configuration from environment variables:

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8443` | TLS listener port |
| `HEALTH_PORT` | `8080` | Health check port (`GET /healthz`) |
| `TLS_CERT` | `certs/tls.crt` | Path to TLS certificate |
| `TLS_KEY` | `certs/tls.key` | Path to TLS private key |

TLS certificates are self-signed and generated automatically by `make kind-up` with the correct SAN for `webhook-service.webhook-system.svc`.

## Dependencies

- `k8s.io/api` + `k8s.io/apimachinery` — Kubernetes type definitions
- `k8s.io/client-go` — in-cluster Kubernetes API client (reads ResourceQuota objects)

No controller-runtime or admission framework — the binary is a plain HTTPS server (~10 MB distroless image).
