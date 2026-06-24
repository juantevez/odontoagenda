// Package postgres contiene los adaptadores de salida PostgreSQL del bounded context Patient.
// Implementa PatientRepository y CoverageHistoryRepository sobre pgx/v5.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── PatientPostgresRepository ─────────────────────────────────────

type PatientPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPatientPostgresRepository(pool *pgxpool.Pool) *PatientPostgresRepository {
	return &PatientPostgresRepository{pool: pool}
}

// ── Save ─────────────────────────────────────────────────────────

// Save persiste un Patient nuevo en patient.patients.
// Retorna ErrAlreadyExists si el NationalID ya existe (INV-01).
func (r *PatientPostgresRepository) Save(ctx context.Context, p *aggregate.Patient) error {
	prefsJSON, err := marshalPreferences(p.Preferences())
	if err != nil {
		return fmt.Errorf("PatientRepo.Save: marshal preferences: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO patient.patients (
			id, user_id, status,
			full_name, birth_date, gender,
			doc_type, doc_number,
			phone, whatsapp, email,
			emergency_name, emergency_phone,
			preferences,
			created_at, updated_at, created_by, version
		) VALUES (
			$1,  $2,  'Active',
			$3,  $4,  $5,
			$6,  $7,
			$8,  $9,  $10,
			$11, $12,
			$13,
			$14, $15, $16, $17
		)`,
		p.ID(), p.UserID(),
		p.FullName().String(), p.BirthDate().Time(), string(p.Gender()),
		string(p.NationalID().Type), p.NationalID().Number,
		contactPhone(p), contactWhatsApp(p), contactEmail(p),
		p.ContactInfo().EmergencyName, contactEmergencyPhone(p),
		prefsJSON,
		p.CreatedAt(), p.UpdatedAt(), p.CreatedBy(), p.Version(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return sharederrors.NewAlreadyExists("Patient", "national_id",
				p.NationalID().Number)
		}
		return fmt.Errorf("PatientRepo.Save: %w", err)
	}

	// Crear DentalHistorySummary inicial (relación 1:1).
	_, err = r.pool.Exec(ctx, `
		INSERT INTO patient.dental_history_summaries
			(id, patient_id, risk_level, visit_count, main_treatments, updated_at)
		VALUES
			($1, $2, 'Bajo', 0, '[]'::jsonb, now())
		ON CONFLICT (patient_id) DO NOTHING`,
		uuid.New(), p.ID(),
	)
	if err != nil {
		return fmt.Errorf("PatientRepo.Save: insert dental history: %w", err)
	}

	return nil
}

// ── Update ────────────────────────────────────────────────────────

// Update persiste cambios con optimistic locking (version).
// Retorna ErrConflict si la versión en BD difiere.
func (r *PatientPostgresRepository) Update(ctx context.Context, p *aggregate.Patient) error {
	prefsJSON, err := marshalPreferences(p.Preferences())
	if err != nil {
		return fmt.Errorf("PatientRepo.Update: marshal preferences: %w", err)
	}

	newVersion := p.Version() + 1

	result, err := r.pool.Exec(ctx, `
		UPDATE patient.patients SET
			full_name       = $1,
			phone           = $2,
			whatsapp        = $3,
			email           = $4,
			emergency_name  = $5,
			emergency_phone = $6,
			preferences     = $7,
			updated_at      = $8,
			version         = $9
		WHERE id = $10 AND version = $11`,
		p.FullName().String(),
		contactPhone(p), contactWhatsApp(p), contactEmail(p),
		p.ContactInfo().EmergencyName, contactEmergencyPhone(p),
		prefsJSON,
		time.Now().UTC(), newVersion,
		p.ID(), p.Version(),
	)
	if err != nil {
		return fmt.Errorf("PatientRepo.Update: %w", err)
	}
	if result.RowsAffected() == 0 {
		return sharederrors.NewConflict(
			fmt.Sprintf("Patient '%s' modificado concurrentemente (versión %d)", p.ID(), p.Version()),
			nil,
		)
	}

	// Persistir coberturas (upsert individual).
	for _, cov := range p.Coverages() {
		if err := r.upsertCoverage(ctx, cov); err != nil {
			return fmt.Errorf("PatientRepo.Update: upsert coverage: %w", err)
		}
	}

	// Persistir alertas médicas (insert if not exists).
	for _, alert := range p.MedicalAlerts() {
		if err := r.upsertAlert(ctx, p.ID(), alert); err != nil {
			return fmt.Errorf("PatientRepo.Update: upsert alert: %w", err)
		}
	}

	// Persistir historial odontológico.
	if history := p.DentalHistory(); history != nil {
		if err := r.updateDentalHistory(ctx, p.ID(), history); err != nil {
			return fmt.Errorf("PatientRepo.Update: update dental history: %w", err)
		}
	}

	return nil
}

// ── FindByID ──────────────────────────────────────────────────────

// FindByID carga el aggregate completo (Patient + coberturas + alertas + historial).
func (r *PatientPostgresRepository) FindByID(ctx context.Context, id sharedtypes.PatientID) (*aggregate.Patient, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id, user_id, status,
			full_name, birth_date, gender,
			doc_type, doc_number,
			phone, whatsapp, email,
			emergency_name, emergency_phone,
			preferences,
			created_at, updated_at, created_by, version
		FROM patient.patients
		WHERE id = $1 AND status != 'Archived'`, id)

	p, err := r.scanPatient(ctx, row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, sharederrors.NewNotFound("Patient", id.String())
		}
		return nil, fmt.Errorf("PatientRepo.FindByID: %w", err)
	}
	return p, nil
}

