BINARY_DIR ?= bin
FINDING_NAMESPACE ?= yardmaster-system
IMG ?= yardmaster:dev
KIND_CLUSTER ?= yardmaster
YARDMASTER ?= $(BINARY_DIR)/yardmaster
KUBECTL_PLUGIN ?= $(BINARY_DIR)/kubectl-yardmaster
DASHBOARD ?= $(BINARY_DIR)/yardmaster-dashboard
CONTROLLER_TOOLS_VERSION ?= v0.18.0
CONTROLLER_GEN ?= go run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: generate
generate:
	$(CONTROLLER_GEN) object paths="./api/..."

.PHONY: manifests
manifests:
	$(CONTROLLER_GEN) crd:maxDescLen=0 paths="./api/..." output:crd:artifacts:config=config/crd

.PHONY: build
build:
	mkdir -p $(BINARY_DIR)
	go build -o $(YARDMASTER) ./cmd/yardmaster
	go build -o $(KUBECTL_PLUGIN) ./cmd/kubectl-yardmaster
	go build -o $(DASHBOARD) ./cmd/yardmaster-dashboard

.PHONY: docker-build
docker-build:
	docker build -t $(IMG) .

.PHONY: test
test:
	go test ./...

.PHONY: fmt
fmt:
	gofmt -w $$(find . -name '*.go')

.PHONY: install
install:
	kubectl apply -k config/crd
	kubectl apply -k config/rbac

.PHONY: deploy
deploy: install
	kubectl apply -k config/manager
	kubectl set image deployment/yardmaster manager=$(IMG) -n $(FINDING_NAMESPACE)
	kubectl apply -k config/dashboard
	kubectl set image deployment/yardmaster-dashboard dashboard=$(IMG) -n $(FINDING_NAMESPACE)

.PHONY: undeploy
undeploy:
	kubectl delete -k config/dashboard --ignore-not-found
	kubectl delete -k config/manager --ignore-not-found

.PHONY: sample
sample: ensure-kind-context
	kubectl label node --all karpenter.sh/nodepool=kind-general --overwrite
	kubectl apply -k config/samples

.PHONY: report
report:
	go run ./cmd/kubectl-yardmaster --finding-namespace=$(FINDING_NAMESPACE) report

.PHONY: dashboard
dashboard:
	go run ./cmd/yardmaster-dashboard --finding-namespace=$(FINDING_NAMESPACE) --addr=:8088

.PHONY: dashboard-port-forward
dashboard-port-forward:
	kubectl -n $(FINDING_NAMESPACE) port-forward svc/yardmaster-dashboard 8088:8088

.PHONY: run
run:
	go run ./cmd/yardmaster --finding-namespace=$(FINDING_NAMESPACE)

.PHONY: kind-up
kind-up:
	kind get clusters | grep -qx "$(KIND_CLUSTER)" || kind create cluster --name "$(KIND_CLUSTER)"

.PHONY: kind-context
kind-context: kind-up
	kubectl config use-context kind-$(KIND_CLUSTER)

.PHONY: ensure-kind-context
ensure-kind-context:
	@test "$$(kubectl config current-context)" = "kind-$(KIND_CLUSTER)" || \
		(echo "Refusing to run sample target outside kind-$(KIND_CLUSTER). Current context: $$(kubectl config current-context)" >&2; exit 1)

.PHONY: kind-load
kind-load:
	kind load docker-image $(IMG) --name "$(KIND_CLUSTER)"

.PHONY: demo-kind
demo-kind: kind-context docker-build kind-load deploy
	kubectl -n $(FINDING_NAMESPACE) rollout status deployment/yardmaster --timeout=90s
	kubectl -n $(FINDING_NAMESPACE) rollout status deployment/yardmaster-dashboard --timeout=90s
	$(MAKE) sample
	$(MAKE) report

.PHONY: smoke-kind
smoke-kind: kind-context install
	kubectl delete -k config/samples --ignore-not-found
	kubectl delete dispatchfindings.yardmaster.dev --all -n $(FINDING_NAMESPACE) --ignore-not-found
	go run ./cmd/yardmaster --finding-namespace=$(FINDING_NAMESPACE) >/tmp/yardmaster-smoke.log 2>&1 & \
		controller_pid=$$!; \
		trap 'kill $$controller_pid >/dev/null 2>&1 || true' EXIT; \
		sleep 5; \
		$(MAKE) sample; \
		sleep 10; \
		$(MAKE) report
