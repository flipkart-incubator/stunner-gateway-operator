###########
# Build the manager binary
# ponytail: FK internal — docker-hub mirror; bitnami/golang is Debian (apt, not apk)
FROM jfrog.fkinternal.com/docker-hub/amd64/golang:sha256__eef6a67266eeed3c86dd47fd01b32faa8bf0229eb83eb3d4d466e80391bd3820 AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY internal/ internal/
COPY pkg/config/ pkg/config/

# Build
COPY .git ./
COPY Makefile ./
RUN apt-get update && apt-get install -y --no-install-recommends git make bash \
    && rm -rf /var/lib/apt/lists/*

RUN export CGO_ENABLED=0 GOOS=linux GOARCH=amd64 && make build-bin

###########
# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM jfrog.fkinternal.com/appsec.local/distroless-static-debian:nonroot

WORKDIR /
COPY --from=builder /workspace/bin/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
