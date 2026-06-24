-- ============================================================
-- OdontoAgenda — Datos de prueba para desarrollo
-- Cubre: IAM, Patient, Professional, Scheduling
-- 
-- Objetivo: tener 3 profesionales de especialidades distintas
--           con horarios de atención, pacientes de prueba y
--           availability schedules listos para reservar turnos.
--
-- Ejecutar con: psql -U odontoagenda -d odontoagenda -f seed_dev.sql
-- ============================================================

BEGIN;

-- ── 0. Limpiar datos previos de prueba (idempotente) ─────────────
DELETE FROM scheduling.availability_schedules WHERE professional_id IN (
    SELECT id FROM professional.professionals
    WHERE email LIKE '%@odontoagenda.test'
);
DELETE FROM scheduling.appointments WHERE professional_id IN (
    SELECT id FROM professional.professionals
    WHERE email LIKE '%@odontoagenda.test'
);
DELETE FROM professional.clinic_assignments WHERE professional_id IN (
    SELECT id FROM professional.professionals
    WHERE email LIKE '%@odontoagenda.test'
);
DELETE FROM professional.licenses WHERE professional_id IN (
    SELECT id FROM professional.professionals
    WHERE email LIKE '%@odontoagenda.test'
);
DELETE FROM professional.professionals WHERE email LIKE '%@odontoagenda.test';

DELETE FROM patient.dental_history_summaries WHERE patient_id IN (
    SELECT id FROM patient.patients WHERE phone LIKE '+5491155500%'
);
DELETE FROM patient.patient_coverages WHERE patient_id IN (
    SELECT id FROM patient.patients WHERE phone LIKE '+5491155500%'
);
DELETE FROM patient.medical_alerts WHERE patient_id IN (
    SELECT id FROM patient.patients WHERE phone LIKE '+5491155500%'
);
DELETE FROM patient.patients WHERE phone LIKE '+5491155500%';

DELETE FROM iam.users WHERE email LIKE '%@odontoagenda.test';

-- ── 1. UUIDs fijos (para poder referenciar en cualquier orden) ────
-- Los comentamos para que sea fácil copiar/pegar en Postman/curl.

-- Clínica simulada (no existe tabla clinic en este MVP, solo el UUID)
-- clinic_id: a1000000-0000-0000-0000-000000000001

-- ── 2. USUARIOS IAM ───────────────────────────────────────────────
-- Password = "Test1234" hasheada con bcrypt cost 12
-- Hash generado offline: $2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TqawnHfbPHbSyHDAdMfZhWAHhg3a
-- (todos los usuarios de prueba usan el mismo password por simplicidad)

INSERT INTO iam.users (id, email, password_hash, role, status, linked_type, created_at, updated_at, version)
VALUES
    -- Recepcionista
    ('b1000000-0000-0000-0000-000000000001',
     'recepcionista@odontoagenda.test',
     '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TqawnHfbPHbSyHDAdMfZhWAHhg3a',
     'recepcionista', 'Active', 'staff',
     NOW(), NOW(), 1),

    -- Admin de sede
    ('b1000000-0000-0000-0000-000000000002',
     'admin@odontoagenda.test',
     '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TqawnHfbPHbSyHDAdMfZhWAHhg3a',
     'admin_sucursal', 'Active', 'staff',
     NOW(), NOW(), 1),

    -- Paciente 1 — con obra social OSDE
    ('b1000000-0000-0000-0000-000000000010',
     'juan.perez@odontoagenda.test',
     '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TqawnHfbPHbSyHDAdMfZhWAHhg3a',
     'paciente', 'Active', 'patient',
     NOW(), NOW(), 1),

    -- Paciente 2 — privado
    ('b1000000-0000-0000-0000-000000000011',
     'maria.garcia@odontoagenda.test',
     '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TqawnHfbPHbSyHDAdMfZhWAHhg3a',
     'paciente', 'Active', 'patient',
     NOW(), NOW(), 1),

    -- Paciente 3 — Swiss Medical
    ('b1000000-0000-0000-0000-000000000012',
     'carlos.rodriguez@odontoagenda.test',
     '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TqawnHfbPHbSyHDAdMfZhWAHhg3a',
     'paciente', 'Active', 'patient',
     NOW(), NOW(), 1);

