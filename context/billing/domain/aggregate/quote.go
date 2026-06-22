// Package aggregate contiene el Aggregate Root del bounded context Billing.
//
// Aggregate Root: Quote (Presupuesto)
// Entidades internas: Payment, LateFee
package aggregate

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/billing/domain/event"
	"github.com/juantevez/odontoagenda/context/billing/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── Payment — Entidad dentro de Quote ────────────────────────────

// Payment representa un pago registrado contra un presupuesto.
// Un Quote puede tener múltiples Payments (anticipo + saldo).
type Payment struct {
	id                uuid.UUID
	amountCents       int64
	paymentMethod     valueobject.PaymentMethod
	status            valueobject.PaymentStatus
	externalReference *string // ID de transacción en pasarela externa (MP, etc.)
	paidAt            *time.Time
	receiptNumber     *string
	notes             string
	createdAt         time.Time
}

func newPayment(
	amountCents int64,
	method valueobject.PaymentMethod,
	externalRef *string,
	notes string,
) Payment {
	return Payment{
		id:                uuid.New(),
		amountCents:       amountCents,
		paymentMethod:     method,
		status:            valueobject.PaymentStatusPending,
		externalReference: externalRef,
		notes:             strings.TrimSpace(notes),
		createdAt:         time.Now().UTC(),
	}
}

func (p Payment) ID() uuid.UUID                          { return p.id }
func (p Payment) AmountCents() int64                     { return p.amountCents }
func (p Payment) PaymentMethod() valueobject.PaymentMethod { return p.paymentMethod }
func (p Payment) Status() valueobject.PaymentStatus      { return p.status }
func (p Payment) ExternalReference() *string             { return p.externalReference }
func (p Payment) PaidAt() *time.Time                     { return p.paidAt }
func (p Payment) ReceiptNumber() *string                 { return p.receiptNumber }
func (p Payment) Notes() string                          { return p.notes }
func (p Payment) CreatedAt() time.Time                   { return p.createdAt }

// ── LateFee — Entidad dentro de Quote ────────────────────────────

// LateFee es el cargo que se aplica por cancelación tardía o no-show.
type LateFee struct {
	id           uuid.UUID
	feeType      valueobject.LateFeeType
	amountCents  int64
	status       valueobject.LateFeeStatus
	waivedBy     *uuid.UUID
	waivedReason string
	createdAt    time.Time
}

func newLateFee(feeType valueobject.LateFeeType, amountCents int64) LateFee {
	return LateFee{
		id:          uuid.New(),
		feeType:     feeType,
		amountCents: amountCents,
		status:      valueobject.LateFeeStatusPending,
		createdAt:   time.Now().UTC(),
	}
}

func (f LateFee) ID() uuid.UUID                      { return f.id }
func (f LateFee) FeeType() valueobject.LateFeeType   { return f.feeType }
func (f LateFee) AmountCents() int64                 { return f.amountCents }
func (f LateFee) Status() valueobject.LateFeeStatus  { return f.status }
func (f LateFee) WaivedBy() *uuid.UUID               { return f.waivedBy }
func (f LateFee) WaivedReason() string               { return f.waivedReason }
func (f LateFee) CreatedAt() time.Time               { return f.createdAt }

// ── Máquina de estados ────────────────────────────────────────────

var validTransitions = map[valueobject.QuoteStatus][]valueobject.QuoteStatus{
	valueobject.QuoteStatusDraft: {
		valueobject.QuoteStatusConfirmed,
		valueobject.QuoteStatusVoided,
	},
	valueobject.QuoteStatusConfirmed: {
		valueobject.QuoteStatusPartialPaid,
		valueobject.QuoteStatusPaid,
		valueobject.QuoteStatusVoided,
		valueobject.QuoteStatusChargedFee,
	},
	valueobject.QuoteStatusPartialPaid: {
		valueobject.QuoteStatusPaid,
		valueobject.QuoteStatusVoided,
		valueobject.QuoteStatusChargedFee,
	},
	valueobject.QuoteStatusPaid: {
		valueobject.QuoteStatusRefunded,
	},
	valueobject.QuoteStatusChargedFee: {
		valueobject.QuoteStatusPaid,
	},
	valueobject.QuoteStatusVoided:   {},
	valueobject.QuoteStatusRefunded: {},
}

