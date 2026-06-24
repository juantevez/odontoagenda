// Package command — handlers de Fases 5 y 7: MercadoPago y devoluciones.
package command

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/billing/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/billing/domain/repository"
	"github.com/juantevez/odontoagenda/context/billing/domain/valueobject"
	"github.com/juantevez/odontoagenda/context/billing/infrastructure/payment"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
)

// ── InitMPPaymentCommand ──────────────────────────────────────────

type InitMPPaymentCommand struct {
	QuoteID        uuid.UUID
	AmountCents    int64
	PatientName    string
	ProcedureDesc  string
	BackURLSuccess string
	BackURLFailure string
	BackURLPending string
	WebhookURL     string
}

type InitMPPaymentResult struct {
	PaymentID    uuid.UUID
	PreferenceID string
	InitPoint    string
	SandboxURL   string
}

type InitMPPaymentHandler struct {
	repo      repository.QuoteRepository
	mpAdapter *payment.MercadoPagoAdapter
	eventBus  events.Bus
	logger    *slog.Logger
}

func NewInitMPPaymentHandler(
	repo repository.QuoteRepository,
	mpAdapter *payment.MercadoPagoAdapter,
	eventBus events.Bus,
) *InitMPPaymentHandler {
	return &InitMPPaymentHandler{
		repo:      repo,
		mpAdapter: mpAdapter,
		eventBus:  eventBus,
		logger:    slog.Default().With("handler", "InitMPPayment"),
	}
}

func (h *InitMPPaymentHandler) Handle(ctx context.Context, cmd InitMPPaymentCommand) (InitMPPaymentResult, error) {
	quote, err := h.repo.FindByID(ctx, cmd.QuoteID)
	if err != nil {
		return InitMPPaymentResult{}, err
	}

	if !quote.Status().IsActive() {
		return InitMPPaymentResult{}, sharederrors.NewPrecondition("quote_active",
			fmt.Sprintf("el Quote debe estar Confirmed o PartialPaid, estado actual: '%s'", quote.Status()))
	}

	pending := quote.PendingAmountCents()
	if cmd.AmountCents <= 0 || cmd.AmountCents > pending {
		return InitMPPaymentResult{}, sharederrors.NewInvalidArgument("amount_cents",
			fmt.Sprintf("monto inválido: %d (pendiente: %d)", cmd.AmountCents, pending))
	}

	prefResp, err := h.mpAdapter.CreatePreference(ctx, payment.PreferenceRequest{
		QuoteID:        cmd.QuoteID.String(),
		PatientName:    cmd.PatientName,
		ProcedureDesc:  cmd.ProcedureDesc,
		AmountCents:    cmd.AmountCents,
		ExternalRef:    cmd.QuoteID.String(),
		BackURLSuccess: cmd.BackURLSuccess,
		BackURLFailure: cmd.BackURLFailure,
		BackURLPending: cmd.BackURLPending,
		WebhookURL:     cmd.WebhookURL,
	})
	if err != nil {
		return InitMPPaymentResult{}, fmt.Errorf("InitMPPayment: create preference: %w", err)
	}

	externalRef := prefResp.PreferenceID
	mpPayment, err := quote.AddPayment(
		cmd.AmountCents,
		valueobject.PaymentMethodMercadoPago,
		&externalRef,
		fmt.Sprintf("MercadoPago preference %s", prefResp.PreferenceID),
	)
	if err != nil {
		return InitMPPaymentResult{}, err
	}

	if err := h.repo.Update(ctx, quote); err != nil {
		return InitMPPaymentResult{}, fmt.Errorf("InitMPPayment: update: %w", err)
	}

	publishEvents(ctx, h.eventBus, quote.PendingEvents(), h.logger)

	h.logger.InfoContext(ctx, "pago MP iniciado",
		"quote_id", cmd.QuoteID,
		"payment_id", mpPayment.ID(),
		"preference_id", prefResp.PreferenceID,
		"amount_cents", cmd.AmountCents,
	)

	return InitMPPaymentResult{
		PaymentID:    mpPayment.ID(),
		PreferenceID: prefResp.PreferenceID,
		InitPoint:    prefResp.InitPoint,
		SandboxURL:   prefResp.SandboxURL,
	}, nil
}

// ── ConfirmPaymentCommand ─────────────────────────────────────────

type ConfirmPaymentCommand struct {
	QuoteID           uuid.UUID
	MPPaymentID       string
	AmountCents       int64
	ExternalReference string
}

