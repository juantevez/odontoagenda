# ── OdontoAgenda — Dockerfile multi-stage ───────────────────────
#
# Requiere: go mod vendor ejecutado localmente antes del build.
# El directorio vendor/ se copia al builder — sin acceso a internet.
#
# Build individual:
#   docker build --target iam          -t odontoagenda/iam .
#   docker build --target patient       -t odontoagenda/patient .
#   docker build --target professional  -t odontoagenda/professional .
#   docker build --target scheduling    -t odontoagenda/scheduling .
#
# O vía docker-compose:
#   docker compose build

# ── Stage 1: builder compartido ──────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copiar go.mod y go.sum primero (mejor cache de layers)
COPY go.mod go.sum ./

# Copiar el directorio vendor (generado con: go mod vendor)
# No se necesita acceso a internet — todas las deps están en vendor/
COPY vendor/ vendor/

# Código fuente completo del monorepo
COPY . .

# Compilar todos los binarios usando las deps del vendor/
# -mod=vendor: fuerza el uso de vendor/ en lugar de module cache
# -ldflags="-s -w": reduce tamaño eliminando debug info
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

# ── Stage 5: imagen mínima para Scheduling ───────────────────────
FROM gcr.io/distroless/static-debian12 AS scheduling

COPY --from=builder /bin/scheduling /scheduling

EXPOSE 8084
ENTRYPOINT ["/scheduling"]
