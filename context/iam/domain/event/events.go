// Package event define los Domain Events del bounded context IAM.
// Todos implementan la interfaz events.DomainEvent del pkg/events.
package event

import (
	"time"

	"github.com/google/uuid"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// DomainEvent es la interfaz local del bounded context.
// Equivale a pkg/events.DomainEvent para evitar dependencia circular.
type DomainEvent interface {
	EventType() string
	AggregateID() string
	AggregateType() string
	BoundedContext() string
	SchemaVersion() int
}

const boundedContext = "iam"

// ── UserRegistered ────────────────────────────────────────────────

// UserRegistered se publica cuando un nuevo usuario completa el registro.
// Consumido por: Notifications (enviar bienvenida), Patient (si el linkedType es "patient").
type UserRegistered struct {
	UserID     sharedtypes.UserID `json:"user_id"`
	Email      string             `json:"email"`
	Role       string             `json:"role"`
	LinkedID   *uuid.UUID         `json:"linked_id,omitempty"`
	OccurredAt time.Time          `json:"occurred_at"`
}

func (e UserRegistered) EventType() string      { return "user.registered" }
func (e UserRegistered) AggregateID() string    { return e.UserID.String() }
func (e UserRegistered) AggregateType() string  { return "User" }
func (e UserRegistered) BoundedContext() string { return boundedContext }
func (e UserRegistered) SchemaVersion() int     { return 1 }

// ── UserLoggedOut ─────────────────────────────────────────────────

// UserLoggedOut se publica en logout global (todos los tokens revocados).
// Consumido por: Notifications (email de alerta si fue forzado).
type UserLoggedOut struct {
	UserID     sharedtypes.UserID `json:"user_id"`
	OccurredAt time.Time          `json:"occurred_at"`
}

func (e UserLoggedOut) EventType() string      { return "user.logged_out" }
func (e UserLoggedOut) AggregateID() string    { return e.UserID.String() }
func (e UserLoggedOut) AggregateType() string  { return "User" }
func (e UserLoggedOut) BoundedContext() string { return boundedContext }
func (e UserLoggedOut) SchemaVersion() int     { return 1 }

// ── UserSuspended ─────────────────────────────────────────────────

// UserSuspended se publica cuando un admin suspende una cuenta.
// Consumido por: Notifications (avisar al usuario), Scheduling (bloquear nuevas reservas).
type UserSuspended struct {
	UserID      sharedtypes.UserID `json:"user_id"`
	Reason      string             `json:"reason"`
	SuspendedBy uuid.UUID          `json:"suspended_by"`
	OccurredAt  time.Time          `json:"occurred_at"`
}

func (e UserSuspended) EventType() string      { return "user.suspended" }
func (e UserSuspended) AggregateID() string    { return e.UserID.String() }
func (e UserSuspended) AggregateType() string  { return "User" }
func (e UserSuspended) BoundedContext() string { return boundedContext }
func (e UserSuspended) SchemaVersion() int     { return 1 }

// ── FamilyMemberAdded ─────────────────────────────────────────────

// FamilyMemberAdded se publica cuando se agrega un miembro a una FamilyAccount.
type FamilyMemberAdded struct {
	FamilyID   sharedtypes.FamilyID  `json:"family_id"`
	PatientID  sharedtypes.PatientID `json:"patient_id"`
	Role       string                `json:"role"`
	IsMinor    bool                  `json:"is_minor"`
	OccurredAt time.Time             `json:"occurred_at"`
}

func (e FamilyMemberAdded) EventType() string      { return "family.member_added" }
func (e FamilyMemberAdded) AggregateID() string    { return e.FamilyID.String() }
func (e FamilyMemberAdded) AggregateType() string  { return "FamilyAccount" }
func (e FamilyMemberAdded) BoundedContext() string { return boundedContext }
func (e FamilyMemberAdded) SchemaVersion() int     { return 1 }
