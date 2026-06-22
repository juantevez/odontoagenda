-- ============================================================
-- Migración: 001_create_billing_schema.sql
-- Bounded Context: Billing & Payments
-- Schema: billing
-- ============================================================

CREATE SCHEMA IF NOT EXISTS billing;

-- ── Tabla principal de presupuestos ───────────────────────────────
-- El id del Quote es el mismo UUID que el AppointmentID (relación 1:1).
-- Esto simplifica los lookups: no se necesita JOIN para encontrar el Quote de una cita.
CREATE TABLE billing.quotes (
    id                    UUID         NOT NULL,   -- mismo UUID que appointment_id
    appointment_id        UUID         NOT NULL,
    patient_id            UUID         NOT NULL,
    clinic_id             UUID         NOT NULL,
    professional_id       UUID         NOT NULL,
    procedure_code        VARCHAR(50)  NOT NULL,
    procedure_description VARCHAR(200),

    status                VARCHAR(20)  NOT NULL DEFAULT 'Draft',

    -- Montos en centavos ARS para evitar errores de punto flotante
    arancel_cents         BIGINT       NOT NULL CHECK (arancel_cents > 0),
    coverage_percent      SMALLINT     NOT NULL DEFAULT 0 CHECK (coverage_percent BETWEEN 0 AND 100),
    coverage_amount_cents BIGINT       NOT NULL DEFAULT 0 CHECK (coverage_amount_cents >= 0),
    co_pay_type           VARCHAR(20)  NOT NULL DEFAULT 'Percent',
    co_pay_amount_cents   BIGINT       NOT NULL DEFAULT 0 CHECK (co_pay_amount_cents >= 0),

    -- Cobertura (cross-context refs, sin FK real)
    coverage_type         VARCHAR(30),
    agreement_id          UUID,
    plan_id               UUID,

    -- Autorización
    requires_authorization BOOLEAN     NOT NULL DEFAULT false,
    authorization_code    VARCHAR(100),

    -- Política de cancelación (snapshot vigente al crear el Quote)
    -- { "free_hours": 24, "late_cancellation_percent": 50, "no_show_percent": 100, "min_fee_cents": 0 }
    cancellation_policy   JSONB        NOT NULL DEFAULT '{"free_hours":24,"late_cancellation_percent":50,"no_show_percent":100,"min_fee_cents":0}'::jsonb,

    -- Flag: Coverage BC no estaba disponible al crear → montos son estimados
    pending_coverage_check BOOLEAN     NOT NULL DEFAULT false,

    -- Pagos y cargos como JSONB (entidades internas del aggregate)
    -- payments: [{ "id", "amount_cents", "payment_method", "status", "external_reference", "paid_at", "receipt_number", "notes", "created_at" }]
    payments              JSONB        NOT NULL DEFAULT '[]'::jsonb,
    -- late_fees: [{ "id", "fee_type", "amount_cents", "status", "waived_by", "waived_reason", "created_at" }]
    late_fees             JSONB        NOT NULL DEFAULT '[]'::jsonb,

    -- Slot de la cita (para calcular cancelación tardía)
    slot_start            TIMESTAMPTZ  NOT NULL,
    slot_end              TIMESTAMPTZ  NOT NULL,

    -- Auditoría y optimistic locking
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    version               BIGINT       NOT NULL DEFAULT 1,

    CONSTRAINT pk_quotes PRIMARY KEY (id),
    CONSTRAINT chk_quotes_status CHECK (
        status IN ('Draft','Confirmed','PartialPaid','Paid','Voided','Refunded','ChargedFee')
    ),
    CONSTRAINT chk_quotes_co_pay_type CHECK (
        co_pay_type IN ('Percent','FixedAmount','None')
    ),
    CONSTRAINT chk_quotes_slot_order CHECK (slot_end > slot_start),
    -- INV-01: la suma de montos debe ser <= arancel (permite diferencias por redondeo de centavo)
    CONSTRAINT chk_quotes_amounts CHECK (
        coverage_amount_cents + co_pay_amount_cents <= arancel_cents + 1
    )
);

