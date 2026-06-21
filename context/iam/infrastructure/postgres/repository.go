// Package postgres contiene los adaptadores de salida (repositorios PostgreSQL)
// del bounded context IAM.
//
// Responsabilidades:
//   - Mapear entre el modelo de dominio (aggregates) y el modelo de datos (SQL rows).
//   - Implementar optimistic locking mediante el campo version.
//   - NO contener lógica de negocio.
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
	"github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/iam/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── UserPostgresRepository ────────────────────────────────────────

// UserPostgresRepository implementa repository.UserRepository sobre pgx/v5.
type UserPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewUserPostgresRepository(pool *pgxpool.Pool) *UserPostgresRepository {
	return &UserPostgresRepository{pool: pool}
}

// Save inserta un nuevo User en la tabla iam.users.
func (r *UserPostgresRepository) Save(ctx context.Context, user *aggregate.User) error {
	tokensJSON, err := marshalRefreshTokens(user.RefreshTokens())
	if err != nil {
		return fmt.Errorf("UserRepo.Save: marshal tokens: %w", err)
	}

	audit := user.Audit()
	_, err = r.pool.Exec(ctx, `
		INSERT INTO iam.users (
			id, email, password_hash, role, status,
			linked_id, linked_type, refresh_tokens,
			created_at, updated_at, created_by, updated_by, version
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11, $12, $13
		)`,
		user.ID(), user.Email().String(), user.Password().String(),
		string(user.Role()), string(user.Status()),
		user.LinkedID(), user.LinkedType(), tokensJSON,
		audit.CreatedAt, audit.UpdatedAt, audit.CreatedBy, audit.UpdatedBy,
		user.Version(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return sharederrors.NewAlreadyExists("User", "email", user.Email().String())
		}
		return fmt.Errorf("UserRepo.Save: %w", err)
	}
	return nil
}

// Update actualiza un User existente con optimistic locking.
// Si la versión en BD difiere de la versión del aggregate, retorna ErrConflict.
// Tras una actualización exitosa incrementa la versión en memoria del aggregate.
func (r *UserPostgresRepository) Update(ctx context.Context, user *aggregate.User) error {
	tokensJSON, err := marshalRefreshTokens(user.RefreshTokens())
	if err != nil {
		return fmt.Errorf("UserRepo.Update: marshal tokens: %w", err)
	}

	audit := user.Audit()
	newVersion := user.Version() + 1

	result, err := r.pool.Exec(ctx, `
		UPDATE iam.users SET
			email = $1,
			password_hash = $2,
			status = $3,
			refresh_tokens = $4,
			updated_at = $5,
			updated_by = $6,
			version = $7
		WHERE id = $8 AND version = $9`,
		user.Email().String(), user.Password().String(),
		string(user.Status()), tokensJSON,
		audit.UpdatedAt, audit.UpdatedBy,
		newVersion,
		user.ID(), user.Version(),
	)
	if err != nil {
		return fmt.Errorf("UserRepo.Update: %w", err)
	}
	if result.RowsAffected() == 0 {
		return sharederrors.NewConflict(
			fmt.Sprintf("User '%s' fue modificado concurrentemente (versión %d)", user.ID(), user.Version()),
			nil,
		)
	}

	user.BumpVersion()
	return nil
}

// FindByID recupera un User por su UUID.
func (r *UserPostgresRepository) FindByID(ctx context.Context, id sharedtypes.UserID) (*aggregate.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role, status,
		       linked_id, linked_type, refresh_tokens,
		       created_at, updated_at, created_by, updated_by, version
		FROM iam.users
		WHERE id = $1`, id)

	return r.scanUser(row)
}

// FindByEmail recupera un User por email.
func (r *UserPostgresRepository) FindByEmail(ctx context.Context, email sharedvo.Email) (*aggregate.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role, status,
		       linked_id, linked_type, refresh_tokens,
		       created_at, updated_at, created_by, updated_by, version
		FROM iam.users
		WHERE email = $1`, email.String())

	return r.scanUser(row)
}

