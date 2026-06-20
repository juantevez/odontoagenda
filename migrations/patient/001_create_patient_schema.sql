-- ============================================================
-- Migración: 001_create_patient_schema.sql
-- Bounded Context: Patient Management
-- Schema: patient
-- ============================================================

CREATE SCHEMA IF NOT EXISTS patient;

-- Extensión para búsqueda fuzzy por nombre/teléfono (detección de duplicados)
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ── Tabla principal de pacientes ─────────────────────────────────
CREATE TABLE patient.patients (
    id              UUID         NOT NULL DEFAULT gen_random_uuid(),
    user_id         UUID,                              -- vínculo con iam.users (nullable: staff puede crear sin cuenta)
    status          VARCHAR(20)  NOT NULL DEFAULT 'Active',

    -- Información personal
    full_name       VARCHAR(150) NOT NULL,
    birth_date      DATE         NOT NULL,
    gender          CHAR(2)      NOT NULL,              -- M, F, NB, NS

    -- Documento de identidad (único en el sistema)
    doc_type        VARCHAR(20)  NOT NULL,
    doc_number      VARCHAR(50)  NOT NULL,

    -- Contacto
    phone           VARCHAR(20)  NOT NULL,
    whatsapp        VARCHAR(20),
    email           VARCHAR(255),
    address         JSONB,                             -- Address VO serializado
    emergency_name  VARCHAR(150),
    emergency_phone VARCHAR(20),

    -- Ubicación domiciliaria para búsqueda de sede cercana
    home_location   GEOMETRY(POINT, 4326),

    -- Preferencias (VO serializado como JSONB para evitar join innecesario)
    preferences     JSONB        NOT NULL DEFAULT '{
        "preferred_time_of_day": "Cualquiera",
        "communication_channel": "WhatsApp"
    }'::jsonb,

    -- Auditoría y control de concurrencia
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by      UUID,
    version         BIGINT       NOT NULL DEFAULT 1,

    CONSTRAINT pk_patients PRIMARY KEY (id),
    CONSTRAINT uq_patients_doc UNIQUE (doc_type, doc_number),
    CONSTRAINT chk_patients_status CHECK (status IN ('Active', 'Archived')),
    CONSTRAINT chk_patients_gender CHECK (gender IN ('M', 'F', 'NB', 'NS'))
);

-- Índice geoespacial GIST para ST_DWithin (sede más cercana)
CREATE INDEX idx_patients_home_location ON patient.patients USING GIST (home_location);

