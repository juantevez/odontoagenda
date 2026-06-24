// Package payment contiene los adaptadores de pasarelas de pago del BC Billing.
package payment

import (
	"context"
	"log/slog"
)

// ── NoopAdapter — métodos inmediatos ──────────────────────────────

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

func (a *NoopAdapter) ConfirmImmediately(ctx context.Context, method, notes string) error {
	a.logger.InfoContext(ctx, "pago confirmado inmediatamente",
		"method", method,
		"notes", notes,
	)
	return nil
}
