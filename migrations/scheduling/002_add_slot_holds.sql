-- ============================================================
-- Migración: 002_add_slot_holds.sql
-- Bounded Context: Scheduling
-- Propósito: Bloqueo temporal de slots durante el proceso de reserva
--            (Stage 2 del Mapa de Muelas).
-- ============================================================

CREATE TABLE scheduling.slot_holds (
    id              UUID        NOT NULL DEFAULT gen_random_uuid(),
    professional_id UUID        NOT NULL,
    clinic_id       UUID        NOT NULL,
    slot_start      TIMESTAMPTZ NOT NULL,
    slot_end        TIMESTAMPTZ NOT NULL,
    held_by         UUID        NOT NULL,  -- user_id
    held_until      TIMESTAMPTZ NOT NULL,

    CONSTRAINT pk_slot_holds PRIMARY KEY (id),
    -- Un slot sólo puede estar bloqueado por un usuario a la vez.
    CONSTRAINT uq_slot_hold UNIQUE (professional_id, clinic_id, slot_start)
);

-- Índice para el cleanup periódico y el filtrado en GetAvailability.
CREATE INDEX idx_slot_holds_expiry  ON scheduling.slot_holds (held_until);
CREATE INDEX idx_slot_holds_profcli ON scheduling.slot_holds (professional_id, clinic_id, slot_start);

COMMENT ON TABLE scheduling.slot_holds IS
    'Bloqueos temporales de slots (TTL ~10 min). Un slot solo puede tener un hold activo a la vez.';
