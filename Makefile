BINARY_DIR ?= bin
YARDMASTER ?= $(BINARY_DIR)/yardmaster
KUBECTL_PLUGIN ?= $(BINARY_DIR)/kubectl-yardmaster

.PHONY: build
build:
	mkdir -p $(BINARY_DIR)
	go build -o $(YARDMASTER) ./cmd/yardmaster
	go build -o $(KUBECTL_PLUGIN) ./cmd/kubectl-yardmaster

.PHONY: test
test:
	go test ./...

.PHONY: fmt
fmt:
	gofmt -w $$(find . -name '*.go')

.PHONY: install
install:
	kubectl apply -f config/crd
	kubectl apply -f config/rbac

.PHONY: run
run:
	go run ./cmd/yardmaster