func canTransition(from, to valueobject.QuoteStatus) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// ── Quote — Aggregate Root ────────────────────────────────────────

// Quote es el Aggregate Root del bounded context Billing.
// Representa el presupuesto económico de una cita y gestiona todo
// el ciclo de pagos asociados.
//
// Invariantes:
//   INV-01: coverageAmountCents + coPayAmountCents = arancelCents
//   INV-02: coPayAmountCents >= 0
//   INV-03: Voided o Refunded no aceptan nuevos Payments
//   INV-04: total Payments Confirmed <= coPayAmountCents
//   INV-05: un solo Quote activo por appointmentID (enforced en repo + DB)
//   INV-06: LateFee solo puede ser Waived por admin
//   INV-07: si CoPayType = None → coPayAmountCents = 0
//   INV-08: Privado → coverageAmountCents = 0, coPayAmountCents = arancelCents
type Quote struct {
	id             sharedtypes.AppointmentID // mismo UUID que el appointment (1:1)
	appointmentID  uuid.UUID
	patientID      sharedtypes.PatientID
	clinicID       sharedtypes.ClinicID
	professionalID sharedtypes.ProfessionalID
	procedureCode  string
	procedureDesc  string

	status valueobject.QuoteStatus

	// Montos en centavos ARS
	arancelCents        int64
	coveragePercent     int
	coverageAmountCents int64
	coPayType           valueobject.CoPayType
	coPayAmountCents    int64

	// Cobertura
	coverageType    string
	agreementID     *uuid.UUID
	planID          *uuid.UUID

	// Autorización
	requiresAuthorization bool
	authorizationCode     *string

	// Política de cancelación (snapshot al crear)
	cancellationPolicy valueobject.CancellationPolicy

	// Flag: Coverage BC no respondió al crear, montos son estimados
	pendingCoverageCheck bool

	// Entidades internas
	payments  []Payment
	lateFees  []LateFee

	// Slot de la cita (para calcular cancelación tardía)
	slotStart time.Time
	slotEnd   time.Time

	createdAt time.Time
	updatedAt time.Time
	version   int64

	pendingEvents []event.DomainEvent
}

// NewQuote crea un Quote en estado Draft a partir del evento appointment.booked.
func NewQuote(
	appointmentID uuid.UUID,
	patientID sharedtypes.PatientID,
	clinicID sharedtypes.ClinicID,
	professionalID sharedtypes.ProfessionalID,
	procedureCode, procedureDesc string,
	arancelCents int64,
	coveragePercent int,
	coverageAmountCents int64,
	coPayType valueobject.CoPayType,
	coPayAmountCents int64,
	coverageType string,
	agreementID, planID *uuid.UUID,
	requiresAuthorization bool,
	policy valueobject.CancellationPolicy,
	pendingCoverageCheck bool,
	slotStart, slotEnd time.Time,
) (*Quote, error) {
	if arancelCents <= 0 {
		return nil, sharederrors.NewInvalidArgument("arancel_cents", "debe ser mayor a 0")
	}
	if coverageAmountCents < 0 {
		return nil, sharederrors.NewInvalidArgument("coverage_amount_cents", "no puede ser negativo")
	}
	if coPayAmountCents < 0 {
		return nil, sharederrors.NewInvalidArgument("co_pay_amount_cents", "no puede ser negativo")
	}

	now := time.Now().UTC()
	q := &Quote{
		id:                  appointmentID, // el Quote usa el mismo UUID que el Appointment
		appointmentID:       appointmentID,
		patientID:           patientID,
		clinicID:            clinicID,
		professionalID:      professionalID,
		procedureCode:       procedureCode,
		procedureDesc:       procedureDesc,
		status:              valueobject.QuoteStatusDraft,
		arancelCents:        arancelCents,
		coveragePercent:     coveragePercent,
		coverageAmountCents: coverageAmountCents,
		coPayType:           coPayType,
		coPayAmountCents:    coPayAmountCents,
		coverageType:        coverageType,
		agreementID:         agreementID,
		planID:              planID,
		requiresAuthorization: requiresAuthorization,
		cancellationPolicy:  policy,
		pendingCoverageCheck: pendingCoverageCheck,
		payments:            []Payment{},
		lateFees:            []LateFee{},
		slotStart:           slotStart,
		slotEnd:             slotEnd,
		createdAt:           now,
		updatedAt:           now,
		version:             1,
		pendingEvents:       []event.DomainEvent{},
	}

	q.pendingEvents = append(q.pendingEvents, event.QuoteCreated{
		QuoteID:             q.id,
		AppointmentID:       appointmentID,
		PatientID:           patientID,
		ClinicID:            clinicID,
		ProcedureCode:       procedureCode,
		ArancelCents:        arancelCents,
		CoverageAmountCents: coverageAmountCents,
		CoPayAmountCents:    coPayAmountCents,
		OccurredAt:          now,
	})
	return q, nil
}

