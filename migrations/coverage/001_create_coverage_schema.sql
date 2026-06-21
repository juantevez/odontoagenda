-- ============================================================
-- Migración: 001_create_coverage_schema.sql
-- Bounded Context: Coverage & Agreements
-- Schema: coverage
-- ============================================================

CREATE SCHEMA IF NOT EXISTS coverage;

-- ── Tabla principal de convenios ─────────────────────────────────
CREATE TABLE coverage.agreements (
    id                       UUID         NOT NULL DEFAULT gen_random_uuid(),
    agreement_code           VARCHAR(50)  NOT NULL,
    provider_name            VARCHAR(100) NOT NULL,
    provider_type            VARCHAR(30)  NOT NULL,
    status                   VARCHAR(20)  NOT NULL DEFAULT 'Active',
    valid_from               DATE         NOT NULL,
    valid_until              DATE,
    contact_email            VARCHAR(255) NOT NULL,
    contact_phone            VARCHAR(20)  NOT NULL,
    cancellation_notice_days INT          NOT NULL DEFAULT 30,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by               UUID,
    version                  BIGINT       NOT NULL DEFAULT 1,

    CONSTRAINT pk_agreements PRIMARY KEY (id),
    CONSTRAINT uq_agreements_code UNIQUE (agreement_code),
    CONSTRAINT chk_agreements_provider_type CHECK (
        provider_type IN ('ObraSocial','PrepagaExterna','PrepagaPropia',
                          'Corporativo','ConvenioEspecial','Privado')
    ),
    CONSTRAINT chk_agreements_status CHECK (
        status IN ('Active','Suspended','Expired')
    ),
    CONSTRAINT chk_agreements_dates CHECK (
        valid_until IS NULL OR valid_until > valid_from
    )
);

CREATE INDEX idx_agreements_status        ON coverage.agreements (status);
CREATE INDEX idx_agreements_provider_type ON coverage.agreements (provider_type);
CREATE INDEX idx_agreements_valid_until   ON coverage.agreements (valid_until)
    WHERE valid_until IS NOT NULL AND status = 'Active';

-- ── Planes de convenio ───────────────────────────────────────────
-- Los plans se almacenan en una tabla separada para facilitar queries
-- del tipo "todos los planes que cubren procedimiento X".
-- covered_procedures se almacena como JSONB (array de ProcedureRule).
CREATE TABLE coverage.plans (
    id                        UUID         NOT NULL DEFAULT gen_random_uuid(),
    agreement_id              UUID         NOT NULL REFERENCES coverage.agreements(id) ON DELETE CASCADE,
    plan_code                 VARCHAR(50)  NOT NULL,
    plan_name                 VARCHAR(150) NOT NULL,
    co_pay_type               VARCHAR(20)  NOT NULL,
    co_pay_value              INT          NOT NULL DEFAULT 0,
    requires_pre_authorization BOOLEAN     NOT NULL DEFAULT false,
    max_annual_visits         INT,
    status                    VARCHAR(20)  NOT NULL DEFAULT 'Active',
    -- Array de ProcedureRule serializado como JSONB.
    -- Estructura de cada elemento:
    -- {
    --   "procedure_code": "ORTODONCIA",
    --   "coverage_percent": 80,
    --   "co_pay_override": null | { "co_pay_type": "Percent", "co_pay_value": 20 },
    --   "requires_authorization": false,
    --   "max_per_year": 12 | null,
    --   "max_amount_cents": null,
    --   "waiting_period_days": 0,
    --   "age_min": null,
    --   "age_max": null
    -- }
    covered_procedures        JSONB        NOT NULL DEFAULT '[]'::jsonb,
    created_at                TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT pk_plans PRIMARY KEY (id),
    CONSTRAINT uq_plans_code_per_agreement UNIQUE (agreement_id, plan_code),
    CONSTRAINT chk_plans_co_pay_type CHECK (
        co_pay_type IN ('Percent','FixedAmount','None')
    ),
    CONSTRAINT chk_plans_status CHECK (
        status IN ('Active','Discontinued')
    ),
    CONSTRAINT chk_plans_co_pay_value CHECK (co_pay_value >= 0)
);

CREATE INDEX idx_plans_agreement    ON coverage.plans (agreement_id);
CREATE INDEX idx_plans_status       ON coverage.plans (status);
-- GIN para queries sobre prestaciones cubiertas:
-- WHERE covered_procedures @> '[{"procedure_code": "ORTODONCIA"}]'
CREATE INDEX idx_plans_procedures_gin ON coverage.plans USING GIN (covered_procedures);

