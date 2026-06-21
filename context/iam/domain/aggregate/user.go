// Package aggregate contiene los Aggregates del bounded context Identity & Access.
//
// Aggregate Root: User
// Entidades internas: RefreshToken, FamilyAccount (ver family.go)
// Value Objects: HashedPassword, UserStatus, Role (en valueobject/)
package aggregate

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/iam/domain/event"
	"github.com/juantevez/odontoagenda/context/iam/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── User — Aggregate Root ────────────────────────────────────────

// User es el Aggregate Root del contexto IAM.
// Representa una cuenta de usuario en el sistema, que puede estar
// asociada a un Patient, Professional u otro actor.
//
// Invariantes:
//   - Email único en todo el sistema (enforced por repositorio + DB unique).
//   - Solo un refresh token activo por dispositivo (device_id).
//   - Un usuario suspendido no puede autenticarse.
//   - El password debe estar hasheado: nunca se almacena en plano.
type User struct {
	id       sharedtypes.UserID
	email    sharedvo.Email
	password valueobject.HashedPassword
	role     valueobject.Role
	status   valueobject.UserStatus

	// linkedID es el ID de la entidad asociada (Patient, Professional, etc.)
	linkedID   *uuid.UUID
	linkedType string // "patient" | "professional" | "staff"

	refreshTokens []RefreshToken

	audit   sharedtypes.AuditInfo
	version int64

	// pendingEvents acumula Domain Events para publicar después de persistir.
	pendingEvents []event.DomainEvent
}

// ── RefreshToken — Entity ────────────────────────────────────────

// RefreshToken es una entidad dentro del User aggregate.
// Cada dispositivo/sesión tiene su propio refresh token.
type RefreshToken struct {
	TokenID   uuid.UUID
	DeviceID  string
	TokenHash string // SHA-256 del token en plano
	ExpiresAt time.Time
	IssuedAt  time.Time
	RevokedAt *time.Time
}

func (rt RefreshToken) IsValid() bool {
	return rt.RevokedAt == nil && time.Now().UTC().Before(rt.ExpiresAt)
}

// ── Constructor ───────────────────────────────────────────────────

// NewUser crea un User nuevo (registro).
// El password ya debe venir hasheado (responsabilidad del application service).
func NewUser(
	email sharedvo.Email,
	hashedPassword valueobject.HashedPassword,
	role valueobject.Role,
	linkedID *uuid.UUID,
	linkedType string,
	createdBy *uuid.UUID,
) (*User, error) {
	if err := role.Validate(); err != nil {
		return nil, sharederrors.NewInvalidArgument("role", err.Error())
	}

	id := sharedtypes.NewID()
	user := &User{
		id:            id,
		email:         email,
		password:      hashedPassword,
		role:          role,
		status:        valueobject.StatusActive,
		linkedID:      linkedID,
		linkedType:    linkedType,
		refreshTokens: []RefreshToken{},
		audit:         sharedtypes.NewAuditInfo(createdBy),
		version:       1,
	}

	user.pendingEvents = append(user.pendingEvents, event.UserRegistered{
		UserID:     id,
		Email:      email.String(),
		Role:       string(role),
		LinkedID:   linkedID,
		OccurredAt: time.Now().UTC(),
	})

	return user, nil
}

// Reconstitute reconstruye un User desde persistencia (sin disparar eventos).
func Reconstitute(
	id sharedtypes.UserID,
	email sharedvo.Email,
	password valueobject.HashedPassword,
	role valueobject.Role,
	status valueobject.UserStatus,
	linkedID *uuid.UUID,
	linkedType string,
	refreshTokens []RefreshToken,
	audit sharedtypes.AuditInfo,
	version int64,
) *User {
	return &User{
		id:            id,
		email:         email,
		password:      password,
		role:          role,
		status:        status,
		linkedID:      linkedID,
		linkedType:    linkedType,
		refreshTokens: refreshTokens,
		audit:         audit,
		version:       version,
		pendingEvents: []event.DomainEvent{},
	}
}

// ── Comportamiento de dominio ─────────────────────────────────────

// Authenticate verifica el password en plano contra el hash almacenado.
// Retorna error si el usuario está suspendido o el password no coincide.
func (u *User) Authenticate(plainPassword string) error {
	if !u.status.IsActive() {
		return sharederrors.NewPrecondition("user_active",
			fmt.Sprintf("usuario con estado '%s' no puede autenticarse", u.status))
	}
	if !u.password.Matches(plainPassword) {
		return sharederrors.NewUnauthorized("credenciales inválidas")
	}
	return nil
}

// IssueRefreshToken registra un nuevo refresh token para el dispositivo dado.
// Si ya existía uno para ese device_id, lo revoca primero (un token por dispositivo).
func (u *User) IssueRefreshToken(deviceID, tokenHash string, ttl time.Duration) (RefreshToken, error) {
	if !u.status.IsActive() {
		return RefreshToken{}, sharederrors.NewPrecondition("user_active",
			"usuario inactivo no puede obtener refresh token")
	}

	// Revocar token anterior del mismo dispositivo.
	u.revokeTokensByDevice(deviceID)

	now := time.Now().UTC()
	rt := RefreshToken{
		TokenID:   uuid.New(),
		DeviceID:  deviceID,
		TokenHash: tokenHash,
		IssuedAt:  now,
		ExpiresAt: now.Add(ttl),
	}
	u.refreshTokens = append(u.refreshTokens, rt)
	selfID := uuid.UUID(u.id)
	u.audit.Touch(&selfID)

	return rt, nil
}