// ── FindByNationalID ──────────────────────────────────────────────

func (r *PatientPostgresRepository) FindByNationalID(ctx context.Context, nationalID sharedvo.NationalID) (*aggregate.Patient, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id, user_id, status,
			full_name, birth_date, gender,
			doc_type, doc_number,
			phone, whatsapp, email,
			emergency_name, emergency_phone,
			preferences,
			created_at, updated_at, created_by, version
		FROM patient.patients
		WHERE doc_type = $1 AND doc_number = $2 AND status != 'Archived'`,
		string(nationalID.Type), nationalID.Number)

	p, err := r.scanPatient(ctx, row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, sharederrors.NewNotFound("Patient", nationalID.String())
		}
		return nil, fmt.Errorf("PatientRepo.FindByNationalID: %w", err)
	}
	return p, nil
}

// ── FindByUserID ──────────────────────────────────────────────────

func (r *PatientPostgresRepository) FindByUserID(ctx context.Context, userID sharedtypes.UserID) (*aggregate.Patient, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id, user_id, status,
			full_name, birth_date, gender,
			doc_type, doc_number,
			phone, whatsapp, email,
			emergency_name, emergency_phone,
			preferences,
			created_at, updated_at, created_by, version
		FROM patient.patients
		WHERE user_id = $1 AND status != 'Archived'`, userID)

	p, err := r.scanPatient(ctx, row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, sharederrors.NewNotFound("Patient", userID.String())
		}
		return nil, fmt.Errorf("PatientRepo.FindByUserID: %w", err)
	}
	return p, nil
}

// ── Search ────────────────────────────────────────────────────────

// Search realiza búsqueda fuzzy por nombre, documento o teléfono usando pg_trgm.
func (r *PatientPostgresRepository) Search(ctx context.Context, query string, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	if query == "" {
		return r.listAll(ctx, page)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT
			id, user_id, status,
			full_name, birth_date, gender,
			doc_type, doc_number,
			phone, whatsapp, email,
			emergency_name, emergency_phone,
			preferences,
			created_at, updated_at, created_by, version
		FROM patient.patients
		WHERE status != 'Archived'
		  AND (
			full_name ILIKE $1
			OR doc_number ILIKE $1
			OR phone ILIKE $1
			OR similarity(full_name, $2) > 0.3
		  )
		ORDER BY similarity(full_name, $2) DESC, full_name ASC
		LIMIT $3 OFFSET $4`,
		"%"+query+"%", query, page.Limit, page.Offset)
	if err != nil {
		return sharedtypes.PagedResult[*aggregate.Patient]{}, fmt.Errorf("PatientRepo.Search: %w", err)
	}
	defer rows.Close()

	patients, err := r.scanPatientRows(ctx, rows)
	if err != nil {
		return sharedtypes.PagedResult[*aggregate.Patient]{}, err
	}

	// Count total.
	var total int64
	_ = r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM patient.patients
		WHERE status != 'Archived'
		  AND (
			full_name ILIKE $1
			OR doc_number ILIKE $1
			OR phone ILIKE $1
			OR similarity(full_name, $2) > 0.3
		  )`, "%"+query+"%", query).Scan(&total)

	return sharedtypes.NewPagedResult(patients, total, page), nil
}

