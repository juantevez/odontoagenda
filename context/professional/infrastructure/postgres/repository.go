// Package postgres contiene los adaptadores de salida PostgreSQL del bounded context Professional.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

type ProfessionalPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewProfessionalPostgresRepository(pool *pgxpool.Pool) *ProfessionalPostgresRepository {
	return &ProfessionalPostgresRepository{pool: pool}
}

func (r *ProfessionalPostgresRepository) Save(_ context.Context, _ *aggregate.Professional) error {
	return fmt.Errorf("not implemented")
}

func (r *ProfessionalPostgresRepository) Update(_ context.Context, _ *aggregate.Professional) error {
	return fmt.Errorf("not implemented")
}

func (r *ProfessionalPostgresRepository) FindByID(ctx context.Context, id sharedtypes.ProfessionalID) (*aggregate.Professional, error) {
	return r.findOne(ctx, `WHERE p.id=$1 AND p.status='Active'`, id)
}

func (r *ProfessionalPostgresRepository) FindByClinic(ctx context.Context, clinicID sharedtypes.ClinicID, specialty *valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	if specialty != nil {
		return r.findMany(ctx, `
			WHERE ca.clinic_id=$1 AND ca.status='Active' AND p.status='Active'
			  AND EXISTS (SELECT 1 FROM professional.licenses l
			              WHERE l.professional_id=p.id AND l.specialty_code=$2 AND l.status='Active')`,
			clinicID, string(*specialty))
	}
	return r.findMany(ctx,
		`WHERE ca.clinic_id=$1 AND ca.status='Active' AND p.status='Active'`,
		clinicID)
}

func (r *ProfessionalPostgresRepository) FindBySpecialty(ctx context.Context, specialty valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	return r.findMany(ctx, `
		WHERE p.status='Active'
		  AND EXISTS (SELECT 1 FROM professional.licenses l
		              WHERE l.professional_id=p.id AND l.specialty_code=$1 AND l.status='Active')`,
		string(specialty))
}

func (r *ProfessionalPostgresRepository) FindAvailableAt(ctx context.Context, clinicID sharedtypes.ClinicID, _ time.Time, specialty *valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	return r.FindByClinic(ctx, clinicID, specialty)
}

func (r *ProfessionalPostgresRepository) FindWithExpiringLicenses(_ context.Context, _ int) ([]*aggregate.Professional, error) {
	return []*aggregate.Professional{}, nil
}

func (r *ProfessionalPostgresRepository) ExistsByNationalID(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (r *ProfessionalPostgresRepository) Search(ctx context.Context, clinicID sharedtypes.ClinicID, q string) ([]*aggregate.Professional, error) {
	query := `
		SELECT DISTINCT p.id, p.user_id, p.full_name, p.email, p.phone, p.status,
		       p.created_at, p.updated_at
		FROM professional.professionals p
		LEFT JOIN professional.clinic_assignments ca ON ca.professional_id=p.id AND ca.status='Active'
		LEFT JOIN professional.licenses l ON l.professional_id=p.id AND l.status='Active'
		WHERE p.status='Active'
		  AND ($1::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR ca.clinic_id=$1)
		  AND (
		        unaccent(p.full_name) ILIKE '%' || unaccent($2) || '%'
		     OR unaccent(l.specialty_code) ILIKE '%' || unaccent($2) || '%'
		  )
		ORDER BY p.full_name`

	rows, err := r.pool.Query(ctx, query, clinicID, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var professionals []*aggregate.Professional
	for rows.Next() {
		p, err := scanProfessionalRow(rows)
		if err != nil {
			return nil, err
		}
		professionals = append(professionals, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, p := range professionals {
		if err := r.loadLicenses(ctx, p); err != nil {
			return nil, err
		}
	}
	if professionals == nil {
		professionals = []*aggregate.Professional{}
	}
	return professionals, nil
}

// ── SQL helpers ───────────────────────────────────────────────────

func (r *ProfessionalPostgresRepository) findOne(ctx context.Context, where string, args ...any) (*aggregate.Professional, error) {
	query := `
		SELECT DISTINCT p.id, p.user_id, p.full_name, p.email, p.phone, p.status,
		       p.created_at, p.updated_at
		FROM professional.professionals p
		LEFT JOIN professional.clinic_assignments ca ON ca.professional_id=p.id ` + where + ` LIMIT 1`

	row := r.pool.QueryRow(ctx, query, args...)
	p, err := scanProfessionalRow(row)
	if err != nil {
		return nil, err
	}
	if err := r.loadLicenses(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (r *ProfessionalPostgresRepository) findMany(ctx context.Context, where string, args ...any) ([]*aggregate.Professional, error) {
	query := `
		SELECT DISTINCT p.id, p.user_id, p.full_name, p.email, p.phone, p.status,
		       p.created_at, p.updated_at
		FROM professional.professionals p
		LEFT JOIN professional.clinic_assignments ca ON ca.professional_id=p.id ` + where + `
		ORDER BY p.full_name`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var professionals []*aggregate.Professional
	for rows.Next() {
		p, err := scanProfessionalRow(rows)
		if err != nil {
			return nil, err
		}
		professionals = append(professionals, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, p := range professionals {
		if err := r.loadLicenses(ctx, p); err != nil {
			return nil, err
		}
	}
	if professionals == nil {
		professionals = []*aggregate.Professional{}
	}
	return professionals, nil
}

func (r *ProfessionalPostgresRepository) loadLicenses(ctx context.Context, p *aggregate.Professional) error {
	rows, err := r.pool.Query(ctx, `
		SELECT id, specialty_code, license_number, issued_at, expires_at, status, created_at, updated_at
		FROM professional.licenses
		WHERE professional_id=$1`, p.ID())
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id, specialtyCode, licenseNumber, statusStr string
			issuedAt, createdAt, updatedAt              time.Time
			expiresAt                                   *time.Time
		)
		if err := rows.Scan(&id, &specialtyCode, &licenseNumber, &issuedAt, &expiresAt, &statusStr, &createdAt, &updatedAt); err != nil {
			return err
		}
		_ = id
		_ = createdAt
		_ = updatedAt
		code := valueobject.SpecialtyCode(specialtyCode)
		specialty, err := valueobject.NewSpecialty(code, code.DisplayName())
		if err != nil {
			continue
		}
		licenseID, _ := uuid.Parse(id)
		p.ReconstituteLicense(licenseID, specialty, licenseNumber, "", issuedAt, expiresAt, valueobject.LicenseStatus(statusStr), "")
	}
	return rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProfessionalRow(row rowScanner) (*aggregate.Professional, error) {
	var (
		id        uuid.UUID
		userID    *uuid.UUID
		fullName  string
		email     string
		phone     string
		status    string
		createdAt time.Time
		updatedAt time.Time
	)
	err := row.Scan(&id, &userID, &fullName, &email, &phone, &status, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("professional not found")
		}
		return nil, err
	}

	fn, err := sharedvo.NewFullName(fullName)
	if err != nil {
		return nil, err
	}
	em, err := sharedvo.NewEmail(email)
	if err != nil {
		return nil, err
	}
	ph, err := sharedvo.NewPhoneNumber(phone)
	if err != nil {
		return nil, err
	}

	return aggregate.Reconstitute(
		id, userID, fn,
		sharedvo.NationalID{},
		em, ph, "",
		valueobject.ProfessionalStatus(status),
		[]aggregate.ProfessionalLicense{},
		[]aggregate.ClinicAssignment{},
		map[string]valueobject.ProcedureDuration{},
		createdAt, updatedAt, nil, 1,
	), nil
}
