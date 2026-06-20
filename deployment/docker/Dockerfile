# ── OdontoAgenda — Dockerfile multi-stage ───────────────────────
#
# Un target por Bounded Context. Cada servicio se construye
# desde el mismo monorepo pero produce un binario independiente.
#
# Build:
#   docker build --target iam         -t odontoagenda/iam .
#   docker build --target patient      -t odontoagenda/patient .
#   docker build --target professional -t odontoagenda/professional .
#
# O vía docker-compose (usa el target definido en cada servicio).

# ── Stage 1: builder compartido ──────────────────────────────────
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Dependencias primero (cache de layers)
COPY go.mod go.sum ./
RUN go mod download

# Código fuente completo del monorepo
COPY . .

# Compilar todos los binarios en paralelo.
# El flag -ldflags="-s -w" reduce el tamaño eliminando debug info.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /bin/iam \
    ./cmd/iam

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /bin/patient \
    ./cmd/patient

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /bin/professional \
    ./cmd/professional

# ── Stage 2: imagen mínima para IAM ──────────────────────────────
FROM gcr.io/distroless/static-debian12 AS iam

COPY --from=builder /bin/iam /iam

EXPOSE 8081
ENTRYPOINT ["/iam"]

# ── Stage 3: imagen mínima para Patient ──────────────────────────
FROM gcr.io/distroless/static-debian12 AS patient

COPY --from=builder /bin/patient /patient

EXPOSE 8082
ENTRYPOINT ["/patient"]

# ── Stage 4: imagen mínima para Professional ─────────────────────
FROM gcr.io/distroless/static-debian12 AS professional

COPY --from=builder /bin/professional /professional

EXPOSE 8083
ENTRYPOINT ["/professional"]
