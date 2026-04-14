FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/shingan ./cmd/shingan \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/shingan-api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/shingan-runner ./cmd/runner \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/shingan-web ./cmd/shingan-web

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