-- ── Afiliaciones de pacientes a planes ───────────────────────────
-- Se sincroniza desde Patient BC via evento patient.coverage.updated.
-- Cross-context: sin FK real a patient schema.
CREATE TABLE coverage.patient_affiliations (
    id                UUID        NOT NULL DEFAULT gen_random_uuid(),
    patient_id        UUID        NOT NULL,
    agreement_id      UUID        NOT NULL REFERENCES coverage.agreements(id),
    plan_id           UUID        NOT NULL REFERENCES coverage.plans(id),
    membership_number VARCHAR(100) NOT NULL,
    affiliated_since  DATE        NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'Active',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT pk_patient_affiliations PRIMARY KEY (id),
    CONSTRAINT chk_affiliation_status CHECK (
        status IN ('Active','Suspended','Cancelled')
    )
);

-- Un paciente puede tener solo una afiliación activa por plan.
CREATE UNIQUE INDEX uq_active_affiliation_per_plan
    ON coverage.patient_affiliations (patient_id, plan_id)
    WHERE status = 'Active';

CREATE INDEX idx_affiliations_patient    ON coverage.patient_affiliations (patient_id);
CREATE INDEX idx_affiliations_plan       ON coverage.patient_affiliations (plan_id);
CREATE INDEX idx_affiliations_agreement  ON coverage.patient_affiliations (agreement_id);

-- ── Solicitudes de autorización ───────────────────────────────────
CREATE TABLE coverage.authorization_requests (
    id                       UUID         NOT NULL DEFAULT gen_random_uuid(),
    agreement_id             UUID         NOT NULL REFERENCES coverage.agreements(id),
    plan_id                  UUID         NOT NULL REFERENCES coverage.plans(id),
    patient_id               UUID         NOT NULL,
    patient_membership_number VARCHAR(100) NOT NULL,
    procedure_code           VARCHAR(50)  NOT NULL,
    appointment_id           UUID,
    requested_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    status                   VARCHAR(20)  NOT NULL DEFAULT 'Pending',
    authorization_code       VARCHAR(100),
    expires_at               TIMESTAMPTZ,
    rejection_reason         TEXT,
    resolved_at              TIMESTAMPTZ,
    resolved_by              UUID,
    version                  BIGINT       NOT NULL DEFAULT 1,

    CONSTRAINT pk_authorization_requests PRIMARY KEY (id),
    CONSTRAINT chk_authorization_status CHECK (
        status IN ('Pending','Approved','Rejected','Expired')
    )
);

CREATE INDEX idx_auth_requests_status      ON coverage.authorization_requests (status)
    WHERE status = 'Pending';
CREATE INDEX idx_auth_requests_patient     ON coverage.authorization_requests (patient_id);
CREATE INDEX idx_auth_requests_agreement   ON coverage.authorization_requests (agreement_id);
CREATE INDEX idx_auth_requests_appointment ON coverage.authorization_requests (appointment_id)
    WHERE appointment_id IS NOT NULL;
-- Para el job de expiración:
CREATE INDEX idx_auth_requests_expires     ON coverage.authorization_requests (expires_at)
    WHERE status = 'Pending' AND expires_at IS NOT NULL;

-- ── Comentarios ───────────────────────────────────────────────────
COMMENT ON TABLE coverage.agreements IS 'Aggregate Agreement: convenio entre OdontoAgenda y un financiador de salud.';
COMMENT ON COLUMN coverage.agreements.provider_type IS 'Privado: pago de bolsillo 100%. Los demás requieren plan y ProcedureRules.';
COMMENT ON TABLE coverage.plans IS 'Entidad Plan dentro del Aggregate Agreement. covered_procedures es JSONB de ProcedureRule.';
COMMENT ON COLUMN coverage.plans.covered_procedures IS 'Array JSONB de ProcedureRule. GIN index para queries por procedureCode.';
COMMENT ON TABLE coverage.patient_affiliations IS 'Sincronizado desde Patient BC via evento patient.coverage.updated. Sin FK real a patient schema.';
COMMENT ON TABLE coverage.authorization_requests IS 'Ciclo de vida: Pending → Approved | Rejected | Expired. Approved no puede pasar a Rejected.';
