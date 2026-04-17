# OptiPilot Helm Chart

Installs OptiPilot controller + forecaster into Kubernetes.

## Prerequisites

1. Kubernetes cluster (Kind/minikube/EKS/GKE/AKS).
2. Helm 3+.
3. Prometheus reachable by the controller.
   - Use external URL via `prometheus.address`, or
   - Leave empty to use `prometheus.fallbackAddress` (default points to kube-prometheus-stack service).

## Install

```bash
helm install optipilot ./charts/optipilot \
  -n optipilot-system \
  --create-namespace
```

## Upgrade

```bash
helm upgrade optipilot ./charts/optipilot -n optipilot-system
```

## Validate

```bash
helm lint ./charts/optipilot
helm template optipilot ./charts/optipilot -n optipilot-system > /tmp/optipilot-rendered.yaml
kubectl -n optipilot-system get pods,svc,pvc
helm test optipilot -n optipilot-system
```

## Values reference (key settings)

| Key | Description | Default |
| --- | --- | --- |
| `namespace` | Namespace to render resources into | `optipilot-system` |
| `createNamespace` | Create namespace manifest in chart output | `false` |
| `controller.image.*` | Controller image settings | `ghcr.io/abi-0611/optipilot/controller:latest` |
| `forecaster.image.*` | Forecaster image settings | `ghcr.io/abi-0611/optipilot/forecaster:latest` |
| `dashboard.enabled` | Expose dashboard/controller service | `true` |
| `dashboard.service.type` | Service type for dashboard | `ClusterIP` |
| `prometheus.address` | Explicit Prometheus URL | `""` |
| `storage.controller.size` | PVC size for controller DB | `5Gi` |
| `storage.forecaster.size` | PVC size for forecaster DB/models | `10Gi` |
| `scaling.defaultMode` | Initial operating mode | `shadow` |
| `scaling.defaultMinReplicas` | Default lower replica bound | `2` |
| `scaling.defaultMaxReplicas` | Default upper replica bound | `15` |
| `scaling.cooldownScaleUp` | Cooldown for scale-up actions (sec) | `120` |
| `scaling.cooldownScaleDown` | Cooldown for scale-down actions (sec) | `600` |
| `forecaster.training.minDataPoints` | Retrain threshold | `1440` |
| `forecaster.training.fullRetrainIntervalHours` | Full retrain cadence | `24` |
| `rbac.create` | Create ClusterRole/Binding | `true` |
| `serviceAccount.name` | Existing service account to use | `""` (auto-generated when created) |

## Access dashboard

```bash
kubectl -n optipilot-system port-forward svc/optipilot-optipilot-controller 8080:8080
```

Then open: <http://localhost:8080>

## Uninstall

```bash
helm uninstall optipilot -n optipilot-system
```

