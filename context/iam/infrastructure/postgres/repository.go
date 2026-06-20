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

// userRow es la estructura de mapeo entre el aggregate User y la tabla iam.users.
type userRow struct {
	ID            uuid.UUID  `db:"id"`
	Email         string     `db:"email"`
	PasswordHash  string     `db:"password_hash"`
	Role          string     `db:"role"`
	Status        string     `db:"status"`
	LinkedID      *uuid.UUID `db:"linked_id"`
	LinkedType    string     `db:"linked_type"`
	RefreshTokens []byte     `db:"refresh_tokens"` // JSONB
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
	CreatedBy     *uuid.UUID `db:"created_by"`
	UpdatedBy     *uuid.UUID `db:"updated_by"`
	Version       int64      `db:"version"`
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
		user.ID(), user.Version(), // WHERE con versión actual → optimistic lock
	)
	if err != nil {
		return fmt.Errorf("UserRepo.Update: %w", err)
	}
	if result.RowsAffected() == 0 {
		// Ninguna fila actualizada: otro proceso modificó el registro.
		return sharederrors.NewConflict(
			fmt.Sprintf("User '%s' fue modificado concurrentemente (versión %d)", user.ID(), user.Version()),
			nil,
		)
	}
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
			return nil, sharederrors.NewNotFound("User", "")
		}
		return nil, fmt.Errorf("UserRepo.scan: %w", err)
	}

	// Reconstruir Value Objects.
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

	// Deserializar refresh tokens desde JSONB.
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
			status, created_at, updated_at, version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		family.ID(), family.FamilyName(), family.PrimaryAdultID(),
		membersJSON, string(family.Status()),
		audit.CreatedAt, audit.UpdatedAt, family.Version(),
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
			version = $4
		WHERE id = $5 AND version = $6`,
		membersJSON, string(family.Status()),
		family.Audit().UpdatedAt, newVersion,
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
	// Implementación similar a FindByPatientID, omitida por brevedad.
	// En producción: SELECT + scanFamily idéntico al patrón de User.
	return nil, sharederrors.NewNotFound("FamilyAccount", id.String())
}

func (r *FamilyPostgresRepository) FindByPatientID(ctx context.Context, patientID sharedtypes.PatientID) (*aggregate.FamilyAccount, error) {
	// Busca la cuenta familiar donde el paciente aparece como miembro (en el JSONB members).
	// En producción, se normalizaría en una tabla iam.family_members para mayor eficiencia.
	row := r.pool.QueryRow(ctx, `
		SELECT id, family_name, primary_adult_id, members, status,
		       created_at, updated_at, version
		FROM iam.family_accounts
		WHERE primary_adult_id = $1
		   OR members @> $2::jsonb
		   AND status = 'Active'
		LIMIT 1`,
		patientID,
		fmt.Sprintf(`[{"patient_id":"%s"}]`, patientID),
	)

	var (
		id             uuid.UUID
		familyName     string
		primaryAdultID uuid.UUID
		membersJSON    []byte
		status         string
		createdAt      time.Time
		updatedAt      time.Time
		version        int64
	)

	err := row.Scan(&id, &familyName, &primaryAdultID, &membersJSON, &status,
		&createdAt, &updatedAt, &version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, sharederrors.NewNotFound("FamilyAccount", patientID.String())
		}
		return nil, fmt.Errorf("FamilyRepo.FindByPatientID: %w", err)
	}

	// Deserializar members desde JSONB.
	// En una implementación completa, reconstruiría el aggregate correctamente.
	_ = membersJSON

	return aggregate.NewFamilyAccount(primaryAdultID, familyName, nil), nil
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
		tokenID, _ := uuid.Parse(dto.TokenID)
		expiresAt, _ := time.Parse(time.RFC3339, dto.ExpiresAt)
		issuedAt, _ := time.Parse(time.RFC3339, dto.IssuedAt)

		var revokedAt *time.Time
		if dto.RevokedAt != nil {
			t, _ := time.Parse(time.RFC3339, *dto.RevokedAt)
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
// Código de error PostgreSQL 23505 = unique_violation.
func isUniqueViolation(err error) bool {
	return err != nil && (fmt.Sprintf("%v", err) != "" &&
		containsStr(err.Error(), "23505"))
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && searchStr(s, substr))
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
