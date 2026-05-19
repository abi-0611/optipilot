.PHONY: kind-up kind-down build-images build-image-controller build-image-forecaster build-image-services build-image-all load-images deploy undeploy logs-controller logs-forecaster restart test-load port-forward-controller port-forward-forecaster port-forward-prometheus port-forward-services

KIND_CLUSTER := optipilot

export DOCKER_BUILDKIT=1

build:
	mkdir -p bin/
	cd controller && go mod tidy
	go build -o bin/controller controller/main.go

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

build-image-controller:
	docker build -t optipilot-controller:latest -f Dockerfile.controller .

build-image-forecaster:
	docker build -t optipilot-forecaster:latest -f Dockerfile.forecaster .

build-image-services:
	cd services/api-gateway && docker build -t api-gateway:latest .
	cd services/order-service && docker build -t order-service:latest .
	cd services/payment-service && docker build -t payment-service:latest .

build-image-all: build-image-controller build-image-forecaster build-image-services

build-images: build-image-all

load-image-controller:
	kind load docker-image optipilot-controller:latest --name $(KIND_CLUSTER)

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

port-forward-controller:
	kubectl -n optipilot-system port-forward svc/optipilot-controller 8080:8080

port-forward-forecaster:
	kubectl -n optipilot-system port-forward svc/optipilot-forecaster 50051:50051

port-forward-prometheus:
	kubectl -n default port-forward svc/prometheus-kube-prometheus-prometheus 9090:9090

port-forward-services:
	kubectl -n default port-forward svc/api-gateway 8081:8081 & \
	kubectl -n default port-forward svc/order-service 8082:8082 & \
	kubectl -n default port-forward svc/payment-service 8083:8083 & \
	wait

restart:
	kubectl rollout restart deployment optipilot-controller -n optipilot-system
	kubectl rollout restart deployment optipilot-forecaster -n optipilot-system

test-load:
	@echo "Sending test load to api-gateway on kind worker node... (Requires vegeta)"
	@echo "Find the node ip and node port (or use port-forward)"