type ConfirmPaymentHandler struct {
	repo     repository.QuoteRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewConfirmPaymentHandler(repo repository.QuoteRepository, eventBus events.Bus) *ConfirmPaymentHandler {
	return &ConfirmPaymentHandler{
		repo:     repo,
		eventBus: eventBus,
		logger:   slog.Default().With("handler", "ConfirmPayment"),
	}
}

func (h *ConfirmPaymentHandler) Handle(ctx context.Context, cmd ConfirmPaymentCommand) error {
	quote, err := h.repo.FindByID(ctx, cmd.QuoteID)
	if err != nil {
		return err
	}

	// Buscar el Payment Pending de MercadoPago en el Quote.
	pendingPaymentID := findPendingMPPayment(quote)
	if pendingPaymentID == uuid.Nil {
		h.logger.WarnContext(ctx, "no hay Payment MP Pending en el Quote, posible duplicado de webhook",
			"quote_id", cmd.QuoteID,
			"mp_payment_id", cmd.MPPaymentID,
		)
		return nil // idempotente
	}

	if err := quote.ConfirmPayment(pendingPaymentID, cmd.MPPaymentID); err != nil {
		return err
	}

	if err := h.repo.Update(ctx, quote); err != nil {
		return fmt.Errorf("ConfirmPayment: update: %w", err)
	}

	publishEvents(ctx, h.eventBus, quote.PendingEvents(), h.logger)

	h.logger.InfoContext(ctx, "pago MP confirmado via webhook",
		"quote_id", cmd.QuoteID,
		"mp_payment_id", cmd.MPPaymentID,
		"quote_status", quote.Status(),
	)
	return nil
}

// ── RefundCommand ─────────────────────────────────────────────────

type RefundCommand struct {
	QuoteID    uuid.UUID
	Reason     string
	RefundedBy uuid.UUID
}

type RefundHandler struct {
	repo      repository.QuoteRepository
	mpAdapter *payment.MercadoPagoAdapter
	eventBus  events.Bus
	logger    *slog.Logger
}

func NewRefundHandler(
	repo repository.QuoteRepository,
	mpAdapter *payment.MercadoPagoAdapter,
	eventBus events.Bus,
) *RefundHandler {
	return &RefundHandler{
		repo:      repo,
		mpAdapter: mpAdapter,
		eventBus:  eventBus,
		logger:    slog.Default().With("handler", "Refund"),
	}
}

// HandleWithAggregate emite la devolución.
// Si el pago fue con MercadoPago: intenta devolución automática via MP.
// Para otros métodos: registra la devolución manual sin llamada externa.
// En ambos casos el Quote queda en estado Refunded.
func (h *RefundHandler) HandleWithAggregate(ctx context.Context, cmd RefundCommand) error {
	quote, err := h.repo.FindByID(ctx, cmd.QuoteID)
	if err != nil {
		return err
	}

	if quote.Status() != valueobject.QuoteStatusPaid {
		return sharederrors.NewPrecondition("quote_paid",
			fmt.Sprintf("solo se pueden devolver Quotes Paid, estado actual: '%s'", quote.Status()))
	}

	// Intentar devolución automática via MP si aplica.
	h.tryMPRefund(ctx, quote)

	if err := quote.Refund(cmd.Reason); err != nil {
		return err
	}

	if err := h.repo.Update(ctx, quote); err != nil {
		return fmt.Errorf("Refund: update: %w", err)
	}

	publishEvents(ctx, h.eventBus, quote.PendingEvents(), h.logger)

	h.logger.InfoContext(ctx, "devolución registrada",
		"quote_id", cmd.QuoteID,
		"reason", cmd.Reason,
		"refunded_by", cmd.RefundedBy,
	)
	return nil
}

// tryMPRefund busca el primer Payment MP confirmado y ejecuta la devolución.
// Si falla, solo loguea — el registro manual en el sistema continúa igual.
func (h *RefundHandler) tryMPRefund(ctx context.Context, quote *aggregate.Quote) {
	for _, p := range quote.Payments() {
		if p.PaymentMethod() == valueobject.PaymentMethodMercadoPago &&
			p.Status() == valueobject.PaymentStatusConfirmed &&
			p.ExternalReference() != nil {

			if err := h.mpAdapter.RefundPayment(ctx, *p.ExternalReference(), p.AmountCents()); err != nil {
				h.logger.ErrorContext(ctx, "devolución MP falló, el admin debe gestionarla manualmente en MP",
					"quote_id", quote.ID(),
					"mp_payment_id", *p.ExternalReference(),
					"amount_cents", p.AmountCents(),
					"error", err,
				)
			} else {
				h.logger.InfoContext(ctx, "devolución MP exitosa",
					"quote_id", quote.ID(),
					"mp_payment_id", *p.ExternalReference(),
					"amount_cents", p.AmountCents(),
				)
			}
			return // solo procesamos el primer pago MP confirmado
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────

// findPendingMPPayment busca el UUID del primer Payment MP en estado Pending.
func findPendingMPPayment(quote *aggregate.Quote) uuid.UUID {
	for _, p := range quote.Payments() {
		if p.PaymentMethod() == valueobject.PaymentMethodMercadoPago &&
			p.Status() == valueobject.PaymentStatusPending {
			return p.ID()
		}
	}
	return uuid.Nil
}
