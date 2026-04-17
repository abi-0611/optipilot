# OptiPilot Kubernetes Deployment

This directory contains manifests and configurations to deploy OptiPilot in a local [kind](https://kind.sigs.k8s.io/) cluster.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/docs/intro/install/)
- `make` (typically pre-installed on Linux/macOS)

## Quick Start

Run these commands from the repository root:

1. **Create the cluster and install Prometheus**:
   ```bash
   make kind-up
   ```

2. **Build the container images**:
   ```bash
   make build-images
   ```

3. **Load the images into the kind cluster**:
   ```bash
   make load-images
   ```

4. **Deploy OptiPilot and test services**:
   ```bash
   make deploy
   ```

## Verification

Check that all pods are running (may take a couple of minutes):
```bash
kubectl get pods -A
```

View the controller logs to ensure it discovered the test services and connected to the forecaster:
```bash
make logs-controller
```

## Accessing the Dashboard

The dashboard is accessible via a NodePort on `30080`. Since `kind` extra port mappings are configured, you can directly access it via your host:
```bash
http://localhost:30080
```

Alternatively, use `kubectl port-forward`:
```bash
kubectl port-forward svc/optipilot-controller 8080:8080 -n optipilot-system
```
Then visit `http://localhost:8080`.

## Testing Autonomous Scaling

The test services (`api-gateway`, `order-service`, `payment-service`) are deployed with labels:
```yaml
optipilot.io/enabled: "true"
optipilot.io/mode: "autonomous"
optipilot.io/min-replicas: "2"
optipilot.io/max-replicas: "10"
```

To see scaling in action, use a load tester like `vegeta` or `hey` against the test services.

## Teardown

To clean up all deployed manifests:
```bash
make undeploy
```

To destroy the local kind cluster completely:
```bash
make kind-down
```
