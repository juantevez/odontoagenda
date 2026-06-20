-- ============================================================
-- Migración: 001_create_iam_schema.sql
-- Bounded Context: Identity & Access (IAM)
-- Schema: iam
-- ============================================================

CREATE SCHEMA IF NOT EXISTS iam;

-- ── Tabla principal de usuarios ──────────────────────────────────
CREATE TABLE iam.users (
    id              UUID        NOT NULL DEFAULT gen_random_uuid(),
    email           VARCHAR(255) NOT NULL,
    password_hash   TEXT        NOT NULL,
    role            VARCHAR(30)  NOT NULL,
    status          VARCHAR(20)  NOT NULL DEFAULT 'Active',

    -- Vínculo con la entidad de negocio asociada (Patient, Professional, etc.)
    linked_id       UUID,
    linked_type     VARCHAR(30),  -- 'patient' | 'professional' | 'staff'

    -- Refresh tokens almacenados como JSONB (array de tokens con metadata).
    -- Se eligió JSONB por la naturaleza variable y poco frecuente de acceso.
    -- Alternativa futura: tabla iam.refresh_tokens si crece mucho.
    refresh_tokens  JSONB        NOT NULL DEFAULT '[]'::jsonb,

    -- Auditoría
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by      UUID,
    updated_by      UUID,

    -- Optimistic locking
    version         BIGINT       NOT NULL DEFAULT 1,

    CONSTRAINT pk_users PRIMARY KEY (id),
    CONSTRAINT uq_users_email UNIQUE (email),
    CONSTRAINT chk_users_role CHECK (
        role IN ('paciente', 'profesional', 'recepcionista', 'admin_sucursal', 'superadmin')
    ),
    CONSTRAINT chk_users_status CHECK (
        status IN ('Active', 'Suspended', 'Pending')
    )
);

-- Índices de acceso frecuente
CREATE INDEX idx_users_email   ON iam.users (email);
CREATE INDEX idx_users_role    ON iam.users (role);
CREATE INDEX idx_users_status  ON iam.users (status);
CREATE INDEX idx_users_linked  ON iam.users (linked_id) WHERE linked_id IS NOT NULL;

-- ── Cuentas familiares ───────────────────────────────────────────
CREATE TABLE iam.family_accounts (
    id               UUID        NOT NULL DEFAULT gen_random_uuid(),
    family_name      VARCHAR(100),
    primary_adult_id UUID        NOT NULL,   -- referencia a patient.patients (cross-schema por UUID)
    status           VARCHAR(20)  NOT NULL DEFAULT 'Active',

    -- Lista de miembros almacenada como JSONB.
    -- Estructura: [{ patient_id, role, relationship, is_minor, guardian_ids[], joined_at }]
    -- Candidata a normalizar en iam.family_members si los queries JSONB se vuelven complejos.
    members          JSONB        NOT NULL DEFAULT '[]'::jsonb,

    -- Auditoría
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),

    -- Optimistic locking
    version          BIGINT       NOT NULL DEFAULT 1,

    CONSTRAINT pk_family_accounts PRIMARY KEY (id),
    CONSTRAINT chk_family_status CHECK (status IN ('Active', 'Suspended'))
);

-- Índice GIN para queries sobre el JSONB de miembros:
-- Permite: members @> '[{"patient_id": "..."}]'
CREATE INDEX idx_family_members_gin ON iam.family_accounts USING GIN (members);
CREATE INDEX idx_family_primary     ON iam.family_accounts (primary_adult_id);
CREATE INDEX idx_family_status      ON iam.family_accounts (status);

-- ── Tabla de historial de passwords (para política de no-reutilización) ──
-- Opcional en v1, se agrega para no romper el schema en una migración futura.
CREATE TABLE iam.password_history (
    id           UUID        NOT NULL DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES iam.users(id) ON DELETE CASCADE,
    password_hash TEXT       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT pk_password_history PRIMARY KEY (id)
);

CREATE INDEX idx_password_history_user ON iam.password_history (user_id, created_at DESC);

-- ── Comentarios de documentación en columnas ─────────────────────
COMMENT ON TABLE  iam.users                    IS 'Aggregate User del bounded context IAM';
COMMENT ON COLUMN iam.users.linked_id          IS 'UUID de la entidad de negocio asociada (paciente, profesional, staff)';
COMMENT ON COLUMN iam.users.refresh_tokens     IS 'Array JSONB de refresh tokens activos/revocados. Max 5 por usuario.';
COMMENT ON COLUMN iam.users.version            IS 'Versión para optimistic locking. Se incrementa en cada UPDATE.';

COMMENT ON TABLE  iam.family_accounts          IS 'Aggregate FamilyAccount: agrupa pacientes relacionados bajo una cuenta familiar';
COMMENT ON COLUMN iam.family_accounts.members  IS 'JSONB con lista de FamilyMember: {patient_id, role, is_minor, guardian_ids[], relationship}';
