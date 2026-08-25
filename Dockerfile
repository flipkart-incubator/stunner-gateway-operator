###########
# Build the manager binary — FK internal bases + go proxy (no proxy.golang.org TLS)
FROM --platform=linux/amd64 jfrog.fkinternal.com/fk-base-images/golang:1.26.0-debian13.3 AS builder

ENV GOPROXY=https://artifactory.artifactory-prod.fkcloud.in/artifactory/api/go/go_virtual \
    GOSUMDB=off

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

COPY main.go main.go
COPY api/ api/
COPY internal/ internal/
COPY pkg/config/ pkg/config/

COPY .git ./
COPY Makefile ./
RUN apt-get update && apt-get install -y --no-install-recommends git make bash \
    && rm -rf /var/lib/apt/lists/*

RUN export CGO_ENABLED=0 GOOS=linux GOARCH=amd64 && make build-bin

###########
FROM --platform=linux/amd64 jfrog.fkinternal.com/appsec.local/distroless-static-debian:nonroot

WORKDIR /
COPY --from=builder /workspace/bin/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
