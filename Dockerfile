# Build stage: runs on the build machine's own platform and cross-compiles for
# the target, so multi-arch builds don't need emulation.
FROM --platform=$BUILDPLATFORM golang:1.22 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Download modules first so source changes don't invalidate this layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o manager ./cmd

# Runtime stage: just the static binary on distroless, running as non-root.
FROM gcr.io/distroless/static:nonroot
LABEL org.opencontainers.image.title="k8s-fm" \
      org.opencontainers.image.description="Kubernetes operator that manages Spotify playlists"
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