// ExistsByEmail verifica unicidad de email sin cargar el aggregate completo.
func (r *UserPostgresRepository) ExistsByEmail(ctx context.Context, email sharedvo.Email) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM iam.users WHERE email = $1)`,
		email.String(),
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("UserRepo.ExistsByEmail: %w", err)
	}
	return exists, nil
}

// scanUser mapea una pgx.Row al aggregate User.
func (r *UserPostgresRepository) scanUser(row pgx.Row) (*aggregate.User, error) {
	var (
		id           uuid.UUID
		email        string
		passwordHash string
		role         string
		status       string
		linkedID     *uuid.UUID
		linkedType   string
		tokensJSON   []byte
		createdAt    time.Time
		updatedAt    time.Time
		createdBy    *uuid.UUID
		updatedBy    *uuid.UUID
		version      int64
	)

	err := row.Scan(
		&id, &email, &passwordHash, &role, &status,
		&linkedID, &linkedType, &tokensJSON,
		&createdAt, &updatedAt, &createdBy, &updatedBy, &version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, sharederrors.NewNotFound("User", id.String())
		}
		return nil, fmt.Errorf("UserRepo.scan: %w", err)
	}

	emailVO, err := sharedvo.NewEmail(email)
	if err != nil {
		return nil, fmt.Errorf("UserRepo.scan: email inválido en BD: %w", err)
	}

	passwordVO := valueobject.LoadHash([]byte(passwordHash))

	roleVO := valueobject.Role(role)
	statusVO, err := valueobject.ParseUserStatus(status)
	if err != nil {
		return nil, fmt.Errorf("UserRepo.scan: status inválido en BD: %w", err)
	}

	tokens, err := unmarshalRefreshTokens(tokensJSON)
	if err != nil {
		return nil, fmt.Errorf("UserRepo.scan: tokens inválidos en BD: %w", err)
	}

	audit := sharedtypes.AuditInfo{
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		CreatedBy: createdBy,
		UpdatedBy: updatedBy,
	}

	return aggregate.Reconstitute(
		id, emailVO, passwordVO, roleVO, statusVO,
		linkedID, linkedType,
		tokens, audit, version,
	), nil
}

// ── FamilyPostgresRepository ──────────────────────────────────────

// FamilyPostgresRepository implementa repository.FamilyRepository.
type FamilyPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewFamilyPostgresRepository(pool *pgxpool.Pool) *FamilyPostgresRepository {
	return &FamilyPostgresRepository{pool: pool}
}

func (r *FamilyPostgresRepository) Save(ctx context.Context, family *aggregate.FamilyAccount) error {
	membersJSON, err := json.Marshal(family.Members())
	if err != nil {
		return fmt.Errorf("FamilyRepo.Save: marshal members: %w", err)
	}

	audit := family.Audit()
	_, err = r.pool.Exec(ctx, `
		INSERT INTO iam.family_accounts (
			id, family_name, primary_adult_id, members,
			status, created_at, updated_at, created_by, updated_by, version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		family.ID(), family.FamilyName(), family.PrimaryAdultID(),
		membersJSON, string(family.Status()),
		audit.CreatedAt, audit.UpdatedAt, audit.CreatedBy, audit.UpdatedBy,
		family.Version(),
	)
	if err != nil {
		return fmt.Errorf("FamilyRepo.Save: %w", err)
	}
	return nil
}

func (r *FamilyPostgresRepository) Update(ctx context.Context, family *aggregate.FamilyAccount) error {
	membersJSON, err := json.Marshal(family.Members())
	if err != nil {
		return fmt.Errorf("FamilyRepo.Update: marshal members: %w", err)
	}

	newVersion := family.Version() + 1
	result, err := r.pool.Exec(ctx, `
		UPDATE iam.family_accounts SET
			members = $1,
			status = $2,
			updated_at = $3,
			updated_by = $4,
			version = $5
		WHERE id = $6 AND version = $7`,
		membersJSON, string(family.Status()),
		family.Audit().UpdatedAt, family.Audit().UpdatedBy,
		newVersion,
		family.ID(), family.Version(),
	)
	if err != nil {
		return fmt.Errorf("FamilyRepo.Update: %w", err)
	}
	if result.RowsAffected() == 0 {
		return sharederrors.NewConflict(
			fmt.Sprintf("FamilyAccount '%s' modificada concurrentemente", family.ID()), nil,
		)
	}
	return nil
}

