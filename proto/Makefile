PROTO_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
REPO_ROOT := $(abspath $(PROTO_ROOT)/..)
PROTO_FILES := $(shell find $(PROTO_ROOT) -name '*.proto')

GO_OUT := $(REPO_ROOT)/gen/go
PY_OUT := $(REPO_ROOT)/gen/python

.PHONY: all go python clean install-plugins

all: go python

go:
	@mkdir -p $(GO_OUT)
	protoc \
		--proto_path=$(PROTO_ROOT) \
		--go_out=$(GO_OUT) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(GO_OUT) \
		--go-grpc_opt=paths=source_relative \
		$(PROTO_FILES)

python:
	@mkdir -p $(PY_OUT)
	python -m grpc_tools.protoc \
		--proto_path=$(PROTO_ROOT) \
		--python_out=$(PY_OUT) \
		--grpc_python_out=$(PY_OUT) \
		--pyi_out=$(PY_OUT) \
		$(PROTO_FILES)
	@touch $(PY_OUT)/optipilot/__init__.py
	@touch $(PY_OUT)/optipilot/v1/__init__.py

clean:
	rm -rf $(GO_OUT) $(PY_OUT)

install-plugins:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	pip install grpcio grpcio-tools