-- ── 3. PACIENTES ─────────────────────────────────────────────────

INSERT INTO patient.patients (
    id, user_id, status,
    full_name, birth_date, gender,
    doc_type, doc_number,
    phone, email,
    preferences,
    created_at, updated_at, version
) VALUES
    (
        'c1000000-0000-0000-0000-000000000001',
        'b1000000-0000-0000-0000-000000000010',
        'Active',
        'Juan Carlos Pérez', '1985-03-15', 'M',
        'DNI', '28345678',
        '+5491155500001', 'juan.perez@odontoagenda.test',
        '{"preferred_time_of_day": "Mañana", "communication_channel": "WhatsApp"}',
        NOW(), NOW(), 1
    ),
    (
        'c1000000-0000-0000-0000-000000000002',
        'b1000000-0000-0000-0000-000000000011',
        'Active',
        'María Elena García', '1990-07-22', 'F',
        'DNI', '33789012',
        '+5491155500002', 'maria.garcia@odontoagenda.test',
        '{"preferred_time_of_day": "Tarde", "communication_channel": "Email"}',
        NOW(), NOW(), 1
    ),
    (
        'c1000000-0000-0000-0000-000000000003',
        'b1000000-0000-0000-0000-000000000012',
        'Active',
        'Carlos Alberto Rodríguez', '1978-11-08', 'M',
        'DNI', '22456789',
        '+5491155500003', 'carlos.rodriguez@odontoagenda.test',
        '{"preferred_time_of_day": "Cualquiera", "communication_channel": "WhatsApp"}',
        NOW(), NOW(), 1
    );

-- Actualizar linked_id en iam.users para los pacientes
UPDATE iam.users SET linked_id = 'c1000000-0000-0000-0000-000000000001'
WHERE id = 'b1000000-0000-0000-0000-000000000010';

UPDATE iam.users SET linked_id = 'c1000000-0000-0000-0000-000000000002'
WHERE id = 'b1000000-0000-0000-0000-000000000011';

UPDATE iam.users SET linked_id = 'c1000000-0000-0000-0000-000000000003'
WHERE id = 'b1000000-0000-0000-0000-000000000012';

-- Historial odontológico inicial (1:1 con cada paciente)
INSERT INTO patient.dental_history_summaries (
    id, patient_id, risk_level, visit_count, main_treatments, updated_at
) VALUES
    (gen_random_uuid(), 'c1000000-0000-0000-0000-000000000001', 'Bajo',   0, '[]', NOW()),
    (gen_random_uuid(), 'c1000000-0000-0000-0000-000000000002', 'Medio',  2, '[]', NOW()),
    (gen_random_uuid(), 'c1000000-0000-0000-0000-000000000003', 'Bajo',   0, '[]', NOW());

-- Coberturas de los pacientes
INSERT INTO patient.patient_coverages (
    id, patient_id, coverage_type, status,
    agreement_id, provider_name, plan_code, membership_number,
    valid_from, co_pay_percent,
    created_at, updated_at, created_by
) VALUES
    -- Juan: OSDE 210
    (gen_random_uuid(),
     'c1000000-0000-0000-0000-000000000001',
     'ObraSocial', 'Active',
     NULL, 'OSDE', 'OSDE-210', '210-0123456789',
     '2024-01-01', 20,
     NOW(), NOW(), 'b1000000-0000-0000-0000-000000000002'),

    -- María: Privado (sin convenio)
    (gen_random_uuid(),
     'c1000000-0000-0000-0000-000000000002',
     'Privado', 'Active',
     NULL, '', '', '',
     '2024-01-01', NULL,
     NOW(), NOW(), 'b1000000-0000-0000-0000-000000000002'),

    -- Carlos: Swiss Medical
    (gen_random_uuid(),
     'c1000000-0000-0000-0000-000000000003',
     'PrepagaExterna', 'Active',
     NULL, 'Swiss Medical', 'SMG-Plan3', 'SMG-987654321',
     '2023-06-01', 30,
     NOW(), NOW(), 'b1000000-0000-0000-0000-000000000002');

