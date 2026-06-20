-- ============================================================
-- Migración: 001_create_professional_schema.sql
-- Bounded Context: Professional Management
-- Schema: professional
-- ============================================================

CREATE SCHEMA IF NOT EXISTS professional;

-- ── Tabla principal de profesionales ────────────────────────────
CREATE TABLE professional.professionals (
    id          UUID         NOT NULL DEFAULT gen_random_uuid(),
    user_id     UUID,                              -- vínculo con iam.users (nullable)
    status      VARCHAR(20)  NOT NULL DEFAULT 'Active',

    full_name   VARCHAR(150) NOT NULL,
    doc_type    VARCHAR(20)  NOT NULL,
    doc_number  VARCHAR(50)  NOT NULL,
    email       VARCHAR(255) NOT NULL,
    phone       VARCHAR(20)  NOT NULL,
    bio         TEXT,

    -- Duraciones por defecto del profesional como JSONB:
    -- { "ODONTOLOGIA_GENERAL": { "procedure_code": "...", "minutes": 30, "buffer_minutes": 5 }, ... }
    default_durations JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by  UUID,
    version     BIGINT       NOT NULL DEFAULT 1,

    CONSTRAINT pk_professionals PRIMARY KEY (id),
    CONSTRAINT uq_professionals_doc UNIQUE (doc_type, doc_number),
    CONSTRAINT uq_professionals_email UNIQUE (email),
    CONSTRAINT chk_professional_status CHECK (
        status IN ('Active', 'Inactive', 'Suspended')
    )
);

CREATE INDEX idx_professionals_status ON professional.professionals (status);
CREATE INDEX idx_professionals_user   ON professional.professionals (user_id) WHERE user_id IS NOT NULL;

-- ── Matrículas profesionales ──────────────────────────────────────
-- Una matrícula por especialidad; pueden existir varias para el mismo profesional
-- si tiene múltiples especialidades (ej: Odontología General + Ortodoncia).
CREATE TABLE professional.licenses (
    id               UUID        NOT NULL DEFAULT gen_random_uuid(),
    professional_id  UUID        NOT NULL REFERENCES professional.professionals(id) ON DELETE CASCADE,

    specialty_code   VARCHAR(50) NOT NULL,
    specialty_name   VARCHAR(100) NOT NULL,
    license_number   VARCHAR(100) NOT NULL,
    issuing_body     VARCHAR(150) NOT NULL,
    issued_at        DATE        NOT NULL,
    expires_at       DATE,                              -- null = sin vencimiento
    status           VARCHAR(20) NOT NULL DEFAULT 'Active',
    document_ref     VARCHAR(500),                     -- S3 key del documento escaneado

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT pk_licenses PRIMARY KEY (id),
    CONSTRAINT chk_license_status CHECK (
        status IN ('Active', 'Expired', 'Suspended', 'Revoked')
    )
);

-- Previene dos matrículas activas para la misma especialidad del mismo profesional.
CREATE UNIQUE INDEX uq_active_license_specialty
    ON professional.licenses (professional_id, specialty_code)
    WHERE status = 'Active';

CREATE INDEX idx_licenses_professional ON professional.licenses (professional_id);
CREATE INDEX idx_licenses_specialty    ON professional.licenses (specialty_code);
CREATE INDEX idx_licenses_expires      ON professional.licenses (expires_at) WHERE expires_at IS NOT NULL AND status = 'Active';

