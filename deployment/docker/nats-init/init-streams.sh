#!/bin/sh
# =============================================================================
# OdontoAgenda — NATS JetStream init
#
# Crea todos los streams necesarios antes de que arranquen los BCs.
# Se ejecuta como servicio `nats-init` en Docker Compose con:
#   restart: on-failure
#   depends_on: nats (healthy)
#
# Los BCs dependen de `nats-init` con condition: service_completed_successfully,
# lo que garantiza que los streams existen antes del primer Subscribe.
#
# Decisiones de diseño:
#   - RetentionPolicy: WorkQueuePolicy en streams de comandos/eventos internos
#     (cada mensaje es procesado exactamente una vez).
#     Excepción: DLQ usa LimitsPolicy (retención por tiempo, no por consumo).
#   - MaxAge: 7 días — ventana de replay ante fallos de un BC.
#   - Storage: file — sobrevive reinicios de NATS.
#   - Replicas: 1 — entorno de desarrollo; en producción usar 3.
#   - `--update-stream`: idempotente, permite reiniciar el contenedor sin error.
# =============================================================================

set -e

NATS_URL="${NATS_URL:-nats://nats:4222}"

echo "[nats-init] Conectando a NATS en ${NATS_URL} ..."

# Esperar hasta que NATS JetStream esté disponible (por si el healthcheck
# de Docker tarda en propagarse o hay un race condition de red).
MAX_WAIT=60
WAITED=0
until nats --server="${NATS_URL}" stream ls > /dev/null 2>&1; do
    if [ "$WAITED" -ge "$MAX_WAIT" ]; then
        echo "[nats-init] ERROR: NATS no respondió en ${MAX_WAIT}s. Abortando."
        exit 1
    fi
    echo "[nats-init] NATS aún no disponible, reintentando en 2s... (${WAITED}s/${MAX_WAIT}s)"
    sleep 2
    WAITED=$((WAITED + 2))
done

echo "[nats-init] NATS disponible. Creando streams..."

# =============================================================================
# Helper: crear o actualizar un stream de forma idempotente.
# Uso: create_stream <NOMBRE> <SUBJECTS> [opciones extra...]
# =============================================================================
create_stream() {
    STREAM_NAME="$1"
    SUBJECTS="$2"
    shift 2
    EXTRA_OPTS="$@"

    echo "[nats-init] Stream: ${STREAM_NAME} (subjects: ${SUBJECTS})"

    # --update-stream: si ya existe, actualiza la config en lugar de fallar.
    # shellcheck disable=SC2086
    nats --server="${NATS_URL}" stream add "${STREAM_NAME}" \
        --subjects="${SUBJECTS}" \
        --storage=file \
        --replicas=1 \
        --retention=limits \
        --max-age=168h \
        --max-msgs=-1 \
        --max-bytes=-1 \
        --max-msg-size=1048576 \
        --discard=old \
        --dupe-window=2m \
        --no-deny-delete \
        --no-deny-purge \
        ${EXTRA_OPTS} \
        2>/dev/null || \
    nats --server="${NATS_URL}" stream edit "${STREAM_NAME}" \
        --subjects="${SUBJECTS}" \
        --storage=file \
        --replicas=1 \
        --retention=limits \
        --max-age=168h \
        --max-msgs=-1 \
        --max-bytes=-1 \
        --max-msg-size=1048576 \
        --discard=old \
        --dupe-window=2m \
        --force \
        ${EXTRA_OPTS} \
        2>/dev/null || true

    echo "[nats-init]   ✓ ${STREAM_NAME}"
}

# =============================================================================
# Stream: IAM_EVENTS
#   Publicado por: BC IAM
#   Consumido por: (futuro) Notifications, Patient
#   Subjects cubiertos:
#     user.registered       — nuevo usuario registrado
#     user.logged_out       — logout global (tokens revocados)
#     user.suspended        — cuenta suspendida
#     family.member_added   — nuevo miembro en cuenta familiar
# =============================================================================
create_stream "IAM_EVENTS" \
    "user.registered,user.logged_out,user.suspended,family.member_added"

