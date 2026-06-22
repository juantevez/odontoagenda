// Package event define los Domain Events del bounded context Billing & Payments.
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

const boundedContext = "billing"

// ── QuoteCreated ──────────────────────────────────────────────────

// QuoteCreated se publica al crear el presupuesto de una cita.
type QuoteCreated struct {
	QuoteID             uuid.UUID             `json:"quote_id"`
	AppointmentID       uuid.UUID             `json:"appointment_id"`
	PatientID           sharedtypes.PatientID `json:"patient_id"`
	ClinicID            sharedtypes.ClinicID  `json:"clinic_id"`
	ProcedureCode       string                `json:"procedure_code"`
	ArancelCents        int64                 `json:"arancel_cents"`
	CoverageAmountCents int64                 `json:"coverage_amount_cents"`
	CoPayAmountCents    int64                 `json:"co_pay_amount_cents"`
	OccurredAt          time.Time             `json:"occurred_at"`
}

func (e QuoteCreated) EventType() string      { return "billing.quote_created" }
func (e QuoteCreated) AggregateID() string    { return e.QuoteID.String() }
func (e QuoteCreated) AggregateType() string  { return "Quote" }
func (e QuoteCreated) BoundedContext() string { return boundedContext }
func (e QuoteCreated) SchemaVersion() int     { return 1 }

// ── PaymentReceived ───────────────────────────────────────────────

// PaymentReceived se publica al confirmar un pago.
// Consumido por: Notifications (enviar comprobante al paciente).
type PaymentReceived struct {
	QuoteID       uuid.UUID             `json:"quote_id"`
	AppointmentID uuid.UUID             `json:"appointment_id"`
	PatientID     sharedtypes.PatientID `json:"patient_id"`
	AmountCents   int64                 `json:"amount_cents"`
	PaymentMethod string                `json:"payment_method"`
	ReceiptNumber *string               `json:"receipt_number,omitempty"`
	OccurredAt    time.Time             `json:"occurred_at"`
}

func (e PaymentReceived) EventType() string      { return "billing.payment_received" }
func (e PaymentReceived) AggregateID() string    { return e.QuoteID.String() }
func (e PaymentReceived) AggregateType() string  { return "Quote" }
func (e PaymentReceived) BoundedContext() string { return boundedContext }
func (e PaymentReceived) SchemaVersion() int     { return 1 }

// ── QuotePaid ─────────────────────────────────────────────────────

// QuotePaid se publica cuando el pago total fue completado.
// Consumido por: Notifications (resumen final), futuro módulo de Liquidaciones.
type QuotePaid struct {
	QuoteID        uuid.UUID             `json:"quote_id"`
	AppointmentID  uuid.UUID             `json:"appointment_id"`
	PatientID      sharedtypes.PatientID `json:"patient_id"`
	TotalPaidCents int64                 `json:"total_paid_cents"`
	OccurredAt     time.Time             `json:"occurred_at"`
}

func (e QuotePaid) EventType() string      { return "billing.quote_paid" }
func (e QuotePaid) AggregateID() string    { return e.QuoteID.String() }
func (e QuotePaid) AggregateType() string  { return "Quote" }
func (e QuotePaid) BoundedContext() string { return boundedContext }
func (e QuotePaid) SchemaVersion() int     { return 1 }

// ── QuoteVoided ───────────────────────────────────────────────────

// QuoteVoided se publica al anular un presupuesto sin cobro.
type QuoteVoided struct {
	QuoteID       uuid.UUID             `json:"quote_id"`
	AppointmentID uuid.UUID             `json:"appointment_id"`
	PatientID     sharedtypes.PatientID `json:"patient_id"`
	Reason        string                `json:"reason"`
	OccurredAt    time.Time             `json:"occurred_at"`
}

func (e QuoteVoided) EventType() string      { return "billing.quote_voided" }
func (e QuoteVoided) AggregateID() string    { return e.QuoteID.String() }
func (e QuoteVoided) AggregateType() string  { return "Quote" }
func (e QuoteVoided) BoundedContext() string { return boundedContext }
func (e QuoteVoided) SchemaVersion() int     { return 1 }

// ── LateFeeApplied ────────────────────────────────────────────────

// LateFeeApplied se publica al aplicar un cargo por cancelación/no-show.
// Consumido por: Notifications (avisar al paciente del cargo).
type LateFeeApplied struct {
	QuoteID       uuid.UUID             `json:"quote_id"`
	AppointmentID uuid.UUID             `json:"appointment_id"`
	PatientID     sharedtypes.PatientID `json:"patient_id"`
	FeeType       string                `json:"fee_type"`
	AmountCents   int64                 `json:"amount_cents"`
	OccurredAt    time.Time             `json:"occurred_at"`
}

func (e LateFeeApplied) EventType() string      { return "billing.late_fee_applied" }
func (e LateFeeApplied) AggregateID() string    { return e.QuoteID.String() }
func (e LateFeeApplied) AggregateType() string  { return "Quote" }
func (e LateFeeApplied) BoundedContext() string { return boundedContext }
func (e LateFeeApplied) SchemaVersion() int     { return 1 }

// ── LateFeeWaived ─────────────────────────────────────────────────

// LateFeeWaived se publica al perdonar un cargo.
// Consumido por: Notifications (avisar al paciente).
type LateFeeWaived struct {
	QuoteID    uuid.UUID             `json:"quote_id"`
	PatientID  sharedtypes.PatientID `json:"patient_id"`
	FeeID      uuid.UUID             `json:"fee_id"`
	WaivedBy   uuid.UUID             `json:"waived_by"`
	Reason     string                `json:"reason"`
	OccurredAt time.Time             `json:"occurred_at"`
}

func (e LateFeeWaived) EventType() string      { return "billing.late_fee_waived" }
func (e LateFeeWaived) AggregateID() string    { return e.QuoteID.String() }
func (e LateFeeWaived) AggregateType() string  { return "Quote" }
func (e LateFeeWaived) BoundedContext() string { return boundedContext }
func (e LateFeeWaived) SchemaVersion() int     { return 1 }

// ── RefundIssued ──────────────────────────────────────────────────

// RefundIssued se publica al emitir una devolución.
// Consumido por: Notifications (avisar al paciente).
type RefundIssued struct {
	QuoteID     uuid.UUID             `json:"quote_id"`
	PatientID   sharedtypes.PatientID `json:"patient_id"`
	AmountCents int64                 `json:"amount_cents"`
	Reason      string                `json:"reason"`
	OccurredAt  time.Time             `json:"occurred_at"`
}

func (e RefundIssued) EventType() string      { return "billing.refund_issued" }
func (e RefundIssued) AggregateID() string    { return e.QuoteID.String() }
func (e RefundIssued) AggregateType() string  { return "Quote" }
func (e RefundIssued) BoundedContext() string { return boundedContext }
func (e RefundIssued) SchemaVersion() int     { return 1 }