// listAll devuelve todos los pacientes activos paginados (cuando query está vacío).
func (r *PatientPostgresRepository) listAll(ctx context.Context, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id, user_id, status,
			full_name, birth_date, gender,
			doc_type, doc_number,
			phone, whatsapp, email,
			emergency_name, emergency_phone,
			preferences,
			created_at, updated_at, created_by, version
		FROM patient.patients
		WHERE status != 'Archived'
		ORDER BY full_name ASC
		LIMIT $1 OFFSET $2`, page.Limit, page.Offset)
	if err != nil {
		return sharedtypes.PagedResult[*aggregate.Patient]{}, fmt.Errorf("PatientRepo.listAll: %w", err)
	}
	defer rows.Close()

	patients, err := r.scanPatientRows(ctx, rows)
	if err != nil {
		return sharedtypes.PagedResult[*aggregate.Patient]{}, err
	}

	var total int64
	_ = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM patient.patients WHERE status != 'Archived'`).Scan(&total)

	return sharedtypes.NewPagedResult(patients, total, page), nil
}

// ── FindNearClinic ────────────────────────────────────────────────

func (r *PatientPostgresRepository) FindNearClinic(ctx context.Context, _ sharedtypes.ClinicID, radiusMeters float64, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	// MVP: devuelve todos los activos paginados sin filtro geoespacial.
	// Implementación real requiere coordenadas de la sede.
	_ = radiusMeters
	return r.listAll(ctx, page)
}

// ── ExistsByNationalID ────────────────────────────────────────────

func (r *PatientPostgresRepository) ExistsByNationalID(ctx context.Context, nationalID sharedvo.NationalID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM patient.patients
			WHERE doc_type = $1 AND doc_number = $2 AND status != 'Archived'
		)`, string(nationalID.Type), nationalID.Number).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("PatientRepo.ExistsByNationalID: %w", err)
	}
	return exists, nil
}

// ── FindPotentialDuplicates ───────────────────────────────────────

// FindPotentialDuplicates busca candidatos duplicados por nombre y teléfono.
// Usa trigram similarity de pg_trgm para tolerar errores de escritura.
func (r *PatientPostgresRepository) FindPotentialDuplicates(ctx context.Context, fullName string, phone string) ([]*aggregate.Patient, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id, user_id, status,
			full_name, birth_date, gender,
			doc_type, doc_number,
			phone, whatsapp, email,
			emergency_name, emergency_phone,
			preferences,
			created_at, updated_at, created_by, version
		FROM patient.patients
		WHERE status != 'Archived'
		  AND (
			similarity(full_name, $1) > 0.5
			OR (phone = $2 AND $2 != '')
		  )
		ORDER BY similarity(full_name, $1) DESC
		LIMIT 10`, fullName, phone)
	if err != nil {
		return nil, fmt.Errorf("PatientRepo.FindPotentialDuplicates: %w", err)
	}
	defer rows.Close()
	return r.scanPatientRows(ctx, rows)
}

// ── Archive ───────────────────────────────────────────────────────

