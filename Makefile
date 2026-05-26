BINARY_DIR ?= bin
FINDING_NAMESPACE ?= yardmaster-system
KIND_CLUSTER ?= yardmaster
YARDMASTER ?= $(BINARY_DIR)/yardmaster
KUBECTL_PLUGIN ?= $(BINARY_DIR)/kubectl-yardmaster
DASHBOARD ?= $(BINARY_DIR)/yardmaster-dashboard

.PHONY: build
build:
	mkdir -p $(BINARY_DIR)
	go build -o $(YARDMASTER) ./cmd/yardmaster
	go build -o $(KUBECTL_PLUGIN) ./cmd/kubectl-yardmaster
	go build -o $(DASHBOARD) ./cmd/yardmaster-dashboard

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

.PHONY: sample
sample:
	kubectl label node --all karpenter.sh/nodepool=kind-general --overwrite
	kubectl apply -k config/samples

.PHONY: report
report:
	go run ./cmd/kubectl-yardmaster --finding-namespace=$(FINDING_NAMESPACE) report

.PHONY: dashboard
dashboard:
	go run ./cmd/yardmaster-dashboard --finding-namespace=$(FINDING_NAMESPACE) --addr=:8088

.PHONY: run
run:
	go run ./cmd/yardmaster --finding-namespace=$(FINDING_NAMESPACE)

.PHONY: kind-up
kind-up:
	kind get clusters | grep -qx "$(KIND_CLUSTER)" || kind create cluster --name "$(KIND_CLUSTER)"

.PHONY: smoke-kind
smoke-kind: kind-up install
	kubectl delete -k config/samples --ignore-not-found
	kubectl delete dispatchfindings.yardmaster.dev --all -n $(FINDING_NAMESPACE) --ignore-not-found
	go run ./cmd/yardmaster --finding-namespace=$(FINDING_NAMESPACE) >/tmp/yardmaster-smoke.log 2>&1 & \
		controller_pid=$$!; \
		trap 'kill $$controller_pid >/dev/null 2>&1 || true' EXIT; \
		sleep 5; \
		$(MAKE) sample; \
		sleep 10; \
		$(MAKE) report
