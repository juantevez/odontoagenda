# OdontoAgenda — Monorepo

Sistema integral de reservas odontológicas multi-especialidad y multi-sucursal.
Stack: **Go 1.23 · PostgreSQL 16 + PostGIS · NATS JetStream**
Arquitectura: **DDD + Hexagonal · Multi-binario · Un proceso por Bounded Context**

---

## Estructura del Monorepo

```
.
├── cmd/                          # Entry points — un binario por Bounded Context
│   ├── iam/
│   │   ├── main.go               # Carga config → llama wire → inicia HTTP → graceful shutdown
│   │   └── wire.go               # DI manual: ensambla repos + domain services + handlers
│   ├── patient/
│   │   ├── main.go
│   │   └── wire.go               # Incluye registro de NATS subscribers
│   ├── professional/
│   │   ├── main.go
│   │   └── wire.go
│   └── scheduling/               # (próximo)
│
├── context/                      # Bounded Contexts DDD
│   ├── iam/                      # Identity & Access Management
│   ├── patient/                  # Patient Management
│   ├── professional/             # Professional Management
│   └── ...                       # (scheduling, coverage, catalog, notifications, billing)
│
│   # Cada contexto sigue la misma estructura interna:
│   └── <bc>/
│       ├── domain/
│       │   ├── aggregate/        # Aggregates + Entities (sin imports de infra)
│       │   ├── event/            # Domain Events
│       │   ├── repository/       # Puertos de salida (interfaces)
│       │   ├── service/          # Domain Services
│       │   └── valueobject/      # Value Objects del contexto
│       ├── application/
│       │   ├── command/          # Command Handlers (casos de uso de escritura)
│       │   └── query/            # Query Handlers (casos de uso de lectura)
│       └── infrastructure/
│           ├── http/             # Handlers REST (adaptador de entrada)
│           ├── postgres/         # Repositorios PostgreSQL (adaptador de salida)
│           └── nats/             # Publishers/Subscribers NATS (entrada async)
│
├── pkg/                          # Shared Kernel — importable por todos los contextos
│   ├── events/                   # Event Bus (NATS JetStream + retry + DLQ + OTel)
│   ├── geospatial/               # Helpers PostGIS
│   ├── middleware/               # JWT, RBAC, RequestID, Logger, Recoverer
│   └── shared/
│       ├── errors/               # DomainError tipado → HTTP status
│       ├── types/                # IDs tipados, PagedResult[T], Result[T]
│       └── valueobject/          # Email, Phone, Address, Money, NationalID
│
├── migrations/                   # SQL por Bounded Context (golang-migrate)
│   ├── iam/
│   ├── patient/
│   └── professional/
│
├── deployment/
│   └── docker/
│       ├── Dockerfile            # Multi-stage: un target por BC (distroless)
│       └── docker-compose.yml    # Infra + un contenedor por servicio
│
└── go.mod
```

---

## Patrón de cada `cmd/<bc>/`

```
main.go   → carga config → llama initApp() → inicia HTTP → graceful shutdown
wire.go   → initApp(): construye el árbol de dependencias completo
              1. Infraestructura  (pgxpool, NATSBus)
              2. Repositorios     (adaptadores de salida)
              3. Domain Services
              4. Command Handlers
              5. Query Handlers
              6. NATS Subscribers (si el BC los tiene)
              7. HTTP Router + Server
```

`main.go` no conoce repositorios ni handlers — es un orquestador de ciclo de vida.
`wire.go` es el único lugar donde aparecen los tipos concretos de infraestructura.

---

## Puertos HTTP por servicio

| Servicio       | Puerto | Descripción                        |
|----------------|--------|------------------------------------|
| IAM            | 8081   | Registro, login, tokens            |
| Patient        | 8082   | Pacientes, coberturas, alertas     |
| Professional   | 8083   | Profesionales, matrículas, agenda  |
| Scheduling     | 8084   | Reservas, disponibilidad (próximo) |
| Coverage       | 8085   | Convenios y prepagas               |
| Notifications  | 8086   | Recordatorios y confirmaciones     |
| Billing        | 8087   | Pagos y facturación                |

---

## Reglas de arquitectura

**Dependencias (Hexagonal):** siempre hacia adentro
```
infrastructure → application → domain
pkg/*          → (no depende de context/*)
context/A      → NO importa context/B   (solo via NATS events)
```

**Comunicación entre BCs:** exclusivamente por Domain Events en NATS JetStream.
Ningún contexto llama directamente al repositorio de otro.

---

## Setup local

```bash
# 1. Variables de entorno (mínimo requerido)
export JWT_SECRET="un-secreto-de-al-menos-32-caracteres-local"

# 2. Levantar infraestructura
cd deployment/docker
docker compose up -d postgres nats redis

# 3. Correr los servicios (cada uno en una terminal)
go run ./cmd/iam
go run ./cmd/patient
go run ./cmd/professional

# 4. Health checks
curl http://localhost:8081/health
curl http://localhost:8082/health
curl http://localhost:8083/health
```

## Build Docker (por servicio)

```bash
docker build --target iam         -t odontoagenda/iam:latest .
docker build --target patient      -t odontoagenda/patient:latest .
docker build --target professional -t odontoagenda/professional:latest .

# O todos juntos con compose:
docker compose up -d
```

## Variables de entorno comunes

| Variable      | Default                                                     | Descripción                        |
|---------------|-------------------------------------------------------------|------------------------------------|
| `JWT_SECRET`  | *(requerido)*                                               | Clave HMAC-SHA256 ≥32 chars        |
| `JWT_ISSUER`  | `odontoagenda.iam`                                          | Claim `iss` del JWT                |
| `DATABASE_URL`| `postgres://odontoagenda:odontoagenda@localhost:5432/...`  | Conexión PostgreSQL                |
| `NATS_URL`    | `nats://localhost:4222`                                     | URL del servidor NATS              |
| `PORT`        | Varía por servicio (8081/8082/8083/...)                     | Puerto HTTP del servicio           |