-- Alerta médica de prueba (Carlos es hipertenso)
INSERT INTO patient.medical_alerts (
    id, patient_id, alert_type, severity,
    description, is_self_reported, is_active,
    created_by, created_at
) VALUES (
    gen_random_uuid(),
    'c1000000-0000-0000-0000-000000000003',
    'Condición', 'Warning',
    'Paciente hipertenso bajo medicación (Enalapril 10mg/día). Consultar antes de anestesia local.',
    false, true,
    'b1000000-0000-0000-0000-000000000002',
    NOW()
);

-- ── 4. PROFESIONALES ─────────────────────────────────────────────
-- Especialidad 1: Odontología General
-- Especialidad 2: Ortodoncia
-- Especialidad 3: Endodoncia

INSERT INTO professional.professionals (
    id, status,
    full_name, doc_type, doc_number, email, phone, bio,
    default_durations,
    created_at, updated_at, version
) VALUES
    (
        'd1000000-0000-0000-0000-000000000001',
        'Active',
        'Dra. Laura Martínez', 'DNI', '24567890',
        'lmartinez@odontoagenda.test', '+5491144440001',
        'Odontóloga generalista con 12 años de experiencia. Especializada en pacientes pediátricos y adultos mayores.',
        -- duración default por procedimiento (minutos totales = duración + buffer)
        '{
            "ODONTOLOGIA_GENERAL": {"procedure_code": "ODONTOLOGIA_GENERAL", "minutes": 30, "buffer_minutes": 10},
            "BLANQUEAMIENTO":       {"procedure_code": "BLANQUEAMIENTO",       "minutes": 60, "buffer_minutes": 10}
        }',
        NOW(), NOW(), 1
    ),
    (
        'd1000000-0000-0000-0000-000000000002',
        'Active',
        'Dr. Sebastián Torres', 'DNI', '29876543',
        'storres@odontoagenda.test', '+5491144440002',
        'Ortodoncista con formación en técnica Damon y ortodoncia invisible (Invisalign). Atiende adultos y adolescentes.',
        '{
            "ORTODONCIA": {"procedure_code": "ORTODONCIA", "minutes": 45, "buffer_minutes": 15}
        }',
        NOW(), NOW(), 1
    ),
    (
        'd1000000-0000-0000-0000-000000000003',
        'Active',
        'Dra. Ana Fernández', 'DNI', '31234567',
        'afernandez@odontoagenda.test', '+5491144440003',
        'Endodoncista certificada. Especialista en tratamiento de conductos con tecnología rotativa y microscopia.',
        '{
            "ENDODONCIA":           {"procedure_code": "ENDODONCIA",           "minutes": 90, "buffer_minutes": 15},
            "ODONTOLOGIA_GENERAL":  {"procedure_code": "ODONTOLOGIA_GENERAL",  "minutes": 30, "buffer_minutes": 10}
        }',
        NOW(), NOW(), 1
    );

-- ── 5. MATRÍCULAS PROFESIONALES ───────────────────────────────────

INSERT INTO professional.licenses (
    id, professional_id,
    specialty_code, specialty_name,
    license_number, issuing_body,
    issued_at, expires_at, status,
    created_at, updated_at
) VALUES
    -- Dra. Martínez — Odontología General + Blanqueamiento
    (gen_random_uuid(), 'd1000000-0000-0000-0000-000000000001',
     'ODONTOLOGIA_GENERAL', 'Odontología General',
     'MN-24567', 'Colegio de Odontólogos de CABA',
     '2012-03-01', NULL, 'Active',
     NOW(), NOW()),

    (gen_random_uuid(), 'd1000000-0000-0000-0000-000000000001',
     'BLANQUEAMIENTO', 'Blanqueamiento Dental',
     'MN-24567-B', 'Colegio de Odontólogos de CABA',
     '2015-06-15', '2027-06-15', 'Active',
     NOW(), NOW()),

    -- Dr. Torres — Ortodoncia
    (gen_random_uuid(), 'd1000000-0000-0000-0000-000000000002',
     'ORTODONCIA', 'Ortodoncia y Ortopedia Dentomaxilofacial',
     'MN-29876', 'Colegio de Odontólogos de CABA',
     '2018-08-01', NULL, 'Active',
     NOW(), NOW()),

    -- Dra. Fernández — Endodoncia + Odontología General
    (gen_random_uuid(), 'd1000000-0000-0000-0000-000000000003',
     'ENDODONCIA', 'Endodoncia',
     'MN-31234', 'Colegio de Odontólogos de CABA',
     '2020-04-01', '2026-04-01', 'Active',
     NOW(), NOW()),

    (gen_random_uuid(), 'd1000000-0000-0000-0000-000000000003',
     'ODONTOLOGIA_GENERAL', 'Odontología General',
     'MN-31234-G', 'Colegio de Odontólogos de CABA',
     '2019-04-01', NULL, 'Active',
     NOW(), NOW());

