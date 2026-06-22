// Package valueobject define los Value Objects del bounded context Billing & Payments.
package valueobject

import "fmt"

// ── QuoteStatus ───────────────────────────────────────────────────

// QuoteStatus es el estado del ciclo de vida de un presupuesto.
// Las transiciones válidas están enforced en el Aggregate Quote.
type QuoteStatus string

const (
	QuoteStatusDraft       QuoteStatus = "Draft"       // creado, appointment aún no confirmado
	QuoteStatusConfirmed   QuoteStatus = "Confirmed"   // appointment confirmado, esperando pago
	QuoteStatusPartialPaid QuoteStatus = "PartialPaid" // pago parcial recibido (anticipo)
	QuoteStatusPaid        QuoteStatus = "Paid"        // pago total recibido
	QuoteStatusVoided      QuoteStatus = "Voided"      // anulado sin cobro (terminal)
	QuoteStatusRefunded    QuoteStatus = "Refunded"    // devuelto (terminal)
	QuoteStatusChargedFee  QuoteStatus = "ChargedFee"  // cargo por cancelación/no-show aplicado
)

func ParseQuoteStatus(s string) (QuoteStatus, error) {
	switch QuoteStatus(s) {
	case QuoteStatusDraft, QuoteStatusConfirmed, QuoteStatusPartialPaid,
		QuoteStatusPaid, QuoteStatusVoided, QuoteStatusRefunded, QuoteStatusChargedFee:
		return QuoteStatus(s), nil
	}
	return "", fmt.Errorf("estado de presupuesto inválido: '%s'", s)
}

func (s QuoteStatus) String() string { return string(s) }

// IsActive reporta si el Quote puede recibir pagos.
func (s QuoteStatus) IsActive() bool {
	return s == QuoteStatusConfirmed || s == QuoteStatusPartialPaid
}

// IsTerminal reporta si el Quote no puede transicionar más.
func (s QuoteStatus) IsTerminal() bool {
	return s == QuoteStatusVoided || s == QuoteStatusRefunded
}

// ── PaymentMethod ─────────────────────────────────────────────────

type PaymentMethod string

const (
	PaymentMethodEfectivo      PaymentMethod = "Efectivo"
	PaymentMethodTarjeta       PaymentMethod = "Tarjeta"
	PaymentMethodTransferencia PaymentMethod = "Transferencia"
	PaymentMethodMercadoPago   PaymentMethod = "MercadoPago"
	PaymentMethodDebito        PaymentMethod = "Debito"
)

func ParsePaymentMethod(s string) (PaymentMethod, error) {
	switch PaymentMethod(s) {
	case PaymentMethodEfectivo, PaymentMethodTarjeta, PaymentMethodTransferencia,
		PaymentMethodMercadoPago, PaymentMethodDebito:
		return PaymentMethod(s), nil
	}
	return "", fmt.Errorf("método de pago inválido: '%s'", s)
}

func (m PaymentMethod) String() string { return string(m) }

// IsImmediate reporta si el método de pago se confirma de forma inmediata
// (sin esperar webhook externo).
func (m PaymentMethod) IsImmediate() bool {
	return m == PaymentMethodEfectivo ||
		m == PaymentMethodTarjeta ||
		m == PaymentMethodTransferencia ||
		m == PaymentMethodDebito
}

// ── PaymentStatus ─────────────────────────────────────────────────

type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "Pending"   // iniciado, esperando confirmación
	PaymentStatusConfirmed PaymentStatus = "Confirmed" // cobro confirmado
	PaymentStatusFailed    PaymentStatus = "Failed"    // falló el cobro
	PaymentStatusRefunded  PaymentStatus = "Refunded"  // devuelto
)

func (s PaymentStatus) String() string    { return string(s) }
func (s PaymentStatus) IsConfirmed() bool { return s == PaymentStatusConfirmed }

// ── CoPayType ─────────────────────────────────────────────────────

// CoPayType define cómo se calcula el copago del paciente.
// Refleja el mismo type de Coverage BC (no importamos de allí para evitar acoplamiento).
type CoPayType string

const (
	CoPayTypePercent     CoPayType = "Percent"
	CoPayTypeFixedAmount CoPayType = "FixedAmount"
	CoPayTypeNone        CoPayType = "None"
)

func ParseCoPayType(s string) (CoPayType, error) {
	switch CoPayType(s) {
	case CoPayTypePercent, CoPayTypeFixedAmount, CoPayTypeNone:
		return CoPayType(s), nil
	}
	return "", fmt.Errorf("tipo de copago inválido: '%s'", s)
}

func (c CoPayType) String() string { return string(c) }

// ── LateFeeType ───────────────────────────────────────────────────

type LateFeeType string

const (
	LateFeeTypeLateCancellation LateFeeType = "LateCancellation"
	LateFeeTypeNoShow           LateFeeType = "NoShow"
)

func (t LateFeeType) String() string { return string(t) }

// ── LateFeeStatus ─────────────────────────────────────────────────

type LateFeeStatus string

const (
	LateFeeStatusPending LateFeeStatus = "Pending"
	LateFeeStatusPaid    LateFeeStatus = "Paid"
	LateFeeStatusWaived  LateFeeStatus = "Waived" // perdonado por admin
)

func (s LateFeeStatus) String() string { return string(s) }

// ── CancellationPolicy ────────────────────────────────────────────

// CancellationPolicy define las reglas de cargo por cancelación/no-show de una sede.
// Se captura como snapshot en el Quote al momento de crearlo.
type CancellationPolicy struct {
	FreeHours               int   // horas de anticipación para cancelar sin cargo
	LateCancellationPercent int   // % del copago que se cobra por cancelación tardía
	NoShowPercent           int   // % del copago que se cobra por no-show
	MinFeeCents             int64 // mínimo a cobrar (aunque el % resulte menor)
}

// DefaultCancellationPolicy retorna la política por defecto del sistema.
func DefaultCancellationPolicy() CancellationPolicy {
	return CancellationPolicy{
		FreeHours:               24,
		LateCancellationPercent: 50,
		NoShowPercent:           100,
		MinFeeCents:             0,
	}
}

// ── QuoteAmounts — resultado del cálculo ──────────────────────────

// QuoteAmounts es el resultado del BillingCalculator.
type QuoteAmounts struct {
	CoverageAmountCents int64
	CoPayAmountCents    int64
}
