-- ============================================================
-- Migración: 001_create_notifications_schema.sql
-- Bounded Context: Notifications
-- Schema: notifications
-- ============================================================

CREATE SCHEMA IF NOT EXISTS notifications;

-- ── Bandeja de entrada del staff ──────────────────────────────────
-- Persiste notificaciones accionables para recepcionistas y admins.
-- Solo 4 tipos se escriben aquí (ver InboxEventSubscriber):
--   appointment_booked, appointment_cancelled, appointment_no_show,
--   license_expiring_soon.
CREATE TABLE notifications.inbox (
    id           UUID        NOT NULL DEFAULT gen_random_uuid(),
    type         VARCHAR(60) NOT NULL,
    clinic_id    UUID,                    -- NULL = visible en todas las sedes
    reference_id TEXT,                   -- appointment_id, license_id, etc.
    title        TEXT        NOT NULL,
    body         TEXT        NOT NULL,
    read_at      TIMESTAMPTZ,            -- NULL = no leído
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT notifications_inbox_pkey PRIMARY KEY (id)
);

-- Consulta principal: todas las notificaciones de una sede, más recientes primero.
CREATE INDEX notifications_inbox_clinic_created
    ON notifications.inbox (clinic_id, created_at DESC);

-- Consulta de no leídas (filtro frecuente para el badge del header).
CREATE INDEX notifications_inbox_unread
    ON notifications.inbox (clinic_id, read_at)
    WHERE read_at IS NULL;
