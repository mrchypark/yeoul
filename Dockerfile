FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/yeould ./cmd/yeould
COPY internal ./internal
COPY pkg ./pkg
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/yeould ./cmd/yeould \
    && mkdir -p /out/data \
    && chown 65532:65532 /out/data

FROM scratch
LABEL org.opencontainers.image.title="Yeoul" \
      org.opencontainers.image.description="Yeoul HTTP service" \
      org.opencontainers.image.source="https://github.com/mrchypark/yeoul" \
      org.opencontainers.image.licenses="NOASSERTION"
COPY --from=build --chown=65532:65532 /out/yeould /yeould
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["/yeould"]
CMD ["--db", "/data/yeoul.ltdb", "--listen", "0.0.0.0:8080"]