// Archive realiza la baja lógica: status = 'Archived'.
func (r *PatientPostgresRepository) Archive(ctx context.Context, id sharedtypes.PatientID, reason string, by sharedtypes.UserID) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE patient.patients
		SET status = 'Archived', updated_at = now()
		WHERE id = $1 AND status = 'Active'`, id)
	if err != nil {
		return fmt.Errorf("PatientRepo.Archive: %w", err)
	}
	if result.RowsAffected() == 0 {
		return sharederrors.NewNotFound("Patient", id.String())
	}
	return nil
}

// ── scan helpers ──────────────────────────────────────────────────

// rowScanner abstrae Scan de pgx.Row (QueryRow) y pgx.Rows (Query).
type rowScanner interface {
	Scan(dest ...any) error
}

func (r *PatientPostgresRepository) scanPatient(ctx context.Context, row rowScanner) (*aggregate.Patient, error) {
	var (
		id             uuid.UUID
		userID         *uuid.UUID
		status         string
		fullName       string
		birthDate      time.Time
		gender         string
		docType        string
		docNumber      string
		phone          string
		whatsapp       *string
		email          *string
		emergencyName  *string
		emergencyPhone *string
		prefsJSON      []byte
		createdAt      time.Time
		updatedAt      time.Time
		createdBy      *uuid.UUID
		version        int64
	)

	err := row.Scan(
		&id, &userID, &status,
		&fullName, &birthDate, &gender,
		&docType, &docNumber,
		&phone, &whatsapp, &email,
		&emergencyName, &emergencyPhone,
		&prefsJSON,
		&createdAt, &updatedAt, &createdBy, &version,
	)
	if err != nil {
		return nil, err
	}

	fullNameVO, err := sharedvo.NewFullName(fullName)
	if err != nil {
		return nil, fmt.Errorf("scanPatient: full_name: %w", err)
	}
	birthDateVO, err := valueobject.NewBirthDateFromTime(birthDate)
	if err != nil {
		return nil, fmt.Errorf("scanPatient: birth_date: %w", err)
	}
	genderVO, err := valueobject.ParseGender(gender)
	if err != nil {
		return nil, fmt.Errorf("scanPatient: gender: %w", err)
	}
	nationalIDVO, err := sharedvo.NewNationalID(sharedvo.DocumentType(docType), docNumber)
	if err != nil {
		return nil, fmt.Errorf("scanPatient: national_id: %w", err)
	}
	phoneVO, err := sharedvo.NewPhoneNumber(phone)
	if err != nil {
		return nil, fmt.Errorf("scanPatient: phone: %w", err)
	}

	contactInfo := aggregate.ContactInfo{Phone: phoneVO}
	if email != nil && *email != "" {
		emailVO, e := sharedvo.NewEmail(*email)
		if e == nil {
			contactInfo.Email = &emailVO
		}
	}
	if whatsapp != nil && *whatsapp != "" {
		wpVO, e := sharedvo.NewPhoneNumber(*whatsapp)
		if e == nil {
			contactInfo.WhatsApp = &wpVO
		}
	}
	if emergencyName != nil {
		contactInfo.EmergencyName = *emergencyName
	}
	if emergencyPhone != nil && *emergencyPhone != "" {
		epVO, e := sharedvo.NewPhoneNumber(*emergencyPhone)
		if e == nil {
			contactInfo.EmergencyPhone = &epVO
		}
	}

	prefs, err := unmarshalPreferences(prefsJSON)
	if err != nil {
		prefs = aggregate.PatientPreferences{
			PreferredTimeOfDay:   valueobject.TimeOfDayAny,
			CommunicationChannel: valueobject.ChannelWhatsApp,
		}
	}

	// Cargar coberturas.
	coverages, err := r.loadCoverages(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("scanPatient: load coverages: %w", err)
	}

	// Cargar alertas médicas.
	alerts, err := r.loadAlerts(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("scanPatient: load alerts: %w", err)
	}

	// Cargar historial odontológico.
	history, err := r.loadDentalHistory(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("scanPatient: load dental history: %w", err)
	}
	// Si no existe todavía (race entre Save e INSERT del summary), crear uno vacío
	// para evitar nil pointer en los query handlers que llaman p.DentalHistory().
	if history == nil {
		history = aggregate.ReconstituteDentalHistory(
			uuid.New(), id, nil,
			valueobject.RiskLevelLow, 0,
			[]aggregate.TreatmentSummary{},
			time.Now().UTC(), "",
		)
	}

	return aggregate.Reconstitute(
		id, userID,
		fullNameVO, birthDateVO, genderVO, nationalIDVO,
		contactInfo, nil,
		coverages, alerts, history, prefs,
		createdAt, updatedAt, createdBy, version,
	), nil
}

func (r *PatientPostgresRepository) scanPatientRows(ctx context.Context, rows pgx.Rows) ([]*aggregate.Patient, error) {
	var patients []*aggregate.Patient
	for rows.Next() {
		p, err := r.scanPatient(ctx, rows)
		if err != nil {
			return nil, fmt.Errorf("PatientRepo.scanPatientRows: %w", err)
		}
		patients = append(patients, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("PatientRepo.scanPatientRows: rows error: %w", err)
	}
	return patients, nil
}

// ── Coberturas ────────────────────────────────────────────────────

func (r *PatientPostgresRepository) loadCoverages(ctx context.Context, patientID uuid.UUID) ([]*coverage.PatientCoverage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id, patient_id, coverage_type, status,
			agreement_id, provider_name, plan_code, membership_number,
			valid_from, valid_until,
			co_pay_percent, co_pay_fixed_cents,
			created_at, updated_at, created_by
		FROM patient.patient_coverages
		WHERE patient_id = $1
		ORDER BY created_at ASC`, patientID)
	if err != nil {
		return nil, fmt.Errorf("loadCoverages: %w", err)
	}
	defer rows.Close()

	var coverages []*coverage.PatientCoverage
	for rows.Next() {
		var (
			id             uuid.UUID
			pid            uuid.UUID
			covType        string
			status         string
			agreementID    *uuid.UUID
			providerName   *string
			planCode       *string
			membershipNum  *string
			validFrom      time.Time
			validUntil     *time.Time
			coPayPercent   *int
			coPayFixed     *int64
			createdAt      time.Time
			updatedAt      time.Time
			createdBy      uuid.UUID
		)
		if err := rows.Scan(
			&id, &pid, &covType, &status,
			&agreementID, &providerName, &planCode, &membershipNum,
			&validFrom, &validUntil,
			&coPayPercent, &coPayFixed,
			&createdAt, &updatedAt, &createdBy,
		); err != nil {
			return nil, fmt.Errorf("loadCoverages scan: %w", err)
		}

		ct, err := valueobject.ParseCoverageType(covType)
		if err != nil {
			continue // dato desconocido: skip
		}

		cov, err := coverage.NewPatientCoverage(
			patientID, ct,
			agreementID,
			strOrEmpty(providerName), strOrEmpty(planCode), strOrEmpty(membershipNum),
			validFrom, validUntil,
			createdBy,
		)
		if err != nil {
			continue
		}
		if coPayPercent != nil {
			_ = cov.SetCoPayPercent(*coPayPercent)
		} else if coPayFixed != nil {
			_ = cov.SetCoPayFixed(*coPayFixed)
		}
		if status == "Suspended" {
			_ = cov.Suspend("reconstituted from DB", uuid.Nil)
		} else if status == "Expired" {
			cov.Expire()
		}
		coverages = append(coverages, cov)
	}
	return coverages, rows.Err()
}