-- ── 6. ASIGNACIONES A SEDE ────────────────────────────────────────
-- clinic_id = a1000000-0000-0000-0000-000000000001 (Sede Central)
--
-- Horarios (weekday: 0=Dom, 1=Lun, 2=Mar, 3=Mié, 4=Jue, 5=Vie, 6=Sáb)
--
-- Dra. Martínez: Lun/Mié/Vie 08:00-13:00 y 15:00-18:00
-- Dr. Torres:    Mar/Jue     09:00-14:00
-- Dra. Fernández: Lun/Mar/Mié/Jue/Vie 08:00-12:00

INSERT INTO professional.clinic_assignments (
    id, professional_id, clinic_id, status,
    assigned_specialties, weekly_schedule, exception_days,
    procedure_durations, assigned_from, assigned_by,
    created_at, updated_at
) VALUES
    -- Dra. Martínez — Odontología General + Blanqueamiento
    (
        'e1000000-0000-0000-0000-000000000001',
        'd1000000-0000-0000-0000-000000000001',
        'a1000000-0000-0000-0000-000000000001',
        'Active',
        '["ODONTOLOGIA_GENERAL", "BLANQUEAMIENTO"]',
        '[
            {"weekday": 1, "start_hour": 8,  "start_min": 0, "end_hour": 13, "end_min": 0},
            {"weekday": 1, "start_hour": 15, "start_min": 0, "end_hour": 18, "end_min": 0},
            {"weekday": 3, "start_hour": 8,  "start_min": 0, "end_hour": 13, "end_min": 0},
            {"weekday": 3, "start_hour": 15, "start_min": 0, "end_hour": 18, "end_min": 0},
            {"weekday": 5, "start_hour": 8,  "start_min": 0, "end_hour": 13, "end_min": 0},
            {"weekday": 5, "start_hour": 15, "start_min": 0, "end_hour": 18, "end_min": 0}
        ]',
        '[]',
        '{
            "ODONTOLOGIA_GENERAL": {"procedure_code": "ODONTOLOGIA_GENERAL", "minutes": 30, "buffer_minutes": 10},
            "BLANQUEAMIENTO":       {"procedure_code": "BLANQUEAMIENTO",       "minutes": 60, "buffer_minutes": 10}
        }',
        '2024-01-01',
        'b1000000-0000-0000-0000-000000000002',
        NOW(), NOW()
    ),

    -- Dr. Torres — Ortodoncia
    (
        'e1000000-0000-0000-0000-000000000002',
        'd1000000-0000-0000-0000-000000000002',
        'a1000000-0000-0000-0000-000000000001',
        'Active',
        '["ORTODONCIA"]',
        '[
            {"weekday": 2, "start_hour": 9, "start_min": 0, "end_hour": 14, "end_min": 0},
            {"weekday": 4, "start_hour": 9, "start_min": 0, "end_hour": 14, "end_min": 0}
        ]',
        '[]',
        '{
            "ORTODONCIA": {"procedure_code": "ORTODONCIA", "minutes": 45, "buffer_minutes": 15}
        }',
        '2024-03-01',
        'b1000000-0000-0000-0000-000000000002',
        NOW(), NOW()
    ),

    -- Dra. Fernández — Endodoncia + Odontología General
    (
        'e1000000-0000-0000-0000-000000000003',
        'd1000000-0000-0000-0000-000000000003',
        'a1000000-0000-0000-0000-000000000001',
        'Active',
        '["ENDODONCIA", "ODONTOLOGIA_GENERAL"]',
        '[
            {"weekday": 1, "start_hour": 8,  "start_min": 0, "end_hour": 12, "end_min": 0},
            {"weekday": 2, "start_hour": 8,  "start_min": 0, "end_hour": 12, "end_min": 0},
            {"weekday": 3, "start_hour": 8,  "start_min": 0, "end_hour": 12, "end_min": 0},
            {"weekday": 4, "start_hour": 8,  "start_min": 0, "end_hour": 12, "end_min": 0},
            {"weekday": 5, "start_hour": 8,  "start_min": 0, "end_hour": 12, "end_min": 0}
        ]',
        '[]',
        '{
            "ENDODONCIA":          {"procedure_code": "ENDODONCIA",          "minutes": 90, "buffer_minutes": 15},
            "ODONTOLOGIA_GENERAL": {"procedure_code": "ODONTOLOGIA_GENERAL", "minutes": 30, "buffer_minutes": 10}
        }',
        '2024-02-01',
        'b1000000-0000-0000-0000-000000000002',
        NOW(), NOW()
    );