// ── Transiciones de estado ────────────────────────────────────────

// Confirm transiciona el Quote de Draft a Confirmed
// (al recibir appointment.confirmed).
func (q *Quote) Confirm() error {
	if !canTransition(q.status, valueobject.QuoteStatusConfirmed) {
		return sharederrors.NewPrecondition("invalid_transition",
			fmt.Sprintf("no se puede confirmar un Quote en estado '%s'", q.status))
	}
	q.status = valueobject.QuoteStatusConfirmed
	q.updatedAt = time.Now().UTC()
	q.version++
	return nil
}

// Void anula el Quote sin cobro.
func (q *Quote) Void(reason string) error {
	if q.status.IsTerminal() {
		return sharederrors.NewPrecondition("invalid_transition",
			fmt.Sprintf("no se puede anular un Quote en estado '%s'", q.status))
	}
	if !canTransition(q.status, valueobject.QuoteStatusVoided) {
		return sharederrors.NewPrecondition("invalid_transition",
			fmt.Sprintf("no se puede anular un Quote en estado '%s'", q.status))
	}
	q.status = valueobject.QuoteStatusVoided
	q.updatedAt = time.Now().UTC()
	q.version++

	q.pendingEvents = append(q.pendingEvents, event.QuoteVoided{
		QuoteID:       q.id,
		AppointmentID: q.appointmentID,
		PatientID:     q.patientID,
		Reason:        reason,
		OccurredAt:    time.Now().UTC(),
	})
	return nil
}

// SetAuthorizationCode actualiza el código de autorización de la prepaga.
func (q *Quote) SetAuthorizationCode(code string) {
	q.authorizationCode = &code
	q.updatedAt = time.Now().UTC()
	q.version++
}

// ── Registro de pagos ─────────────────────────────────────────────

// AddPayment registra un nuevo pago contra el Quote.
// INV-03: no acepta pagos si está Voided o Refunded.
// INV-04: el total no puede superar coPayAmountCents.
func (q *Quote) AddPayment(
	amountCents int64,
	method valueobject.PaymentMethod,
	externalRef *string,
	notes string,
) (*Payment, error) {
	if q.status.IsTerminal() {
		return nil, sharederrors.NewPrecondition("quote_not_terminal",
			fmt.Sprintf("no se puede registrar un pago en un Quote en estado '%s'", q.status))
	}
	if !q.status.IsActive() {
		return nil, sharederrors.NewPrecondition("quote_active",
			fmt.Sprintf("el Quote debe estar Confirmed o PartialPaid para recibir pagos, estado actual: '%s'", q.status))
	}
	if amountCents <= 0 {
		return nil, sharederrors.NewInvalidArgument("amount_cents", "debe ser mayor a 0")
	}

	pending := q.PendingAmountCents()
	if amountCents > pending {
		return nil, sharederrors.NewInvalidArgument("amount_cents",
			fmt.Sprintf("el monto (%d) supera el saldo pendiente (%d centavos)", amountCents, pending))
	}

	payment := newPayment(amountCents, method, externalRef, notes)

	// Para métodos inmediatos: confirmar directamente.
	if method.IsImmediate() {
		now := time.Now().UTC()
		payment.status = valueobject.PaymentStatusConfirmed
		payment.paidAt = &now
	}

	q.payments = append(q.payments, payment)
	q.updatedAt = time.Now().UTC()
	q.version++

	// Recalcular estado del Quote.
	if payment.status.IsConfirmed() {
		q.recalculateStatus()
		if payment.status.IsConfirmed() {
			q.publishPaymentEvent(payment)
		}
	}

	return &payment, nil
}