func (r *PatientPostgresRepository) upsertCoverage(ctx context.Context, cov *coverage.PatientCoverage) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO patient.patient_coverages (
			id, patient_id, coverage_type, status,
			agreement_id, provider_name, plan_code, membership_number,
			valid_from, valid_until,
			co_pay_percent, co_pay_fixed_cents,
			created_at, updated_at, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (id) DO UPDATE SET
			status           = EXCLUDED.status,
			co_pay_percent   = EXCLUDED.co_pay_percent,
			co_pay_fixed_cents = EXCLUDED.co_pay_fixed_cents,
			valid_until      = EXCLUDED.valid_until,
			updated_at       = EXCLUDED.updated_at`,
		cov.ID(), cov.PatientID(), string(cov.CoverageType()), string(cov.Status()),
		cov.AgreementID(),
		nullableStr(cov.ProviderName()), nullableStr(cov.PlanCode()), nullableStr(cov.MembershipNumber()),
		cov.ValidFrom(), cov.ValidUntil(),
		cov.CoPayPercent(), cov.CoPayFixed(),
		cov.CreatedAt(), cov.UpdatedAt(), cov.CreatedBy(),
	)
	return err
}

// ── Alertas médicas ───────────────────────────────────────────────

func (r *PatientPostgresRepository) loadAlerts(ctx context.Context, patientID uuid.UUID) ([]aggregate.MedicalAlert, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id, patient_id, alert_type, severity,
			description, is_self_reported, is_active,
			created_by, created_at, revoked_at, revoked_by
		FROM patient.medical_alerts
		WHERE patient_id = $1
		ORDER BY created_at ASC`, patientID)
	if err != nil {
		return nil, fmt.Errorf("loadAlerts: %w", err)
	}
	defer rows.Close()

	// MedicalAlert es un tipo concreto con campos privados.
	// Usamos ReconstituteMedicalAlert (helper que exponemos abajo).
	var alerts []aggregate.MedicalAlert
	for rows.Next() {
		var (
			id             uuid.UUID
			pid            uuid.UUID
			alertType      string
			severity       string
			description    string
			isSelfReported bool
			isActive       bool
			createdBy      uuid.UUID
			createdAt      time.Time
			revokedAt      *time.Time
			revokedBy      *uuid.UUID
		)
		if err := rows.Scan(
			&id, &pid, &alertType, &severity,
			&description, &isSelfReported, &isActive,
			&createdBy, &createdAt, &revokedAt, &revokedBy,
		); err != nil {
			return nil, fmt.Errorf("loadAlerts scan: %w", err)
		}

		at, err := valueobject.ParseAlertType(alertType)
		if err != nil {
			continue
		}
		sev, err := valueobject.ParseAlertSeverity(severity)
		if err != nil {
			continue
		}

		alerts = append(alerts, aggregate.ReconstituteMedicalAlert(
			id, at, sev, description, isSelfReported, isActive,
			createdBy, createdAt, revokedAt, revokedBy,
		))
	}
	return alerts, rows.Err()
}