-- ── 7. AVAILABILITY SCHEDULES ─────────────────────────────────────
-- Proyección de disponibilidad en scheduling.
-- Replica el weekly_schedule de la asignación + duraciones.
-- booked_slots y blocked_slots vacíos (sin reservas previas).

INSERT INTO scheduling.availability_schedules (
    id, professional_id, clinic_id,
    working_hours, exception_days, blocked_slots, booked_slots,
    procedure_durations,
    is_active, updated_at, version
) VALUES
    -- Dra. Martínez
    (
        'f1000000-0000-0000-0000-000000000001',
        'd1000000-0000-0000-0000-000000000001',
        'a1000000-0000-0000-0000-000000000001',
        '[
            {"weekday": 1, "start_hour": 8,  "start_min": 0, "end_hour": 13, "end_min": 0},
            {"weekday": 1, "start_hour": 15, "start_min": 0, "end_hour": 18, "end_min": 0},
            {"weekday": 3, "start_hour": 8,  "start_min": 0, "end_hour": 13, "end_min": 0},
            {"weekday": 3, "start_hour": 15, "start_min": 0, "end_hour": 18, "end_min": 0},
            {"weekday": 5, "start_hour": 8,  "start_min": 0, "end_hour": 13, "end_min": 0},
            {"weekday": 5, "start_hour": 15, "start_min": 0, "end_hour": 18, "end_min": 0}
        ]',
        '[]', '[]', '[]',
        '{"ODONTOLOGIA_GENERAL": 40, "BLANQUEAMIENTO": 70}',
        true, NOW(), 1
    ),

    -- Dr. Torres
    (
        'f1000000-0000-0000-0000-000000000002',
        'd1000000-0000-0000-0000-000000000002',
        'a1000000-0000-0000-0000-000000000001',
        '[
            {"weekday": 2, "start_hour": 9, "start_min": 0, "end_hour": 14, "end_min": 0},
            {"weekday": 4, "start_hour": 9, "start_min": 0, "end_hour": 14, "end_min": 0}
        ]',
        '[]', '[]', '[]',
        '{"ORTODONCIA": 60}',
        true, NOW(), 1
    ),

    -- Dra. Fernández
    (
        'f1000000-0000-0000-0000-000000000003',
        'd1000000-0000-0000-0000-000000000003',
        'a1000000-0000-0000-0000-000000000001',
        '[
            {"weekday": 1, "start_hour": 8, "start_min": 0, "end_hour": 12, "end_min": 0},
            {"weekday": 2, "start_hour": 8, "start_min": 0, "end_hour": 12, "end_min": 0},
            {"weekday": 3, "start_hour": 8, "start_min": 0, "end_hour": 12, "end_min": 0},
            {"weekday": 4, "start_hour": 8, "start_min": 0, "end_hour": 12, "end_min": 0},
            {"weekday": 5, "start_hour": 8, "start_min": 0, "end_hour": 12, "end_min": 0}
        ]',
        '[]', '[]', '[]',
        '{"ENDODONCIA": 105, "ODONTOLOGIA_GENERAL": 40}',
        true, NOW(), 1
    );

COMMIT;

-- ── 8. RESUMEN DE DATOS CREADOS ───────────────────────────────────