// ValidateRefreshToken verifica que el hash dado corresponde a un token activo.
func (u *User) ValidateRefreshToken(tokenHash string) (*RefreshToken, error) {
	for i := range u.refreshTokens {
		rt := &u.refreshTokens[i]
		if rt.TokenHash == tokenHash {
			if !rt.IsValid() {
				return nil, sharederrors.NewUnauthorized("refresh token expirado o revocado")
			}
			return rt, nil
		}
	}
	return nil, sharederrors.NewUnauthorized("refresh token no encontrado")
}

// RevokeRefreshToken revoca el token con el hash dado.
func (u *User) RevokeRefreshToken(tokenHash string) error {
	now := time.Now().UTC()
	for i := range u.refreshTokens {
		if u.refreshTokens[i].TokenHash == tokenHash {
			u.refreshTokens[i].RevokedAt = &now
			selfID := uuid.UUID(u.id)
			u.audit.Touch(&selfID)
			return nil
		}
	}
	return sharederrors.NewNotFound("RefreshToken", tokenHash)
}

// RevokeAllTokens revoca todos los refresh tokens activos (logout global).
func (u *User) RevokeAllTokens() {
	now := time.Now().UTC()
	selfID := uuid.UUID(u.id)
	for i := range u.refreshTokens {
		if u.refreshTokens[i].RevokedAt == nil {
			u.refreshTokens[i].RevokedAt = &now
		}
	}
	u.audit.Touch(&selfID)

	u.pendingEvents = append(u.pendingEvents, event.UserLoggedOut{
		UserID:     u.id,
		OccurredAt: time.Now().UTC(),
	})
}

// ChangePassword actualiza el password hasheado.
// Revoca todos los refresh tokens activos (sesiones previas invalidadas).
func (u *User) ChangePassword(newHashedPassword valueobject.HashedPassword) {
	u.password = newHashedPassword
	u.RevokeAllTokens() // ya llama audit.Touch internamente
}

// Suspend desactiva la cuenta. Un usuario suspendido no puede autenticarse.
// Invariante: Superadmin no puede ser suspendido (protección del sistema).
func (u *User) Suspend(reason string, byUser uuid.UUID) error {
	if u.role == valueobject.RoleSuperAdmin {
		return sharederrors.NewPrecondition("suspend_superadmin",
			"no se puede suspender a un superadmin")
	}
	if u.status == valueobject.StatusSuspended {
		return sharederrors.NewPrecondition("already_suspended",
			"el usuario ya está suspendido")
	}

	u.status = valueobject.StatusSuspended
	u.RevokeAllTokens()
	u.audit.Touch(&byUser)

	u.pendingEvents = append(u.pendingEvents, event.UserSuspended{
		UserID:      u.id,
		Reason:      reason,
		SuspendedBy: byUser,
		OccurredAt:  time.Now().UTC(),
	})

	return nil
}

// Activate reactiva una cuenta suspendida.
func (u *User) Activate(byUser uuid.UUID) error {
	if u.status == valueobject.StatusActive {
		return sharederrors.NewPrecondition("already_active",
			"el usuario ya está activo")
	}
	u.status = valueobject.StatusActive
	u.audit.Touch(&byUser)
	return nil
}

// ── Getters (solo lectura desde fuera del aggregate) ──────────────

// BumpVersion incrementa la versión en memoria tras una persistencia exitosa.
// Solo debe llamarse desde la capa de repositorio después de un Update exitoso.
func (u *User) BumpVersion() { u.version++ }

func (u *User) ID() sharedtypes.UserID               { return u.id }
func (u *User) Email() sharedvo.Email                { return u.email }
func (u *User) Role() valueobject.Role               { return u.role }
func (u *User) Status() valueobject.UserStatus       { return u.status }
func (u *User) LinkedID() *uuid.UUID                 { return u.linkedID }
func (u *User) LinkedType() string                   { return u.linkedType }
func (u *User) Password() valueobject.HashedPassword { return u.password }
func (u *User) RefreshTokens() []RefreshToken        { return u.refreshTokens }
func (u *User) Audit() sharedtypes.AuditInfo         { return u.audit }
func (u *User) Version() int64                       { return u.version }

// PendingEvents retorna los Domain Events acumulados y los limpia.
func (u *User) PendingEvents() []event.DomainEvent {
	evts := u.pendingEvents
	u.pendingEvents = nil
	return evts
}

// ── Helpers internos ──────────────────────────────────────────────

func (u *User) revokeTokensByDevice(deviceID string) {
	now := time.Now().UTC()
	for i := range u.refreshTokens {
		if u.refreshTokens[i].DeviceID == deviceID && u.refreshTokens[i].RevokedAt == nil {
			u.refreshTokens[i].RevokedAt = &now
		}
	}
}