// ConfirmPayment confirma un Payment que estaba Pending (ej: callback de MercadoPago).
func (q *Quote) ConfirmPayment(paymentID uuid.UUID, externalRef string) error {
	for i := range q.payments {
		if q.payments[i].id == paymentID {
			if q.payments[i].status != valueobject.PaymentStatusPending {
				return sharederrors.NewPrecondition("payment_pending",
					"solo se pueden confirmar pagos en estado Pending")
			}
			now := time.Now().UTC()
			q.payments[i].status = valueobject.PaymentStatusConfirmed
			q.payments[i].paidAt = &now
			if externalRef != "" {
				q.payments[i].externalReference = &externalRef
			}
			q.updatedAt = now
			q.version++

			q.recalculateStatus()
			q.publishPaymentEvent(q.payments[i])
			return nil
		}
	}
	return sharederrors.NewNotFound("Payment", paymentID.String())
}

// FailPayment marca un Payment como fallido.
func (q *Quote) FailPayment(paymentID uuid.UUID) error {
	for i := range q.payments {
		if q.payments[i].id == paymentID {
			if q.payments[i].status != valueobject.PaymentStatusPending {
				return sharederrors.NewPrecondition("payment_pending",
					"solo se pueden fallar pagos en estado Pending")
			}
			q.payments[i].status = valueobject.PaymentStatusFailed
			q.updatedAt = time.Now().UTC()
			q.version++
			return nil
		}
	}
	return sharederrors.NewNotFound("Payment", paymentID.String())
}

// ── LateFees ──────────────────────────────────────────────────────

// ApplyLateFee aplica un cargo por cancelación tardía o no-show.
func (q *Quote) ApplyLateFee(feeType valueobject.LateFeeType) (*LateFee, error) {
	if q.status.IsTerminal() {
		return nil, sharederrors.NewPrecondition("quote_not_terminal",
			"no se puede aplicar cargo a un Quote anulado o devuelto")
	}

	amountCents := q.calculateLateFeeAmount(feeType)
	if amountCents == 0 {
		// Sin cargo configurado: void el quote directamente.
		return nil, nil
	}

	fee := newLateFee(feeType, amountCents)
	q.lateFees = append(q.lateFees, fee)

	if canTransition(q.status, valueobject.QuoteStatusChargedFee) {
		q.status = valueobject.QuoteStatusChargedFee
	}
	q.updatedAt = time.Now().UTC()
	q.version++

	q.pendingEvents = append(q.pendingEvents, event.LateFeeApplied{
		QuoteID:       q.id,
		AppointmentID: q.appointmentID,
		PatientID:     q.patientID,
		FeeType:       string(feeType),
		AmountCents:   amountCents,
		OccurredAt:    time.Now().UTC(),
	})

	return &fee, nil
}

// WaiveLateFee perdona un cargo por cancelación/no-show.
func (q *Quote) WaiveLateFee(feeID uuid.UUID, waivedBy uuid.UUID, reason string) error {
	for i := range q.lateFees {
		if q.lateFees[i].id == feeID {
			if q.lateFees[i].status != valueobject.LateFeeStatusPending {
				return sharederrors.NewPrecondition("late_fee_pending",
					"solo se pueden perdonar cargos en estado Pending")
			}
			q.lateFees[i].status = valueobject.LateFeeStatusWaived
			q.lateFees[i].waivedBy = &waivedBy
			q.lateFees[i].waivedReason = strings.TrimSpace(reason)
			q.updatedAt = time.Now().UTC()
			q.version++

			q.pendingEvents = append(q.pendingEvents, event.LateFeeWaived{
				QuoteID:     q.id,
				PatientID:   q.patientID,
				FeeID:       feeID,
				WaivedBy:    waivedBy,
				Reason:      reason,
				OccurredAt:  time.Now().UTC(),
			})
			return nil
		}
	}
	return sharederrors.NewNotFound("LateFee", feeID.String())
}

// Refund emite una devolución total del Quote.
func (q *Quote) Refund(reason string) error {
	if q.status != valueobject.QuoteStatusPaid {
		return sharederrors.NewPrecondition("quote_paid",
			"solo se pueden devolver Quotes en estado Paid")
	}
	q.status = valueobject.QuoteStatusRefunded
	q.updatedAt = time.Now().UTC()
	q.version++

	totalPaid := q.TotalConfirmedCents()
	q.pendingEvents = append(q.pendingEvents, event.RefundIssued{
		QuoteID:     q.id,
		PatientID:   q.patientID,
		AmountCents: totalPaid,
		Reason:      reason,
		OccurredAt:  time.Now().UTC(),
	})
	return nil
}