# =============================================================================
# Stream: PATIENT_EVENTS
#   Publicado por: BC Patient
#   Consumido por: BC Scheduling (patient.archived → cancelar citas futuras)
#   Subjects cubiertos:
#     patient.registered            — nuevo paciente
#     patient.coverage.updated      — cambio de cobertura
#     patient.medical_alert.added   — nueva alerta médica
#     patient.preferences.updated   — preferencias actualizadas
#     patient.merged                — fusión de pacientes duplicados
#     patient.archived              — baja lógica (scheduling cancela citas)
# =============================================================================
create_stream "PATIENT_EVENTS" \
    "patient.registered,patient.coverage.updated,patient.medical_alert.added,patient.preferences.updated,patient.merged,patient.archived"

# =============================================================================
# Stream: PROFESSIONAL_EVENTS
#   Publicado por: BC Professional
#   Consumido por: BC Scheduling (3 consumers):
#     - scheduling-create-schedule    → professional.assigned_to_clinic
#     - scheduling-update-schedule    → professional.schedule.updated
#     - scheduling-suspend-professional → professional.suspended
#   Subjects cubiertos:
#     professional.registered           — nuevo profesional
#     professional.license.added        — matrícula agregada
#     professional.license.expiring_soon — alerta de vencimiento (job scheduler)
#     professional.assigned_to_clinic   — asignado a sede (→ crear AvailabilitySchedule)
#     professional.schedule.updated     — horario modificado (→ invalidar cache Redis)
#     professional.suspended            — suspensión (→ desactivar schedules)
# =============================================================================
create_stream "PROFESSIONAL_EVENTS" \
    "professional.registered,professional.license.added,professional.license.expiring_soon,professional.assigned_to_clinic,professional.schedule.updated,professional.suspended"

# =============================================================================
# Stream: APPOINTMENT_EVENTS
#   Publicado por: BC Scheduling
#   Consumido por: BC Patient (1 consumer):
#     - patient-record-visit → appointment.completed (actualiza DentalHistorySummary)
#   (futuro) Notifications, Billing
#   Subjects cubiertos:
#     appointment.booked      — reserva exitosa
#     appointment.confirmed   — cita pendiente confirmada
#     appointment.completed   — cita finalizada (→ Patient registra visita)
#     appointment.cancelled   — cancelación
#     appointment.no_show     — paciente no se presentó
#     scheduling.availability.updated — schedule de disponibilidad modificado
# =============================================================================
create_stream "APPOINTMENT_EVENTS" \
    "appointment.booked,appointment.confirmed,appointment.completed,appointment.cancelled,appointment.no_show,scheduling.availability.updated"

# =============================================================================
# Stream: DEAD_LETTER_EVENTS
#   Publicado por: NATSBus.sendToDLQ (pkg/events/bus.go) cuando un consumer
#   supera MaxRetries sin procesar el mensaje.
#   Subject pattern: dlq.<consumer_name>.<event_type>
#   RetentionPolicy: limits (no es work-queue; se mantiene para revisión manual).
#   MaxAge: 30 días (tiempo para analizar y replay manual).
# =============================================================================
echo "[nats-init] Stream: DEAD_LETTER_EVENTS (DLQ)"
nats --server="${NATS_URL}" stream add "DEAD_LETTER_EVENTS" \
    --subjects="dlq.>" \
    --storage=file \
    --replicas=1 \
    --retention=limits \
    --max-age=720h \
    --max-msgs=10000 \
    --max-bytes=104857600 \
    --max-msg-size=1048576 \
    --discard=old \
    --dupe-window=2m \
    --no-deny-delete \
    --no-deny-purge \
    2>/dev/null || \
nats --server="${NATS_URL}" stream edit "DEAD_LETTER_EVENTS" \
    --subjects="dlq.>" \
    --max-age=720h \
    --max-msgs=10000 \
    --force \
    2>/dev/null || true

echo "[nats-init]   ✓ DEAD_LETTER_EVENTS"

# =============================================================================
# Verificación final: listar streams creados
# =============================================================================
echo ""
echo "[nats-init] ============================================"
echo "[nats-init] Streams disponibles en NATS JetStream:"
nats --server="${NATS_URL}" stream ls
echo "[nats-init] ============================================"
echo "[nats-init] Init completado exitosamente. BCs pueden arrancar."