-- ── Asignaciones a sedes ──────────────────────────────────────────
-- Una asignación por sede activa. Contiene el horario y las duraciones.
CREATE TABLE professional.clinic_assignments (
    id               UUID        NOT NULL DEFAULT gen_random_uuid(),
    professional_id  UUID        NOT NULL REFERENCES professional.professionals(id) ON DELETE CASCADE,
    clinic_id        UUID        NOT NULL,              -- referencia cross-context sin FK real
    status           VARCHAR(20) NOT NULL DEFAULT 'Active',

    -- Especialidades que practica en esta sede: ["ORTODONCIA", "ODONTOLOGIA_GENERAL"]
    assigned_specialties JSONB   NOT NULL DEFAULT '[]'::jsonb,

    -- Horario recurrente: array de DaySchedule
    -- [{ "weekday": 1, "start_hour": 9, "start_min": 0, "end_hour": 13, "end_min": 0 }, ...]
    weekly_schedule  JSONB       NOT NULL DEFAULT '[]'::jsonb,

    -- Días de excepción: vacaciones, feriados, horarios especiales
    -- [{ "date": "2026-07-09", "reason": "Feriado", "is_working": false }, ...]
    exception_days   JSONB       NOT NULL DEFAULT '[]'::jsonb,

    -- Duraciones de procedimientos específicas para esta sede (override del default).
    -- Misma estructura que professionals.default_durations.
    procedure_durations JSONB    NOT NULL DEFAULT '{}'::jsonb,

    assigned_from    DATE        NOT NULL,
    assigned_until   DATE,                              -- null = sin fecha de fin
    assigned_by      UUID        NOT NULL,

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT pk_clinic_assignments PRIMARY KEY (id),
    CONSTRAINT chk_assignment_status CHECK (
        status IN ('Active', 'Suspended', 'Ended')
    )
);

-- Previene dos asignaciones activas a la misma sede.
CREATE UNIQUE INDEX uq_active_clinic_assignment
    ON professional.clinic_assignments (professional_id, clinic_id)
    WHERE status = 'Active';

CREATE INDEX idx_assignments_professional ON professional.clinic_assignments (professional_id);
CREATE INDEX idx_assignments_clinic       ON professional.clinic_assignments (clinic_id);
CREATE INDEX idx_assignments_active       ON professional.clinic_assignments (clinic_id, status)
    WHERE status = 'Active';

-- Índices GIN para queries sobre los JSONB de horarios y especialidades.
-- Permiten: weekly_schedule @> '[{"weekday": 1}]'
CREATE INDEX idx_assignments_schedule_gin    ON professional.clinic_assignments USING GIN (weekly_schedule);
CREATE INDEX idx_assignments_specialties_gin ON professional.clinic_assignments USING GIN (assigned_specialties);
CREATE INDEX idx_assignments_exceptions_gin  ON professional.clinic_assignments USING GIN (exception_days);

-- ── Comentarios ───────────────────────────────────────────────────
COMMENT ON TABLE  professional.professionals              IS 'Aggregate Professional del bounded context Professional Management';
COMMENT ON COLUMN professional.professionals.default_durations IS 'JSONB: map ProcedureCode → ProcedureDuration. Override del catálogo para este profesional.';
COMMENT ON COLUMN professional.professionals.version      IS 'Optimistic locking: se incrementa en cada UPDATE';

COMMENT ON TABLE  professional.licenses                   IS 'Matrícula habilitante por especialidad. Max 1 activa por especialidad por profesional.';
COMMENT ON COLUMN professional.licenses.document_ref      IS 'Referencia al documento escaneado (ej: S3 key). No se almacena el archivo en BD.';
COMMENT ON COLUMN professional.licenses.expires_at        IS 'NULL = sin vencimiento. Job scheduler verifica los próximos 30 días.';

COMMENT ON TABLE  professional.clinic_assignments         IS 'Asignación de un profesional a una sede. Contiene horario y duraciones de procedimientos.';
COMMENT ON COLUMN professional.clinic_assignments.clinic_id IS 'UUID de la sede (cross-context: sin FK real hacia clinic schema)';
COMMENT ON COLUMN professional.clinic_assignments.weekly_schedule IS 'Array JSONB de DaySchedule: horario recurrente semanal';
COMMENT ON COLUMN professional.clinic_assignments.exception_days  IS 'Array JSONB de ExceptionDay: vacaciones, feriados, horarios especiales';
COMMENT ON COLUMN professional.clinic_assignments.procedure_durations IS 'JSONB: override de duraciones para esta sede específica';