// ── Queries sobre el estado interno ──────────────────────────────

// TotalConfirmedCents retorna la suma de todos los Payments confirmados.
func (q *Quote) TotalConfirmedCents() int64 {
	var total int64
	for _, p := range q.payments {
		if p.status.IsConfirmed() {
			total += p.amountCents
		}
	}
	return total
}

// PendingAmountCents retorna el saldo pendiente de pago.
func (q *Quote) PendingAmountCents() int64 {
	pending := q.coPayAmountCents - q.TotalConfirmedCents()
	if pending < 0 {
		return 0
	}
	return pending
}

// IsLateCancellation evalúa si una cancelación a esta hora es tardía.
func (q *Quote) IsLateCancellation() bool {
	hoursUntilSlot := q.slotStart.Sub(time.Now().UTC()).Hours()
	return hoursUntilSlot < float64(q.cancellationPolicy.FreeHours)
}

// BumpVersion incrementa la versión tras persistencia exitosa.
func (q *Quote) BumpVersion() { q.version++ }

// PendingEvents retorna y limpia los eventos pendientes.
func (q *Quote) PendingEvents() []event.DomainEvent {
	evts := q.pendingEvents
	q.pendingEvents = nil
	return evts
}

// ── Getters ───────────────────────────────────────────────────────

func (q *Quote) ID() uuid.UUID                              { return q.id }
func (q *Quote) AppointmentID() uuid.UUID                   { return q.appointmentID }
func (q *Quote) PatientID() sharedtypes.PatientID           { return q.patientID }
func (q *Quote) ClinicID() sharedtypes.ClinicID             { return q.clinicID }
func (q *Quote) ProfessionalID() sharedtypes.ProfessionalID { return q.professionalID }
func (q *Quote) ProcedureCode() string                      { return q.procedureCode }
func (q *Quote) ProcedureDesc() string                      { return q.procedureDesc }
func (q *Quote) Status() valueobject.QuoteStatus            { return q.status }
func (q *Quote) ArancelCents() int64                        { return q.arancelCents }
func (q *Quote) CoveragePercent() int                       { return q.coveragePercent }
func (q *Quote) CoverageAmountCents() int64                 { return q.coverageAmountCents }
func (q *Quote) CoPayType() valueobject.CoPayType           { return q.coPayType }
func (q *Quote) CoPayAmountCents() int64                    { return q.coPayAmountCents }
func (q *Quote) CoverageType() string                       { return q.coverageType }
func (q *Quote) AgreementID() *uuid.UUID                    { return q.agreementID }
func (q *Quote) PlanID() *uuid.UUID                         { return q.planID }
func (q *Quote) RequiresAuthorization() bool                { return q.requiresAuthorization }
func (q *Quote) AuthorizationCode() *string                 { return q.authorizationCode }
func (q *Quote) CancellationPolicy() valueobject.CancellationPolicy { return q.cancellationPolicy }
func (q *Quote) PendingCoverageCheck() bool                 { return q.pendingCoverageCheck }
func (q *Quote) Payments() []Payment                        { return q.payments }
func (q *Quote) LateFees() []LateFee                        { return q.lateFees }
func (q *Quote) SlotStart() time.Time                       { return q.slotStart }
func (q *Quote) SlotEnd() time.Time                         { return q.slotEnd }
func (q *Quote) CreatedAt() time.Time                       { return q.createdAt }
func (q *Quote) UpdatedAt() time.Time                       { return q.updatedAt }
func (q *Quote) Version() int64                             { return q.version }

