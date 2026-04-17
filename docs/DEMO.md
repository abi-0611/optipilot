# OptiPilot End-to-End Demo (Kind + Helm + Vegeta)

This walkthrough demonstrates OptiPilot from deployment to proactive scaling behavior.

## 1. Prerequisites

- Docker
- kind
- kubectl
- Helm
- Vegeta (`vegeta` binary in `PATH`)

## 2. Create a fresh Kind cluster

```bash
kind create cluster --name optipilot --config deploy/kind-config.yaml
kubectl config use-context kind-optipilot
```

## 3. Install Prometheus (kube-prometheus-stack prerequisite)

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -n monitoring --create-namespace \
  -f deploy/prometheus/values.yaml
```

Wait until Prometheus is ready:

```bash
kubectl -n monitoring get pods
```

## 4. Build and load OptiPilot images into Kind

```bash
docker build -t optipilot-controller:demo -f Dockerfile.controller .
docker build -t optipilot-forecaster:demo -f Dockerfile.forecaster .

kind load docker-image optipilot-controller:demo --name optipilot
kind load docker-image optipilot-forecaster:demo --name optipilot
```

## 5. Install OptiPilot via Helm

```bash
helm install optipilot ./charts/optipilot \
  -n optipilot-system \
  --create-namespace \
  --set controller.image.repository=optipilot-controller \
  --set controller.image.tag=demo \
  --set controller.image.pullPolicy=IfNotPresent \
  --set forecaster.image.repository=optipilot-forecaster \
  --set forecaster.image.tag=demo \
  --set forecaster.image.pullPolicy=IfNotPresent
```

Validate deployment:

```bash
kubectl -n optipilot-system get deploy,svc,pods,pvc
helm test optipilot -n optipilot-system
```

## 6. Deploy test services (labeled for OptiPilot)

```bash
kubectl apply -f deploy/manifests/test-services.yaml
kubectl get deploy,svc -n default
```

`test-services.yaml` already includes:
- `optipilot.io/enabled=true`
- min/max replicas
- mode annotation

## 7. Access dashboard

```bash
kubectl -n optipilot-system port-forward svc/optipilot-optipilot-controller 8080:8080
```

Open: <http://localhost:8080>

Expected:
- services appear in dashboard APIs
- initial state is learning/shadow-like behavior while data accumulates

## 8. Run Vegeta realistic scenario

In a separate terminal:

```bash
bash loadtest/run-all.sh
```

This runs `loadtest/scenarios/realistic.sh`, which combines:
- ramp pattern
- spike pattern
- periodic pulse pattern
across 3 services.

Results are saved under `loadtest/results/` with timestamped report files.

## 9. Observe predictive autoscaling behavior

During/after load:

```bash
kubectl -n optipilot-system logs deploy/optipilot-optipilot-controller -f
kubectl -n default get deploy -w
```

In dashboard/API:
- live RPS and metrics updates
- predictions and model status
- scaling decisions/audit trail
- replica changes before demand peaks (when service mode is autonomous and model is promoted)

## 10. Promote control mode (shadow → autonomous)

```bash
curl -X POST http://localhost:8080/api/services/api-gateway/mode \
  -H 'Content-Type: application/json' \
  -d '{"mode":"autonomous","triggered_by":"demo"}'
```

Repeat for `order-service` and `payment-service` as needed.

## 11. Drift + retrain demonstration

1. Send irregular/wild traffic bursts (run `spike.sh` repeatedly or custom high-rate patterns).
2. Watch mode/status degradation signals in logs/dashboard.
3. Trigger manual retrain:

```bash
curl -X POST http://localhost:8080/api/services/api-gateway/retrain
```

4. Verify model version change and audit updates.

## 12. Kill switch demonstration

```bash
curl -X POST http://localhost:8080/api/kill-switch \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true}'
```

Verify scaling actions pause, then re-enable:

```bash
curl -X POST http://localhost:8080/api/kill-switch \
  -H 'Content-Type: application/json' \
  -d '{"enabled":false}'
```

## 13. Final integration smoke checklist

1. Fresh Kind cluster created.
2. Helm install succeeds.
3. 3 test services deployed with `optipilot.io/enabled=true`.
4. Wait ~30 minutes for sufficient data and model progression.
5. Set services to `autonomous`.
6. Run `loadtest/scenarios/realistic.sh`.
7. Verify in dashboard:
   - predictions present
   - scaling decisions visible
   - replica counts respond proactively
8. Verify in cluster:
   - `kubectl get deploy -w` shows replica updates before spike peak.
9. Verify audit trail:
   - `GET /api/audit` includes full sequence.
10. Cleanup succeeds with uninstall.

## 14. Teardown

```bash
helm uninstall optipilot -n optipilot-system
kubectl delete -f deploy/manifests/test-services.yaml
kind delete cluster --name optipilot
```

