# --platform=$BUILDPLATFORM pins the builder to the native arch so the Go
# toolchain runs un-emulated; we cross-compile to the target arch below.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# buildx injects TARGETOS / TARGETARCH for each requested --platform; CGO is off
# so this cross-compiles natively (no QEMU on the Go build). For a plain
# `docker build` (no buildx) TARGETARCH is empty and Go falls back to the host
# arch, so single-arch local builds keep working.
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/shingan ./cmd/shingan \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/shingan-api ./cmd/api \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/shingan-runner ./cmd/runner \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/shingan-web ./cmd/shingan-web

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/shingan         /app/shingan
COPY --from=builder /out/shingan-api     /app/shingan-api
COPY --from=builder /out/shingan-runner  /app/shingan-runner
COPY --from=builder /out/shingan-web     /app/shingan-web

ENV PATH="/app:${PATH}"

# Default: CLI analyze mode. Override with docker run ... shingan-api
ENTRYPOINT ["/app/shingan"]
CMD ["--help"]

EXPOSE 8080