// Reconstitute reconstruye un Quote desde persistencia sin disparar eventos.
func Reconstitute(
	id uuid.UUID,
	appointmentID uuid.UUID,
	patientID sharedtypes.PatientID,
	clinicID sharedtypes.ClinicID,
	professionalID sharedtypes.ProfessionalID,
	procedureCode, procedureDesc string,
	status valueobject.QuoteStatus,
	arancelCents int64,
	coveragePercent int,
	coverageAmountCents int64,
	coPayType valueobject.CoPayType,
	coPayAmountCents int64,
	coverageType string,
	agreementID, planID *uuid.UUID,
	requiresAuthorization bool,
	authorizationCode *string,
	policy valueobject.CancellationPolicy,
	pendingCoverageCheck bool,
	payments []Payment,
	lateFees []LateFee,
	slotStart, slotEnd time.Time,
	createdAt, updatedAt time.Time,
	version int64,
) *Quote {
	return &Quote{
		id:                  id,
		appointmentID:       appointmentID,
		patientID:           patientID,
		clinicID:            clinicID,
		professionalID:      professionalID,
		procedureCode:       procedureCode,
		procedureDesc:       procedureDesc,
		status:              status,
		arancelCents:        arancelCents,
		coveragePercent:     coveragePercent,
		coverageAmountCents: coverageAmountCents,
		coPayType:           coPayType,
		coPayAmountCents:    coPayAmountCents,
		coverageType:        coverageType,
		agreementID:         agreementID,
		planID:              planID,
		requiresAuthorization: requiresAuthorization,
		authorizationCode:   authorizationCode,
		cancellationPolicy:  policy,
		pendingCoverageCheck: pendingCoverageCheck,
		payments:            payments,
		lateFees:            lateFees,
		slotStart:           slotStart,
		slotEnd:             slotEnd,
		createdAt:           createdAt,
		updatedAt:           updatedAt,
		version:             version,
		pendingEvents:       []event.DomainEvent{},
	}
}

// ReconstitutePayment reconstruye un Payment desde persistencia.
func ReconstitutePayment(
	id uuid.UUID,
	amountCents int64,
	method valueobject.PaymentMethod,
	status valueobject.PaymentStatus,
	externalRef *string,
	paidAt *time.Time,
	receiptNumber *string,
	notes string,
	createdAt time.Time,
) Payment {
	return Payment{
		id: id, amountCents: amountCents, paymentMethod: method,
		status: status, externalReference: externalRef, paidAt: paidAt,
		receiptNumber: receiptNumber, notes: notes, createdAt: createdAt,
	}
}

// ReconstituteLateFee reconstruye un LateFee desde persistencia.
func ReconstituteLateFee(
	id uuid.UUID,
	feeType valueobject.LateFeeType,
	amountCents int64,
	status valueobject.LateFeeStatus,
	waivedBy *uuid.UUID,
	waivedReason string,
	createdAt time.Time,
) LateFee {
	return LateFee{
		id: id, feeType: feeType, amountCents: amountCents,
		status: status, waivedBy: waivedBy, waivedReason: waivedReason,
		createdAt: createdAt,
	}
}

// ── helpers internos ──────────────────────────────────────────────

// recalculateStatus actualiza el estado del Quote tras confirmar un pago.
func (q *Quote) recalculateStatus() {
	total := q.TotalConfirmedCents()
	if total >= q.coPayAmountCents {
		q.status = valueobject.QuoteStatusPaid
		q.pendingEvents = append(q.pendingEvents, event.QuotePaid{
			QuoteID:       q.id,
			AppointmentID: q.appointmentID,
			PatientID:     q.patientID,
			TotalPaidCents: total,
			OccurredAt:    time.Now().UTC(),
		})
	} else if total > 0 {
		q.status = valueobject.QuoteStatusPartialPaid
	}
}

func (q *Quote) publishPaymentEvent(p Payment) {
	q.pendingEvents = append(q.pendingEvents, event.PaymentReceived{
		QuoteID:       q.id,
		AppointmentID: q.appointmentID,
		PatientID:     q.patientID,
		AmountCents:   p.amountCents,
		PaymentMethod: string(p.paymentMethod),
		OccurredAt:    time.Now().UTC(),
	})
}

// calculateLateFeeAmount calcula el monto del cargo según la política y el tipo.
func (q *Quote) calculateLateFeeAmount(feeType valueobject.LateFeeType) int64 {
	var percent int
	switch feeType {
	case valueobject.LateFeeTypeLateCancellation:
		percent = q.cancellationPolicy.LateCancellationPercent
	case valueobject.LateFeeTypeNoShow:
		percent = q.cancellationPolicy.NoShowPercent
	}

	if percent == 0 {
		return 0
	}

	amount := q.coPayAmountCents * int64(percent) / 100
	if amount < q.cancellationPolicy.MinFeeCents {
		amount = q.cancellationPolicy.MinFeeCents
	}
	// El cargo nunca puede superar el copago original.
	if amount > q.coPayAmountCents {
		amount = q.coPayAmountCents
	}
	return amount
}
