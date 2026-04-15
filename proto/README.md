# OptiPilot Proto

Shared gRPC contract between the Go actuator (client) and Python ML forecaster (server).

## Layout

```
proto/
  optipilot/v1/prediction.proto    # service + message definitions
  Makefile                         # codegen targets
gen/
  go/optipilot/v1/                 # generated Go stubs
  python/optipilot/v1/             # generated Python stubs
```

## Prerequisites

Install `protoc` (already present on this machine at `/usr/bin/protoc`). Then install the language plugins:

```bash
# Go plugins (require Go toolchain; binaries land in $(go env GOPATH)/bin)
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
export PATH="$PATH:$(go env GOPATH)/bin"

# Python plugin
pip install grpcio grpcio-tools
```

Or use the Makefile shortcut:

```bash
make -C proto install-plugins
```

## Regenerate stubs

```bash
make -C proto all        # both languages
make -C proto go         # Go only
make -C proto python     # Python only
make -C proto clean      # wipe gen/
```

Equivalent raw commands:

```bash
# Go
protoc \
  --proto_path=proto \
  --go_out=gen/go --go_opt=paths=source_relative \
  --go-grpc_out=gen/go --go-grpc_opt=paths=source_relative \
  proto/optipilot/v1/prediction.proto

# Python
python -m grpc_tools.protoc \
  --proto_path=proto \
  --python_out=gen/python \
  --grpc_python_out=gen/python \
  --pyi_out=gen/python \
  proto/optipilot/v1/prediction.proto
```

## Importing the stubs

**Go** — consume from `github.com/optipilot/proto/gen/go/optipilot/v1` (package alias `optipilotv1`).

**Python** — add `gen/python` to `sys.path` (or install as a local package), then:

```python
from optipilot.v1 import prediction_pb2, prediction_pb2_grpc
```