-- Índices de acceso frecuente
CREATE INDEX idx_patients_user_id   ON patient.patients (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX idx_patients_doc       ON patient.patients (doc_type, doc_number);
CREATE INDEX idx_patients_status    ON patient.patients (status);

-- Índice trigram para búsqueda fuzzy por nombre (pg_trgm)
-- Permite: WHERE full_name % 'juan perez' (similitud)
-- y WHERE full_name ILIKE '%perez%' con soporte de índice
CREATE INDEX idx_patients_name_trgm ON patient.patients USING GIN (full_name gin_trgm_ops);
CREATE INDEX idx_patients_phone_trgm ON patient.patients USING GIN (phone gin_trgm_ops);

-- ── Coberturas del paciente ───────────────────────────────────────
CREATE TABLE patient.patient_coverages (
    id                UUID        NOT NULL DEFAULT gen_random_uuid(),
    patient_id        UUID        NOT NULL REFERENCES patient.patients(id) ON DELETE CASCADE,

    coverage_type     VARCHAR(30) NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'Active',

    -- Referencia al futuro bounded context Coverage & Agreements
    agreement_id      UUID,                              -- sin FK real (cross-context)
    provider_name     VARCHAR(100),
    plan_code         VARCHAR(50),
    membership_number VARCHAR(100),

    valid_from        DATE        NOT NULL,
    valid_until       DATE,                              -- null = sin vencimiento

    -- Copago: solo uno aplica (percent XOR fixed)
    co_pay_percent    SMALLINT,                          -- 0-100
    co_pay_fixed_cents BIGINT,                           -- centavos ARS

    -- Límites anuales y estado de consumo (JSONB: ProcedureCode → centavos)
    annual_limits     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    remaining_limits  JSONB       NOT NULL DEFAULT '{}'::jsonb,

    -- Beneficios del plan (array de Benefit VO)
    benefits          JSONB       NOT NULL DEFAULT '[]'::jsonb,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by        UUID        NOT NULL,

    CONSTRAINT pk_patient_coverages PRIMARY KEY (id),
    CONSTRAINT chk_coverage_type CHECK (
        coverage_type IN ('Privado', 'PrepagaPropia', 'PrepagaExterna', 'ObraSocial', 'Corporativo', 'ConvenioEspecial')
    ),
    CONSTRAINT chk_coverage_status CHECK (status IN ('Active', 'Suspended', 'Expired')),
    CONSTRAINT chk_co_pay CHECK (
        (co_pay_percent IS NULL OR co_pay_fixed_cents IS NULL) -- solo uno de los dos
    ),
    CONSTRAINT chk_valid_dates CHECK (
        valid_until IS NULL OR valid_until >= valid_from
    )
);

-- Previene dos coberturas activas del mismo tipo para el mismo paciente
CREATE UNIQUE INDEX uq_active_coverage_type
    ON patient.patient_coverages (patient_id, coverage_type)
    WHERE status = 'Active';

CREATE INDEX idx_coverages_patient ON patient.patient_coverages (patient_id);
CREATE INDEX idx_coverages_agreement ON patient.patient_coverages (agreement_id) WHERE agreement_id IS NOT NULL;

-- ── Historial de cambios de cobertura (append-only) ───────────────
CREATE TABLE patient.coverage_history (
    id               UUID        NOT NULL DEFAULT gen_random_uuid(),
    patient_id       UUID        NOT NULL REFERENCES patient.patients(id),
    previous_type    VARCHAR(30),
    new_type         VARCHAR(30) NOT NULL,
    previous_status  VARCHAR(20),
    new_status       VARCHAR(20) NOT NULL,
    reason           TEXT,
    changed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    changed_by       UUID        NOT NULL,

    CONSTRAINT pk_coverage_history PRIMARY KEY (id)
);

CREATE INDEX idx_coverage_history_patient ON patient.coverage_history (patient_id, changed_at DESC);

-- ── Alertas médicas ───────────────────────────────────────────────
CREATE TABLE patient.medical_alerts (
    id               UUID        NOT NULL DEFAULT gen_random_uuid(),
    patient_id       UUID        NOT NULL REFERENCES patient.patients(id) ON DELETE CASCADE,
    alert_type       VARCHAR(30) NOT NULL,
    severity         VARCHAR(20) NOT NULL,
    description      TEXT        NOT NULL,
    is_self_reported BOOLEAN     NOT NULL DEFAULT false,
    is_active        BOOLEAN     NOT NULL DEFAULT true,
    created_by       UUID        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at       TIMESTAMPTZ,
    revoked_by       UUID,

    CONSTRAINT pk_medical_alerts PRIMARY KEY (id),
    CONSTRAINT chk_alert_severity CHECK (severity IN ('Info', 'Warning', 'Critical')),
    CONSTRAINT chk_alert_type CHECK (
        alert_type IN ('Alergia', 'Medicamento', 'Condición', 'Anestesia',
                       'RiesgoSangrado', 'RiesgoInfeccioso', 'Otro')
    )
);

CREATE INDEX idx_alerts_patient_active ON patient.medical_alerts (patient_id) WHERE is_active = true;
CREATE INDEX idx_alerts_severity ON patient.medical_alerts (severity) WHERE is_active = true;

-- ── Historial odontológico resumido ───────────────────────────────
-- Entity separada con su propio ciclo de vida.
-- Se actualiza por eventos de Scheduling (AppointmentCompleted), no directamente.
CREATE TABLE patient.dental_history_summaries (
    id               UUID        NOT NULL DEFAULT gen_random_uuid(),
    patient_id       UUID        NOT NULL REFERENCES patient.patients(id) ON DELETE CASCADE,
    last_visit_date  DATE,
    risk_level       VARCHAR(10) NOT NULL DEFAULT 'Bajo',
    visit_count      INT         NOT NULL DEFAULT 0,
    main_treatments  JSONB       NOT NULL DEFAULT '[]'::jsonb,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by_event VARCHAR(100),                      -- nombre del evento que lo actualizó

    CONSTRAINT pk_dental_history PRIMARY KEY (id),
    CONSTRAINT uq_dental_history_patient UNIQUE (patient_id),  -- 1:1 con Patient
    CONSTRAINT chk_risk_level CHECK (risk_level IN ('Bajo', 'Medio', 'Alto'))
);

-- ── Comentarios ───────────────────────────────────────────────────
COMMENT ON TABLE patient.patients IS 'Aggregate Patient del bounded context Patient Management';
COMMENT ON COLUMN patient.patients.home_location IS 'Coordenadas EPSG:4326 del domicilio para búsqueda de sede más cercana con PostGIS';
COMMENT ON COLUMN patient.patients.preferences IS 'JSONB: {preferred_clinic_id?, preferred_professional_ids?, preferred_time_of_day, communication_channel}';
COMMENT ON COLUMN patient.patients.version IS 'Optimistic locking: se incrementa en cada UPDATE';
COMMENT ON TABLE patient.coverage_history IS 'Log append-only de cambios de cobertura. Nunca se modifica ni elimina.';
COMMENT ON TABLE patient.dental_history_summaries IS 'Resumen del historial clínico. Solo actualizable por eventos de Scheduling.';
COMMENT ON COLUMN patient.patient_coverages.agreement_id IS 'Referencia sin FK al futuro bounded context Coverage & Agreements';
