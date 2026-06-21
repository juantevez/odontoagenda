// Package event define los Domain Events del bounded context Coverage & Agreements.
package event

import (
	"time"

	"github.com/google/uuid"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// DomainEvent es la interfaz local del bounded context.
type DomainEvent interface {
	EventType() string
	AggregateID() string
	AggregateType() string
	BoundedContext() string
	SchemaVersion() int
}

const boundedContext = "coverage"

// ── AgreementCreated ──────────────────────────────────────────────

// AgreementCreated se publica al crear un nuevo convenio.
// Consumido por: Billing (registrar el financiador), Notifications.
type AgreementCreated struct {
	AgreementID   uuid.UUID `json:"agreement_id"`
	AgreementCode string    `json:"agreement_code"`
	ProviderName  string    `json:"provider_name"`
	ProviderType  string    `json:"provider_type"`
	OccurredAt    time.Time `json:"occurred_at"`
}

func (e AgreementCreated) EventType() string      { return "agreement.created" }
func (e AgreementCreated) AggregateID() string    { return e.AgreementID.String() }
func (e AgreementCreated) AggregateType() string  { return "Agreement" }
func (e AgreementCreated) BoundedContext() string { return boundedContext }
func (e AgreementCreated) SchemaVersion() int     { return 1 }

// ── AgreementPlanAdded ────────────────────────────────────────────

// AgreementPlanAdded se publica al agregar un Plan a un Agreement.
// Consumido por: Billing (actualizar reglas de precio).
type AgreementPlanAdded struct {
	AgreementID uuid.UUID `json:"agreement_id"`
	PlanID      uuid.UUID `json:"plan_id"`
	PlanCode    string    `json:"plan_code"`
	CoPayType   string    `json:"co_pay_type"`
	CoPayValue  int       `json:"co_pay_value"`
	OccurredAt  time.Time `json:"occurred_at"`
}

func (e AgreementPlanAdded) EventType() string      { return "agreement.plan_added" }
func (e AgreementPlanAdded) AggregateID() string    { return e.AgreementID.String() }
func (e AgreementPlanAdded) AggregateType() string  { return "Agreement" }
func (e AgreementPlanAdded) BoundedContext() string { return boundedContext }
func (e AgreementPlanAdded) SchemaVersion() int     { return 1 }

// ── AgreementProcedureRuleUpdated ─────────────────────────────────

// AgreementProcedureRuleUpdated se publica al agregar o modificar una ProcedureRule.
// Consumido por: Billing (invalidar cache de precios para ese plan+procedimiento).
type AgreementProcedureRuleUpdated struct {
	AgreementID           uuid.UUID `json:"agreement_id"`
	PlanID                uuid.UUID `json:"plan_id"`
	ProcedureCode         string    `json:"procedure_code"`
	CoveragePercent       int       `json:"coverage_percent"`
	RequiresAuthorization bool      `json:"requires_authorization"`
	OccurredAt            time.Time `json:"occurred_at"`
}

func (e AgreementProcedureRuleUpdated) EventType() string      { return "agreement.procedure_rule_updated" }
func (e AgreementProcedureRuleUpdated) AggregateID() string    { return e.AgreementID.String() }
func (e AgreementProcedureRuleUpdated) AggregateType() string  { return "Agreement" }
func (e AgreementProcedureRuleUpdated) BoundedContext() string { return boundedContext }
func (e AgreementProcedureRuleUpdated) SchemaVersion() int     { return 1 }

// ── AgreementSuspended ────────────────────────────────────────────

// AgreementSuspended se publica al suspender un convenio.
// Consumido por: Scheduling (bloquear nuevas reservas), Billing, Notifications.
type AgreementSuspended struct {
	AgreementID uuid.UUID `json:"agreement_id"`
	Reason      string    `json:"reason"`
	SuspendedBy uuid.UUID `json:"suspended_by"`
	OccurredAt  time.Time `json:"occurred_at"`
}

func (e AgreementSuspended) EventType() string      { return "agreement.suspended" }
func (e AgreementSuspended) AggregateID() string    { return e.AgreementID.String() }
func (e AgreementSuspended) AggregateType() string  { return "Agreement" }
func (e AgreementSuspended) BoundedContext() string { return boundedContext }
func (e AgreementSuspended) SchemaVersion() int     { return 1 }

// ── AgreementActivated ────────────────────────────────────────────

// AgreementActivated se publica al reactivar un convenio suspendido.
// Consumido por: Scheduling, Billing.
type AgreementActivated struct {
	AgreementID uuid.UUID `json:"agreement_id"`
	ActivatedBy uuid.UUID `json:"activated_by"`
	OccurredAt  time.Time `json:"occurred_at"`
}

func (e AgreementActivated) EventType() string      { return "agreement.activated" }
func (e AgreementActivated) AggregateID() string    { return e.AgreementID.String() }
func (e AgreementActivated) AggregateType() string  { return "Agreement" }
func (e AgreementActivated) BoundedContext() string { return boundedContext }
func (e AgreementActivated) SchemaVersion() int     { return 1 }

// ── AgreementExpired ──────────────────────────────────────────────

// AgreementExpired se publica cuando un convenio llega a su validUntil.
// Publicado por el job scheduler. Consumido por: Scheduling, Billing, Notifications.
type AgreementExpired struct {
	AgreementID uuid.UUID `json:"agreement_id"`
	ExpiredAt   time.Time `json:"expired_at"`
}

func (e AgreementExpired) EventType() string      { return "agreement.expired" }
func (e AgreementExpired) AggregateID() string    { return e.AgreementID.String() }
func (e AgreementExpired) AggregateType() string  { return "Agreement" }
func (e AgreementExpired) BoundedContext() string { return boundedContext }
func (e AgreementExpired) SchemaVersion() int     { return 1 }

// ── AuthorizationRequested ────────────────────────────────────────

// AuthorizationRequested se publica al crear una solicitud de autorización.
// Consumido por: Notifications (avisar al admin para resolución manual).
type AuthorizationRequested struct {
	AuthorizationID uuid.UUID              `json:"authorization_id"`
	AgreementID     uuid.UUID              `json:"agreement_id"`
	PlanID          uuid.UUID              `json:"plan_id"`
	PatientID       sharedtypes.PatientID  `json:"patient_id"`
	ProcedureCode   string                 `json:"procedure_code"`
	AppointmentID   *uuid.UUID             `json:"appointment_id,omitempty"`
	OccurredAt      time.Time              `json:"occurred_at"`
}

func (e AuthorizationRequested) EventType() string      { return "authorization.requested" }
func (e AuthorizationRequested) AggregateID() string    { return e.AuthorizationID.String() }
func (e AuthorizationRequested) AggregateType() string  { return "AuthorizationRequest" }
func (e AuthorizationRequested) BoundedContext() string { return boundedContext }
func (e AuthorizationRequested) SchemaVersion() int     { return 1 }

// ── AuthorizationResolved ─────────────────────────────────────────

// AuthorizationResolved se publica cuando una autorización es aprobada o rechazada.
// Consumido por: Scheduling (actualizar Appointment a Confirmed o Cancelled).
// Notifications (informar al paciente).
type AuthorizationResolved struct {
	AuthorizationID   uuid.UUID             `json:"authorization_id"`
	PatientID         sharedtypes.PatientID `json:"patient_id"`
	ProcedureCode     string                `json:"procedure_code"`
	AppointmentID     *uuid.UUID            `json:"appointment_id,omitempty"`
	Status            string                `json:"status"` // Approved | Rejected
	AuthorizationCode *string               `json:"authorization_code,omitempty"`
	RejectionReason   *string               `json:"rejection_reason,omitempty"`
	OccurredAt        time.Time             `json:"occurred_at"`
}

func (e AuthorizationResolved) EventType() string      { return "authorization.resolved" }
func (e AuthorizationResolved) AggregateID() string    { return e.AuthorizationID.String() }
func (e AuthorizationResolved) AggregateType() string  { return "AuthorizationRequest" }
func (e AuthorizationResolved) BoundedContext() string { return boundedContext }
func (e AuthorizationResolved) SchemaVersion() int     { return 1 }

// ── AuthorizationExpired ──────────────────────────────────────────

// AuthorizationExpired se publica cuando una autorización Pending supera el deadline.
// Consumido por: Scheduling (cancelar el Appointment), Notifications.
type AuthorizationExpired struct {
	AuthorizationID uuid.UUID             `json:"authorization_id"`
	PatientID       sharedtypes.PatientID `json:"patient_id"`
	AppointmentID   *uuid.UUID            `json:"appointment_id,omitempty"`
	OccurredAt      time.Time             `json:"occurred_at"`
}

func (e AuthorizationExpired) EventType() string      { return "authorization.expired" }
func (e AuthorizationExpired) AggregateID() string    { return e.AuthorizationID.String() }
func (e AuthorizationExpired) AggregateType() string  { return "AuthorizationRequest" }
func (e AuthorizationExpired) BoundedContext() string { return boundedContext }
func (e AuthorizationExpired) SchemaVersion() int     { return 1 }