func (r *FamilyPostgresRepository) FindByID(ctx context.Context, id sharedtypes.FamilyID) (*aggregate.FamilyAccount, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, family_name, primary_adult_id, members, status,
		       created_at, updated_at, created_by, updated_by, version
		FROM iam.family_accounts
		WHERE id = $1`, id)

	return r.scanFamily(row)
}

func (r *FamilyPostgresRepository) FindByPatientID(ctx context.Context, patientID sharedtypes.PatientID) (*aggregate.FamilyAccount, error) {
	// Los paréntesis son obligatorios: AND tiene mayor precedencia que OR.
	row := r.pool.QueryRow(ctx, `
		SELECT id, family_name, primary_adult_id, members, status,
		       created_at, updated_at, created_by, updated_by, version
		FROM iam.family_accounts
		WHERE (primary_adult_id = $1 OR members @> $2::jsonb)
		  AND status = 'Active'
		LIMIT 1`,
		patientID,
		fmt.Sprintf(`[{"patient_id":"%s"}]`, patientID),
	)

	return r.scanFamily(row)
}

// scanFamily mapea una pgx.Row al aggregate FamilyAccount.
func (r *FamilyPostgresRepository) scanFamily(row pgx.Row) (*aggregate.FamilyAccount, error) {
	var (
		id             uuid.UUID
		familyName     string
		primaryAdultID uuid.UUID
		membersJSON    []byte
		status         string
		createdAt      time.Time
		updatedAt      time.Time
		createdBy      *uuid.UUID
		updatedBy      *uuid.UUID
		version        int64
	)

	err := row.Scan(
		&id, &familyName, &primaryAdultID, &membersJSON, &status,
		&createdAt, &updatedAt, &createdBy, &updatedBy, &version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, sharederrors.NewNotFound("FamilyAccount", id.String())
		}
		return nil, fmt.Errorf("FamilyRepo.scan: %w", err)
	}

	statusVO, err := aggregate.ParseFamilyStatus(status)
	if err != nil {
		return nil, fmt.Errorf("FamilyRepo.scan: status inválido en BD: %w", err)
	}

	var members []aggregate.FamilyMember
	if err := json.Unmarshal(membersJSON, &members); err != nil {
		return nil, fmt.Errorf("FamilyRepo.scan: members inválidos en BD: %w", err)
	}

	audit := sharedtypes.AuditInfo{
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		CreatedBy: createdBy,
		UpdatedBy: updatedBy,
	}

	return aggregate.ReconstituteFamilyAccount(
		id, familyName, primaryAdultID, members, statusVO, audit, version,
	), nil
}

// ── Helpers de serialización ──────────────────────────────────────

// refreshTokenJSON es el DTO para serializar RefreshTokens en JSONB.
type refreshTokenJSON struct {
	TokenID   string  `json:"token_id"`
	DeviceID  string  `json:"device_id"`
	TokenHash string  `json:"token_hash"`
	ExpiresAt string  `json:"expires_at"`
	IssuedAt  string  `json:"issued_at"`
	RevokedAt *string `json:"revoked_at,omitempty"`
}

func marshalRefreshTokens(tokens []aggregate.RefreshToken) ([]byte, error) {
	dtos := make([]refreshTokenJSON, len(tokens))
	for i, t := range tokens {
		dto := refreshTokenJSON{
			TokenID:   t.TokenID.String(),
			DeviceID:  t.DeviceID,
			TokenHash: t.TokenHash,
			ExpiresAt: t.ExpiresAt.UTC().Format(time.RFC3339),
			IssuedAt:  t.IssuedAt.UTC().Format(time.RFC3339),
		}
		if t.RevokedAt != nil {
			s := t.RevokedAt.UTC().Format(time.RFC3339)
			dto.RevokedAt = &s
		}
		dtos[i] = dto
	}
	return json.Marshal(dtos)
}

func unmarshalRefreshTokens(data []byte) ([]aggregate.RefreshToken, error) {
	if len(data) == 0 || string(data) == "null" {
		return []aggregate.RefreshToken{}, nil
	}
	var dtos []refreshTokenJSON
	if err := json.Unmarshal(data, &dtos); err != nil {
		return nil, err
	}
	tokens := make([]aggregate.RefreshToken, len(dtos))
	for i, dto := range dtos {
		tokenID, err := uuid.Parse(dto.TokenID)
		if err != nil {
			return nil, fmt.Errorf("token[%d]: token_id inválido: %w", i, err)
		}
		expiresAt, err := time.Parse(time.RFC3339, dto.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("token[%d]: expires_at inválido: %w", i, err)
		}
		issuedAt, err := time.Parse(time.RFC3339, dto.IssuedAt)
		if err != nil {
			return nil, fmt.Errorf("token[%d]: issued_at inválido: %w", i, err)
		}

		var revokedAt *time.Time
		if dto.RevokedAt != nil {
			t, err := time.Parse(time.RFC3339, *dto.RevokedAt)
			if err != nil {
				return nil, fmt.Errorf("token[%d]: revoked_at inválido: %w", i, err)
			}
			revokedAt = &t
		}

		tokens[i] = aggregate.RefreshToken{
			TokenID:   tokenID,
			DeviceID:  dto.DeviceID,
			TokenHash: dto.TokenHash,
			ExpiresAt: expiresAt,
			IssuedAt:  issuedAt,
			RevokedAt: revokedAt,
		}
	}
	return tokens, nil
}

// isUniqueViolation detecta violaciones de UNIQUE constraint en PostgreSQL.
// Usa pgconn.PgError para inspeccionar el SQLSTATE 23505 directamente.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

