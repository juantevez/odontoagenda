-- ============================================================
-- Migración: 001_create_scheduling_schema.sql
-- Bounded Context: Scheduling
-- Schema: scheduling
-- ============================================================

CREATE SCHEMA IF NOT EXISTS scheduling;

-- ── Tabla de citas ────────────────────────────────────────────────
-- Aggregate Root: Appointment
-- UNIQUE constraint (professional_id, clinic_id, slot_start) WHERE status activo
-- es la última línea de defensa contra doble reserva (después de Redis lock).
CREATE TABLE scheduling.appointments (
    id               UUID         NOT NULL DEFAULT gen_random_uuid(),

    -- Actores
    patient_id       UUID         NOT NULL,   -- cross-context, sin FK
    booked_by_id     UUID         NOT NULL,   -- quien hizo la reserva (puede ser guardian)
    professional_id  UUID         NOT NULL,   -- cross-context, sin FK
    clinic_id        UUID         NOT NULL,   -- cross-context, sin FK
    procedure_code   VARCHAR(50)  NOT NULL,   -- cross-context ref a Treatment Catalog

    -- Slot de tiempo
    slot_start       TIMESTAMPTZ  NOT NULL,
    slot_end         TIMESTAMPTZ  NOT NULL,

    -- Estado
    status           VARCHAR(20)  NOT NULL DEFAULT 'Confirmed',

    -- Cobertura (capturada al momento de la reserva)
    coverage_type    VARCHAR(30),
    agreement_id     UUID,
    requires_auth_id VARCHAR(100),            -- código de autorización de prepaga

    -- Finalización
    clinical_notes   TEXT,

    -- Cancelación
    cancellation_reason   VARCHAR(50),
    cancellation_note     TEXT,
    cancelled_at          TIMESTAMPTZ,
    cancelled_by_user_id  UUID,
    is_late_cancellation  BOOLEAN NOT NULL DEFAULT false,

    -- Auditoría y optimistic locking
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by       UUID         NOT NULL,
    version          BIGINT       NOT NULL DEFAULT 1,

    CONSTRAINT pk_appointments PRIMARY KEY (id),
    CONSTRAINT chk_appointment_status CHECK (
        status IN ('Pending', 'Confirmed', 'InProgress', 'Completed', 'Cancelled', 'NoShow')
    ),
    CONSTRAINT chk_slot_order CHECK (slot_end > slot_start),
    CONSTRAINT chk_slot_duration CHECK (
        EXTRACT(EPOCH FROM (slot_end - slot_start)) >= 300  -- mínimo 5 minutos
    )
);

-- UNIQUE parcial: previene doble reserva a nivel de BD.
-- El UNIQUE solo aplica a estados activos; permite múltiples cancelaciones del mismo slot.
CREATE UNIQUE INDEX uq_appointments_no_double_booking
    ON scheduling.appointments (professional_id, clinic_id, slot_start)
    WHERE status IN ('Pending', 'Confirmed', 'InProgress');

-- Índices de acceso frecuente
CREATE INDEX idx_appointments_patient     ON scheduling.appointments (patient_id, status);
CREATE INDEX idx_appointments_professional ON scheduling.appointments (professional_id, clinic_id, slot_start);
CREATE INDEX idx_appointments_clinic_date  ON scheduling.appointments (clinic_id, slot_start);
CREATE INDEX idx_appointments_status       ON scheduling.appointments (status) WHERE status IN ('Pending', 'Confirmed', 'InProgress');

-- Índice para countActiveByPatient (query frecuente en validación de límite)
CREATE INDEX idx_appointments_patient_active
    ON scheduling.appointments (patient_id)
    WHERE status IN ('Pending', 'Confirmed', 'InProgress');

-- ── AvailabilitySchedules ─────────────────────────────────────────
-- Aggregate Root: AvailabilitySchedule
-- Proyección de disponibilidad por (professional_id, clinic_id).
-- Se sincroniza vía eventos del contexto Professional.
CREATE TABLE scheduling.availability_schedules (
    id               UUID         NOT NULL DEFAULT gen_random_uuid(),
    professional_id  UUID         NOT NULL,
    clinic_id        UUID         NOT NULL,

    -- Horario recurrente semanal (array JSONB)
    -- [{ "weekday": 1, "start_hour": 9, "start_min": 0, "end_hour": 13, "end_min": 0 }]
    working_hours    JSONB        NOT NULL DEFAULT '[]'::jsonb,

    -- Días de excepción (vacaciones, feriados, horarios especiales)
    -- [{ "date": "2026-07-09", "is_working": false, "reason": "Feriado" }]
    exception_days   JSONB        NOT NULL DEFAULT '[]'::jsonb,

    -- Slots bloqueados manualmente
    -- [{ "id": "uuid", "slot": {...}, "reason": "vacation", "note": "..." }]
    blocked_slots    JSONB        NOT NULL DEFAULT '[]'::jsonb,

    -- Proyección de appointments activos sobre la agenda.
    -- Se mantiene sincronizado al confirmar/cancelar appointments.
    -- [{ "appointment_id": "uuid", "slot": {...}, "patient_id": "uuid", "status": "..." }]
    booked_slots     JSONB        NOT NULL DEFAULT '[]'::jsonb,

    -- Duraciones por procedimiento: { "ORTODONCIA": 60, "ODONTOLOGIA_GENERAL": 30 }
    procedure_durations JSONB     NOT NULL DEFAULT '{}'::jsonb,

    is_active        BOOLEAN      NOT NULL DEFAULT true,
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    version          BIGINT       NOT NULL DEFAULT 1,

    CONSTRAINT pk_availability_schedules PRIMARY KEY (id)
);

-- Clave de negocio única: un schedule por (professional, clinic)
CREATE UNIQUE INDEX uq_schedule_professional_clinic
    ON scheduling.availability_schedules (professional_id, clinic_id)
    WHERE is_active = true;

CREATE INDEX idx_schedule_clinic    ON scheduling.availability_schedules (clinic_id) WHERE is_active = true;
CREATE INDEX idx_schedule_prof      ON scheduling.availability_schedules (professional_id);
CREATE INDEX idx_schedule_booked_gin ON scheduling.availability_schedules USING GIN (booked_slots);

-- ── Comentarios ───────────────────────────────────────────────────
COMMENT ON TABLE scheduling.appointments IS 'Aggregate Appointment. Tiene UNIQUE constraint parcial para prevenir doble reserva.';
COMMENT ON COLUMN scheduling.appointments.version IS 'Optimistic locking. El UNIQUE constraint + version previenen doble reserva incluso bajo alta concurrencia.';
COMMENT ON COLUMN scheduling.appointments.booked_by_id IS 'Puede diferir de patient_id cuando un guardian reserva para un menor.';

COMMENT ON TABLE scheduling.availability_schedules IS 'Proyección de disponibilidad por (professional_id, clinic_id). Se sincroniza via eventos del contexto Professional.';
COMMENT ON COLUMN scheduling.availability_schedules.booked_slots IS 'Proyección de appointments activos. Se actualiza al confirmar/cancelar. Permite calcular disponibilidad sin JOIN a appointments.';
