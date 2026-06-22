// Package payment contiene los adaptadores de pasarelas de pago del BC Billing.
package payment

import (
	"context"
	"log/slog"
)

// ── PaymentAdapter — puerto de salida ─────────────────────────────

// PaymentAdapter es el puerto de salida para pasarelas de pago externas.
// En Fases 1-3 (MVP) solo se usan métodos inmediatos (efectivo, transferencia),
// que no requieren llamadas externas. El adapter real (MercadoPago) se implementa en Fase 5.
type PaymentAdapter interface {
	// IsSupported reporta si este adapter maneja el método de pago dado.
	IsSupported(method string) bool
}

// ── NoopAdapter — stub para métodos inmediatos ────────────────────

// NoopAdapter es el adaptador para métodos de pago inmediatos
// (Efectivo, Tarjeta, Transferencia, Débito).
// No requiere llamadas externas: la confirmación es instantánea.
type NoopAdapter struct {
	logger *slog.Logger
}

func NewNoopAdapter() *NoopAdapter {
	return &NoopAdapter{
		logger: slog.Default().With("component", "payment.noop"),
	}
}

func (a *NoopAdapter) IsSupported(method string) bool {
	switch method {
	case "Efectivo", "Tarjeta", "Transferencia", "Debito":
		return true
	}
	return false
}

// ConfirmImmediately simula la confirmación instantánea de un pago.
// Para métodos inmediatos no hay llamada externa: el operador ingresa
// el pago y se confirma en el acto.
func (a *NoopAdapter) ConfirmImmediately(ctx context.Context, method, notes string) error {
	a.logger.InfoContext(ctx, "pago confirmado inmediatamente",
		"method", method,
		"notes", notes,
	)
	return nil
}

// ── MercadoPagoAdapter — stub para Fase 5 ────────────────────────

// MercadoPagoAdapter será implementado en Fase 5.
// Por ahora solo registra los métodos para que el código compile.
type MercadoPagoAdapter struct {
	logger *slog.Logger
}

func NewMercadoPagoAdapter() *MercadoPagoAdapter {
	return &MercadoPagoAdapter{
		logger: slog.Default().With("component", "payment.mercadopago"),
	}
}

func (a *MercadoPagoAdapter) IsSupported(method string) bool {
	return method == "MercadoPago"
}

// InitiatePayment inicia un cobro en MercadoPago. Stub: retorna error.
// Implementación real pendiente en Fase 5.
func (a *MercadoPagoAdapter) InitiatePayment(_ context.Context, _ int64, _ string) (string, error) {
	return "", nil // stub: en Fase 5 retorna preferenceID
}
