# ── OdontoAgenda — Dockerfile multi-stage ───────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -mod=vendor \
    -ldflags="-s -w" \
    -o /bin/iam \
    ./cmd/iam

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -mod=vendor \
    -ldflags="-s -w" \
    -o /bin/patient \
    ./cmd/patient

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -mod=vendor \
    -ldflags="-s -w" \
    -o /bin/professional \
    ./cmd/professional

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -mod=vendor \
    -ldflags="-s -w" \
    -o /bin/scheduling \
    ./cmd/scheduling

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -mod=vendor \
    -ldflags="-s -w" \
    -o /bin/notifications \
    ./cmd/notifications


RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -mod=vendor \
    -ldflags="-s -w" \
    -o /bin/coverage \
    ./cmd/coverage


RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -mod=vendor \
    -ldflags="-s -w" \
    -o /bin/billing \
    ./cmd/billing

# ── IAM ──────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS iam
COPY --from=builder /bin/iam /iam
EXPOSE 8081
ENTRYPOINT ["/iam"]

# ── Patient ───────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS patient
COPY --from=builder /bin/patient /patient
EXPOSE 8082
ENTRYPOINT ["/patient"]

# ── Professional ──────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS professional
COPY --from=builder /bin/professional /professional
EXPOSE 8083
ENTRYPOINT ["/professional"]

# ── Scheduling ────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS scheduling
COPY --from=builder /bin/scheduling /scheduling
EXPOSE 8084
ENTRYPOINT ["/scheduling"]

# ── Notifications ─────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS notifications
COPY --from=builder /bin/notifications /notifications
EXPOSE 8086
ENTRYPOINT ["/notifications"]

# ── Coverage ──────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS coverage
COPY --from=builder /bin/coverage /coverage
EXPOSE 8085
ENTRYPOINT ["/coverage"]

# ── Billing ───────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS billing
COPY --from=builder /bin/billing /billing
EXPOSE 8087
ENTRYPOINT ["/billing"]