func (r *PatientPostgresRepository) upsertAlert(ctx context.Context, patientID uuid.UUID, a aggregate.MedicalAlert) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO patient.medical_alerts (
			id, patient_id, alert_type, severity,
			description, is_self_reported, is_active,
			created_by, created_at, revoked_at, revoked_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			is_active  = EXCLUDED.is_active,
			revoked_at = EXCLUDED.revoked_at,
			revoked_by = EXCLUDED.revoked_by`,
		a.ID(), patientID, string(a.AlertType()), string(a.Severity()),
		a.Description(), a.IsSelfReported(), a.IsActive(),
		a.CreatedBy(), a.CreatedAt(), a.RevokedAt(), nil,
	)
	return err
}

// ── Historial odontológico ────────────────────────────────────────

// treatmentRow es el DTO JSON para main_treatments en BD.
type treatmentRow struct {
	ProcedureCode  string    `json:"procedure_code"`
	Description    string    `json:"description"`
	PerformedAt    time.Time `json:"performed_at"`
	ClinicID       string    `json:"clinic_id"`
	ProfessionalID string    `json:"professional_id"`
}

func (r *PatientPostgresRepository) loadDentalHistory(ctx context.Context, patientID uuid.UUID) (*aggregate.DentalHistorySummary, error) {
	var (
		id              uuid.UUID
		lastVisitDate   *time.Time
		riskLevel       string
		visitCount      int
		treatmentsJSON  []byte
		updatedAt       time.Time
		updatedByEvent  *string
	)

	err := r.pool.QueryRow(ctx, `
		SELECT id, last_visit_date, risk_level, visit_count,
		       main_treatments, updated_at, updated_by_event
		FROM patient.dental_history_summaries
		WHERE patient_id = $1`, patientID).Scan(
		&id, &lastVisitDate, &riskLevel, &visitCount,
		&treatmentsJSON, &updatedAt, &updatedByEvent,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // no existe todavía; se creará en Save
		}
		return nil, fmt.Errorf("loadDentalHistory: %w", err)
	}

	rl, err := valueobject.ParseRiskLevel(riskLevel)
	if err != nil {
		rl = valueobject.RiskLevelLow
	}

	var treatments []treatmentRow
	_ = json.Unmarshal(treatmentsJSON, &treatments)

	summaryTreatments := make([]aggregate.TreatmentSummary, 0, len(treatments))
	for _, t := range treatments {
		summaryTreatments = append(summaryTreatments, aggregate.TreatmentSummary{
			ProcedureCode:  t.ProcedureCode,
			Description:    t.Description,
			PerformedAt:    t.PerformedAt,
			ClinicID:       uuid.MustParse(t.ClinicID),
			ProfessionalID: uuid.MustParse(t.ProfessionalID),
		})
	}

	eventID := ""
	if updatedByEvent != nil {
		eventID = *updatedByEvent
	}

	return aggregate.ReconstituteDentalHistory(
		id, patientID, lastVisitDate, rl, visitCount, summaryTreatments, updatedAt, eventID,
	), nil
}

func (r *PatientPostgresRepository) updateDentalHistory(ctx context.Context, patientID uuid.UUID, h *aggregate.DentalHistorySummary) error {
	treatments := make([]treatmentRow, 0, len(h.MainTreatments()))
	for _, t := range h.MainTreatments() {
		treatments = append(treatments, treatmentRow{
			ProcedureCode:  t.ProcedureCode,
			Description:    t.Description,
			PerformedAt:    t.PerformedAt,
			ClinicID:       t.ClinicID.String(),
			ProfessionalID: t.ProfessionalID.String(),
		})
	}
	treatmentsJSON, err := json.Marshal(treatments)
	if err != nil {
		return fmt.Errorf("updateDentalHistory: marshal treatments: %w", err)
	}

	eventID := h.UpdatedByEvent()

	_, err = r.pool.Exec(ctx, `
		UPDATE patient.dental_history_summaries SET
			last_visit_date  = $1,
			risk_level       = $2,
			visit_count      = $3,
			main_treatments  = $4,
			updated_at       = $5,
			updated_by_event = $6
		WHERE patient_id = $7`,
		h.LastVisitDate(), string(h.RiskLevel()), h.VisitCount(),
		treatmentsJSON, h.UpdatedAt(), nullableStr(eventID),
		patientID,
	)
	return err
}

// ── Preferences serialization ─────────────────────────────────────

type prefsJSON struct {
	PreferredClinicID    *string `json:"preferred_clinic_id,omitempty"`
	PreferredTimeOfDay   string  `json:"preferred_time_of_day"`
	CommunicationChannel string  `json:"communication_channel"`
}

func marshalPreferences(p aggregate.PatientPreferences) ([]byte, error) {
	dto := prefsJSON{
		PreferredTimeOfDay:   string(p.PreferredTimeOfDay),
		CommunicationChannel: string(p.CommunicationChannel),
	}
	if p.PreferredClinicID != nil {
		s := p.PreferredClinicID.String()
		dto.PreferredClinicID = &s
	}
	return json.Marshal(dto)
}

func unmarshalPreferences(data []byte) (aggregate.PatientPreferences, error) {
	var dto prefsJSON
	if err := json.Unmarshal(data, &dto); err != nil {
		return aggregate.PatientPreferences{}, err
	}
	prefs := aggregate.PatientPreferences{
		PreferredTimeOfDay:   valueobject.PreferredTimeOfDay(dto.PreferredTimeOfDay),
		CommunicationChannel: valueobject.CommunicationChannel(dto.CommunicationChannel),
	}
	if dto.PreferredClinicID != nil {
		if id, err := uuid.Parse(*dto.PreferredClinicID); err == nil {
			clinicID := sharedtypes.ClinicID(id)
			prefs.PreferredClinicID = &clinicID
		}
	}
	return prefs, nil
}

// ── ContactInfo helpers ───────────────────────────────────────────

func contactPhone(p *aggregate.Patient) string {
	return p.ContactInfo().Phone.String()
}

func contactWhatsApp(p *aggregate.Patient) *string {
	if p.ContactInfo().WhatsApp != nil {
		s := p.ContactInfo().WhatsApp.String()
		return &s
	}
	return nil
}

func contactEmail(p *aggregate.Patient) *string {
	if p.ContactInfo().Email != nil {
		s := p.ContactInfo().Email.String()
		return &s
	}
	return nil
}

func contactEmergencyPhone(p *aggregate.Patient) *string {
	if p.ContactInfo().EmergencyPhone != nil {
		s := p.ContactInfo().EmergencyPhone.String()
		return &s
	}
	return nil
}

// ── Generic helpers ───────────────────────────────────────────────

func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ── CoverageHistoryPostgresRepository ────────────────────────────

type CoverageHistoryPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewCoverageHistoryPostgresRepository(pool *pgxpool.Pool) *CoverageHistoryPostgresRepository {
	return &CoverageHistoryPostgresRepository{pool: pool}
}

func (r *CoverageHistoryPostgresRepository) Append(ctx context.Context, entry coverage.CoverageHistoryEntry) error {
	prevType := ""
	if entry.PreviousType != nil {
		prevType = string(*entry.PreviousType)
	}
	prevStatus := ""
	if entry.PreviousStatus != nil {
		prevStatus = string(*entry.PreviousStatus)
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO patient.coverage_history (
			id, patient_id,
			previous_type, new_type,
			previous_status, new_status,
			reason, changed_at, changed_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		entry.ID, entry.PatientID,
		nullableStr(prevType), string(entry.NewType),
		nullableStr(prevStatus), string(entry.NewStatus),
		entry.Reason, entry.ChangedAt, entry.ChangedBy,
	)
	if err != nil {
		return fmt.Errorf("CoverageHistoryRepo.Append: %w", err)
	}
	return nil
}

func (r *CoverageHistoryPostgresRepository) FindByPatientID(ctx context.Context, patientID sharedtypes.PatientID, page sharedtypes.Page) (sharedtypes.PagedResult[coverage.CoverageHistoryEntry], error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, patient_id,
		       previous_type, new_type,
		       previous_status, new_status,
		       reason, changed_at, changed_by
		FROM patient.coverage_history
		WHERE patient_id = $1
		ORDER BY changed_at DESC
		LIMIT $2 OFFSET $3`, patientID, page.Limit, page.Offset)
	if err != nil {
		return sharedtypes.PagedResult[coverage.CoverageHistoryEntry]{}, fmt.Errorf("CoverageHistoryRepo.FindByPatientID: %w", err)
	}
	defer rows.Close()

	var entries []coverage.CoverageHistoryEntry
	for rows.Next() {
		var (
			id          uuid.UUID
			pid         uuid.UUID
			prevType    *string
			newType     string
			prevStatus  *string
			newStatus   string
			reason      *string
			changedAt   time.Time
			changedBy   uuid.UUID
		)
		if err := rows.Scan(&id, &pid, &prevType, &newType, &prevStatus, &newStatus, &reason, &changedAt, &changedBy); err != nil {
			return sharedtypes.PagedResult[coverage.CoverageHistoryEntry]{}, err
		}

		nt, _ := valueobject.ParseCoverageType(newType)
		entry := coverage.CoverageHistoryEntry{
			ID:        id,
			PatientID: pid,
			NewType:   nt,
			NewStatus: coverage.CoverageStatus(newStatus),
			ChangedAt: changedAt,
			ChangedBy: changedBy,
		}
		if prevType != nil {
			pt, _ := valueobject.ParseCoverageType(*prevType)
			entry.PreviousType = &pt
		}
		if prevStatus != nil {
			ps := coverage.CoverageStatus(*prevStatus)
			entry.PreviousStatus = &ps
		}
		if reason != nil {
			entry.Reason = *reason
		}
		entries = append(entries, entry)
	}

	var total int64
	_ = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM patient.coverage_history WHERE patient_id = $1`, patientID).Scan(&total)

	return sharedtypes.NewPagedResult(entries, total, page), rows.Err()
}