-- INV-05: solo un Quote activo por appointment.
-- Los estados terminales (Voided, Refunded) permiten múltiples filas (para historial de reintentos).
CREATE UNIQUE INDEX uq_quotes_active_per_appointment
    ON billing.quotes (appointment_id)
    WHERE status IN ('Draft','Confirmed','PartialPaid','Paid','ChargedFee');

-- Índices de acceso frecuente
CREATE INDEX idx_quotes_patient_status
    ON billing.quotes (patient_id, status);

CREATE INDEX idx_quotes_clinic_date
    ON billing.quotes (clinic_id, created_at);

CREATE INDEX idx_quotes_status
    ON billing.quotes (status)
    WHERE status IN ('Confirmed','PartialPaid','ChargedFee');

CREATE INDEX idx_quotes_appointment
    ON billing.quotes (appointment_id);

-- GIN para queries sobre los JSONB internos (pagos y cargos)
CREATE INDEX idx_quotes_payments_gin
    ON billing.quotes USING GIN (payments);
CREATE INDEX idx_quotes_late_fees_gin
    ON billing.quotes USING GIN (late_fees);

-- ── Política de cancelación por sede ─────────────────────────────
-- Permite personalizar los porcentajes de cargo por sede.
-- Si no existe un registro para la sede, Billing usa los valores por defecto.
CREATE TABLE billing.clinic_cancellation_policies (
    id                             UUID        NOT NULL DEFAULT gen_random_uuid(),
    clinic_id                      UUID        NOT NULL,
    cancellation_free_hours        INT         NOT NULL DEFAULT 24
                                               CHECK (cancellation_free_hours >= 0),
    late_cancellation_fee_percent  SMALLINT    NOT NULL DEFAULT 50
                                               CHECK (late_cancellation_fee_percent BETWEEN 0 AND 100),
    no_show_fee_percent            SMALLINT    NOT NULL DEFAULT 100
                                               CHECK (no_show_fee_percent BETWEEN 0 AND 100),
    min_fee_cents                  BIGINT      NOT NULL DEFAULT 0
                                               CHECK (min_fee_cents >= 0),
    updated_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by                     UUID,

    CONSTRAINT pk_clinic_cancellation_policies PRIMARY KEY (id),
    CONSTRAINT uq_clinic_cancellation_policies UNIQUE (clinic_id)
);

-- ── Comentarios ───────────────────────────────────────────────────
COMMENT ON TABLE billing.quotes IS
    'Aggregate Quote: presupuesto económico de una cita. '
    'El id es el mismo UUID que el appointment_id (relación 1:1). '
    'payments y late_fees son entidades internas serializadas como JSONB.';

COMMENT ON COLUMN billing.quotes.id IS
    'Mismo UUID que appointment_id. Permite lookup directo sin JOIN.';

COMMENT ON COLUMN billing.quotes.arancel_cents IS
    'Arancel bruto del procedimiento en centavos ARS. Viene en el evento appointment.booked (Opción A).';

COMMENT ON COLUMN billing.quotes.cancellation_policy IS
    'Snapshot de la política vigente al crear el Quote. '
    'No se actualiza si la política cambia posteriormente.';

COMMENT ON COLUMN billing.quotes.pending_coverage_check IS
    'True cuando Coverage BC no respondió al crear el Quote. '
    'Los montos son estimados (pago privado como fallback). '
    'Recepcionista debe verificar y corregir manualmente.';

COMMENT ON COLUMN billing.quotes.payments IS
    'Array JSONB de Payment entities. '
    'Estructura: [{id, amount_cents, payment_method, status, external_reference, paid_at, receipt_number, notes, created_at}]';

COMMENT ON COLUMN billing.quotes.late_fees IS
    'Array JSONB de LateFee entities. '
    'Estructura: [{id, fee_type, amount_cents, status, waived_by, waived_reason, created_at}]';

COMMENT ON TABLE billing.clinic_cancellation_policies IS
    'Política de cancelación por sede. '
    'Si no existe registro para la sede, se usan los valores por defecto del sistema '
    '(24h gratuitas, 50% por tardía, 100% por no-show).';