DO $$
BEGIN
    RAISE NOTICE '============================================';
    RAISE NOTICE 'DATOS DE PRUEBA INSERTADOS CORRECTAMENTE';
    RAISE NOTICE '============================================';
    RAISE NOTICE '';
    RAISE NOTICE '── CREDENCIALES (password: Test1234) ────────';
    RAISE NOTICE 'recepcionista@odontoagenda.test  → rol: recepcionista';
    RAISE NOTICE 'admin@odontoagenda.test          → rol: admin_sucursal';
    RAISE NOTICE 'juan.perez@odontoagenda.test     → rol: paciente (OSDE 210, 20%% copago)';
    RAISE NOTICE 'maria.garcia@odontoagenda.test   → rol: paciente (Privado)';
    RAISE NOTICE 'carlos.rodriguez@odontoagenda.test → rol: paciente (Swiss Medical, 30%% copago)';
    RAISE NOTICE '';
    RAISE NOTICE '── PROFESIONALES ────────────────────────────';
    RAISE NOTICE 'Dra. Martínez  → ODONTOLOGIA_GENERAL + BLANQUEAMIENTO';
    RAISE NOTICE '               Lun/Mié/Vie 08-13 y 15-18';
    RAISE NOTICE '               30min consulta + 10min buffer';
    RAISE NOTICE '';
    RAISE NOTICE 'Dr. Torres     → ORTODONCIA';
    RAISE NOTICE '               Mar/Jue 09-14';
    RAISE NOTICE '               45min consulta + 15min buffer';
    RAISE NOTICE '';
    RAISE NOTICE 'Dra. Fernández → ENDODONCIA + ODONTOLOGIA_GENERAL';
    RAISE NOTICE '               Lun a Vie 08-12';
    RAISE NOTICE '               90min endodoncia + 15min buffer';
    RAISE NOTICE '';
    RAISE NOTICE '── CLINIC ID ────────────────────────────────';
    RAISE NOTICE 'a1000000-0000-0000-0000-000000000001';
    RAISE NOTICE '';
    RAISE NOTICE '── EJEMPLO DE RESERVA (curl) ────────────────';
    RAISE NOTICE 'Primero hacer login y obtener token, luego:';
    RAISE NOTICE 'POST /api/v1/scheduling/appointments';
    RAISE NOTICE '  professional_id: d1000000-0000-0000-0000-000000000001';
    RAISE NOTICE '  clinic_id:       a1000000-0000-0000-0000-000000000001';
    RAISE NOTICE '  procedure_code:  ODONTOLOGIA_GENERAL';
    RAISE NOTICE '  (elegir un lunes/miércoles/viernes en horario 08-13 o 15-18)';
    RAISE NOTICE '============================================';
END $$;

-- ── 9. QUERIES DE VERIFICACIÓN (ejecutar después del seed) ────────
-- Copiar y pegar individualmente para confirmar que todo quedó bien.

/*
-- Verificar usuarios
SELECT email, role, status, linked_id FROM iam.users
WHERE email LIKE '%@odontoagenda.test'
ORDER BY role;

-- Verificar pacientes con cobertura
SELECT p.full_name, p.gender, pc.coverage_type, pc.provider_name, pc.co_pay_percent
FROM patient.patients p
LEFT JOIN patient.patient_coverages pc ON p.id = pc.patient_id AND pc.status = 'Active'
WHERE p.phone LIKE '+5491155500%'
ORDER BY p.full_name;

-- Verificar profesionales con matrículas
SELECT pr.full_name, l.specialty_code, l.license_number, l.expires_at, l.status
FROM professional.professionals pr
JOIN professional.licenses l ON pr.id = l.professional_id
WHERE pr.email LIKE '%@odontoagenda.test'
ORDER BY pr.full_name, l.specialty_code;

-- Verificar availability schedules con slots calculables
SELECT
    pr.full_name,
    jsonb_array_length(s.working_hours) AS dias_atencion,
    s.procedure_durations,
    s.is_active
FROM scheduling.availability_schedules s
JOIN professional.professionals pr ON s.professional_id = pr.id
WHERE pr.email LIKE '%@odontoagenda.test';

-- Simular qué slots estarían disponibles (ejemplo lunes próximo)
-- Reemplazar '2026-06-29' con el próximo lunes real
SELECT
    pr.full_name,
    s.procedure_durations,
    s.working_hours
FROM scheduling.availability_schedules s
JOIN professional.professionals pr ON s.professional_id = pr.id
WHERE pr.id = 'd1000000-0000-0000-0000-000000000001'
  AND s.clinic_id = 'a1000000-0000-0000-0000-000000000001'
  AND s.is_active = true;
*/
