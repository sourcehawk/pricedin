# Build context: server/

FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -o /out/pricedin ./cmd/pricedin
# The folder of the filesystem blob store. A new named volume mounted here
# copies its owner, so the nonroot user can write to it.
RUN mkdir -p /out/data/blob

# The fixture server for Compose and the end-to-end tests: `--target
# fixtureserver`. It serves the testdata folder, which Compose mounts at
# /testdata. It must come before the last stage, which is the default
# target.
FROM build AS build-fixtureserver
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -o /out/fixtureserver ./cmd/fixtureserver

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99 AS fixtureserver
COPY --from=build-fixtureserver /out/fixtureserver /fixtureserver
USER nonroot:nonroot
EXPOSE 8090
ENTRYPOINT ["/fixtureserver"]
CMD ["-addr", ":8090", "-root", "/testdata"]

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
COPY --from=build /out/pricedin /pricedin
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot:nonroot
EXPOSE 8080 9090
ENTRYPOINT ["/pricedin"]
CMD ["serve"]
