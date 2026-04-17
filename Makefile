.PHONY: kind-up kind-down build-images load-images deploy undeploy logs-controller logs-forecaster restart test-load

KIND_CLUSTER := optipilot

kind-up:
	kind create cluster --name $(KIND_CLUSTER) --config deploy/kind-config.yaml
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo update
	helm install prometheus kube-prometheus-stack \
		--repo https://prometheus-community.github.io/helm-charts \
		--namespace default \
		--values deploy/prometheus/values.yaml

kind-down:
	kind delete cluster --name $(KIND_CLUSTER)

build-images:
	docker build -t optipilot-controller:latest -f Dockerfile.controller .
	docker build -t optipilot-forecaster:latest -f Dockerfile.forecaster .
	cd services/api-gateway && docker build -t api-gateway:latest .
	cd services/order-service && docker build -t order-service:latest .
	cd services/payment-service && docker build -t payment-service:latest .

load-images:
	kind load docker-image optipilot-controller:latest optipilot-forecaster:latest api-gateway:latest order-service:latest payment-service:latest --name $(KIND_CLUSTER)

deploy:
	kubectl apply -f deploy/manifests/namespace.yaml
	kubectl apply -f deploy/manifests/rbac.yaml
	kubectl apply -f deploy/manifests/pvc.yaml
	kubectl apply -f deploy/manifests/configmap.yaml
	kubectl apply -f deploy/manifests/controller-deployment.yaml
	kubectl apply -f deploy/manifests/forecaster-deployment.yaml
	kubectl apply -f deploy/manifests/test-services.yaml

undeploy:
	kubectl delete -f deploy/manifests/test-services.yaml || true
	kubectl delete -f deploy/manifests/controller-deployment.yaml || true
	kubectl delete -f deploy/manifests/forecaster-deployment.yaml || true
	kubectl delete -f deploy/manifests/configmap.yaml || true
	kubectl delete -f deploy/manifests/pvc.yaml || true
	kubectl delete -f deploy/manifests/rbac.yaml || true
	kubectl delete -f deploy/manifests/namespace.yaml || true

logs-controller:
	kubectl logs -n optipilot-system deployment/optipilot-controller -f

logs-forecaster:
	kubectl logs -n optipilot-system deployment/optipilot-forecaster -f

restart:
	kubectl rollout restart deployment optipilot-controller -n optipilot-system
	kubectl rollout restart deployment optipilot-forecaster -n optipilot-system

test-load:
	@echo "Sending test load to api-gateway on kind worker node... (Requires vegeta)"
	@echo "Find the node ip and node port (or use port-forward)"
