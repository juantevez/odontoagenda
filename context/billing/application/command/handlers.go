// Package command contiene los Command Handlers del bounded context Billing.
package command

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/billing/domain/aggregate"
	billingevent "github.com/juantevez/odontoagenda/context/billing/domain/event"
	"github.com/juantevez/odontoagenda/context/billing/domain/repository"
	billingservice "github.com/juantevez/odontoagenda/context/billing/domain/service"
	"github.com/juantevez/odontoagenda/context/billing/domain/valueobject"
	coverageclient "github.com/juantevez/odontoagenda/context/billing/infrastructure/coverage"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── CreateQuote ───────────────────────────────────────────────────

// CreateQuoteCommand se construye desde el evento appointment.booked.
type CreateQuoteCommand struct {
	AppointmentID  uuid.UUID
	PatientID      sharedtypes.PatientID
	ClinicID       sharedtypes.ClinicID
	ProfessionalID sharedtypes.ProfessionalID
	ProcedureCode  string
	ProcedureDesc  string
	ArancelCents   int64
	SlotStart      time.Time
	SlotEnd        time.Time
	// Cobertura del paciente (del payload del evento)
	CoverageType string
	AgreementID  string
	PlanID       string
	PatientAge   int
}

type CreateQuoteHandler struct {
	repo           repository.QuoteRepository
	coverageClient *coverageclient.CoverageClient
	calculator     *billingservice.BillingCalculator
	policyService  *billingservice.CancellationPolicyService
	eventBus       events.Bus
	logger         *slog.Logger
}

func NewCreateQuoteHandler(
	repo repository.QuoteRepository,
	coverageClient *coverageclient.CoverageClient,
	calculator *billingservice.BillingCalculator,
	policyService *billingservice.CancellationPolicyService,
	eventBus events.Bus,
) *CreateQuoteHandler {
	return &CreateQuoteHandler{
		repo:           repo,
		coverageClient: coverageClient,
		calculator:     calculator,
		policyService:  policyService,
		eventBus:       eventBus,
		logger:         slog.Default().With("handler", "CreateQuote"),
	}
}

func (h *CreateQuoteHandler) Handle(ctx context.Context, cmd CreateQuoteCommand) error {
	// Idempotencia: si ya existe un Quote para este appointment, ignorar.
	exists, err := h.repo.ExistsByAppointmentID(ctx, cmd.AppointmentID)
	if err != nil {
		return fmt.Errorf("CreateQuote: check exists: %w", err)
	}
	if exists {
		h.logger.InfoContext(ctx, "Quote ya existe para este appointment, ignorando",
			"appointment_id", cmd.AppointmentID)
		return nil
	}

	// Consultar Coverage BC para obtener el CoverageResult.
	coverageInput, err := h.coverageClient.CalculateCoverage(ctx, coverageclient.CalculateCoverageRequest{
		AgreementID:     cmd.AgreementID,
		PlanID:          cmd.PlanID,
		ProcedureCode:   cmd.ProcedureCode,
		PatientID:       cmd.PatientID,
		PatientAge:      cmd.PatientAge,
		AppointmentDate: cmd.SlotStart,
		VisitsThisYear:  0, // MVP: Billing no trackea esto todavía
	})

	// Si Coverage falló, pendingCoverageCheck = true.
	pendingCoverageCheck := err != nil
	if err != nil {
		h.logger.WarnContext(ctx, "Coverage BC no disponible, usando fallback privado",
			"appointment_id", cmd.AppointmentID, "error", err)
	}

	// Calcular montos.
	amounts := h.calculator.Calculate(cmd.ArancelCents, coverageInput)

	// Parsear CoPayType.
	coPayType, parseErr := valueobject.ParseCoPayType(coverageInput.CoPayType)
	if parseErr != nil {
		coPayType = valueobject.CoPayTypePercent
	}

	// Resolver IDs opcionales de cobertura.
	var agreementUUID, planUUID *uuid.UUID
	if cmd.AgreementID != "" {
		if id, err := uuid.Parse(cmd.AgreementID); err == nil {
			agreementUUID = &id
		}
	}
	if cmd.PlanID != "" {
		if id, err := uuid.Parse(cmd.PlanID); err == nil {
			planUUID = &id
		}
	}

	// Obtener política de cancelación de la sede.
	policy := h.policyService.GetForClinic(cmd.ClinicID)

	// Crear el Quote.
	quote, err := aggregate.NewQuote(
		cmd.AppointmentID,
		cmd.PatientID,
		cmd.ClinicID,
		cmd.ProfessionalID,
		cmd.ProcedureCode,
		cmd.ProcedureDesc,
		cmd.ArancelCents,
		coverageInput.CoveragePercent,
		amounts.CoverageAmountCents,
		coPayType,
		amounts.CoPayAmountCents,
		cmd.CoverageType,
		agreementUUID,
		planUUID,
		coverageInput.RequiresAuthorization,
		policy,
		pendingCoverageCheck,
		cmd.SlotStart,
		cmd.SlotEnd,
	)
	if err != nil {
		return fmt.Errorf("CreateQuote: new quote: %w", err)
	}

	if err := h.repo.Save(ctx, quote); err != nil {
		return fmt.Errorf("CreateQuote: save: %w", err)
	}

	publishEvents(ctx, h.eventBus, quote.PendingEvents(), h.logger)

	h.logger.InfoContext(ctx, "Quote creado",
		"quote_id", quote.ID(),
		"appointment_id", cmd.AppointmentID,
		"arancel_cents", cmd.ArancelCents,
		"co_pay_cents", amounts.CoPayAmountCents,
		"coverage_cents", amounts.CoverageAmountCents,
		"pending_coverage_check", pendingCoverageCheck,
	)
	return nil
}

// ── ConfirmQuote ──────────────────────────────────────────────────

type ConfirmQuoteCommand struct {
	AppointmentID uuid.UUID
}

type ConfirmQuoteHandler struct {
	repo     repository.QuoteRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewConfirmQuoteHandler(repo repository.QuoteRepository, eventBus events.Bus) *ConfirmQuoteHandler {
	return &ConfirmQuoteHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "ConfirmQuote")}
}

func (h *ConfirmQuoteHandler) Handle(ctx context.Context, cmd ConfirmQuoteCommand) error {
	quote, err := h.repo.FindByAppointmentID(ctx, cmd.AppointmentID)
	if err != nil {
		if sharederrors.IsNotFound(err) {
			h.logger.WarnContext(ctx, "no hay Quote para confirmar",
				"appointment_id", cmd.AppointmentID)
			return nil // idempotente
		}
		return err
	}

	if err := quote.Confirm(); err != nil {
		return err
	}

	if err := h.repo.Update(ctx, quote); err != nil {
		return fmt.Errorf("ConfirmQuote: update: %w", err)
	}

	h.logger.InfoContext(ctx, "Quote confirmado",
		"quote_id", quote.ID(),
		"appointment_id", cmd.AppointmentID,
	)
	return nil
}

// ── VoidQuote ─────────────────────────────────────────────────────

type VoidQuoteCommand struct {
	AppointmentID uuid.UUID
	Reason        string
}

type VoidQuoteHandler struct {
	repo     repository.QuoteRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewVoidQuoteHandler(repo repository.QuoteRepository, eventBus events.Bus) *VoidQuoteHandler {
	return &VoidQuoteHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "VoidQuote")}
}

func (h *VoidQuoteHandler) Handle(ctx context.Context, cmd VoidQuoteCommand) error {
	quote, err := h.repo.FindByAppointmentID(ctx, cmd.AppointmentID)
	if err != nil {
		if sharederrors.IsNotFound(err) {
			return nil // no hay Quote, nada que anular
		}
		return err
	}

	if quote.Status().IsTerminal() {
		return nil // ya está anulado o devuelto
	}

	if err := quote.Void(cmd.Reason); err != nil {
		return err
	}

	if err := h.repo.Update(ctx, quote); err != nil {
		return fmt.Errorf("VoidQuote: update: %w", err)
	}

	publishEvents(ctx, h.eventBus, quote.PendingEvents(), h.logger)
	return nil
}

// ── ApplyLateFee ──────────────────────────────────────────────────

type ApplyLateFeeCommand struct {
	AppointmentID      uuid.UUID
	FeeType            string
	IsLateCancellation bool
}

type ApplyLateFeeHandler struct {
	repo     repository.QuoteRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewApplyLateFeeHandler(repo repository.QuoteRepository, eventBus events.Bus) *ApplyLateFeeHandler {
	return &ApplyLateFeeHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "ApplyLateFee")}
}

func (h *ApplyLateFeeHandler) Handle(ctx context.Context, cmd ApplyLateFeeCommand) error {
	quote, err := h.repo.FindByAppointmentID(ctx, cmd.AppointmentID)
	if err != nil {
		if sharederrors.IsNotFound(err) {
			return nil // no hay Quote, nada que cobrar
		}
		return err
	}

	// Si el Quote está en Draft, la cita nunca se confirmó: anular sin cargo.
	if quote.Status() == valueobject.QuoteStatusDraft {
		return h.voidWithoutFee(ctx, quote)
	}

	feeType := valueobject.LateFeeTypeLateCancellation
	if cmd.FeeType == string(valueobject.LateFeeTypeNoShow) {
		feeType = valueobject.LateFeeTypeNoShow
	}

	fee, err := quote.ApplyLateFee(feeType)
	if err != nil {
		return err
	}
	if fee == nil {
		// Política con 0% de cargo: simplemente void.
		return h.voidWithoutFee(ctx, quote)
	}

	if err := h.repo.Update(ctx, quote); err != nil {
		return fmt.Errorf("ApplyLateFee: update: %w", err)
	}

	publishEvents(ctx, h.eventBus, quote.PendingEvents(), h.logger)

	h.logger.InfoContext(ctx, "cargo aplicado",
		"quote_id", quote.ID(),
		"fee_type", cmd.FeeType,
		"amount_cents", fee.AmountCents(),
	)
	return nil
}

func (h *ApplyLateFeeHandler) voidWithoutFee(ctx context.Context, quote *aggregate.Quote) error {
	if err := quote.Void("cancelación sin cargo"); err != nil {
		return err
	}
	if err := h.repo.Update(ctx, quote); err != nil {
		return fmt.Errorf("ApplyLateFee: void: %w", err)
	}
	publishEvents(ctx, h.eventBus, quote.PendingEvents(), h.logger)
	return nil
}

// ── RegisterPayment ───────────────────────────────────────────────

type RegisterPaymentCommand struct {
	QuoteID       uuid.UUID
	AmountCents   int64
	PaymentMethod string
	Notes         string
}

type RegisterPaymentResult struct {
	PaymentID uuid.UUID
	QuoteStatus string
}

type RegisterPaymentHandler struct {
	repo     repository.QuoteRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewRegisterPaymentHandler(repo repository.QuoteRepository, eventBus events.Bus) *RegisterPaymentHandler {
	return &RegisterPaymentHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "RegisterPayment")}
}

func (h *RegisterPaymentHandler) Handle(ctx context.Context, cmd RegisterPaymentCommand) (RegisterPaymentResult, error) {
	method, err := valueobject.ParsePaymentMethod(cmd.PaymentMethod)
	if err != nil {
		return RegisterPaymentResult{}, sharederrors.NewInvalidArgument("payment_method", err.Error())
	}

	quote, err := h.repo.FindByID(ctx, cmd.QuoteID)
	if err != nil {
		return RegisterPaymentResult{}, err
	}

	payment, err := quote.AddPayment(cmd.AmountCents, method, nil, cmd.Notes)
	if err != nil {
		return RegisterPaymentResult{}, err
	}

	if err := h.repo.Update(ctx, quote); err != nil {
		return RegisterPaymentResult{}, fmt.Errorf("RegisterPayment: update: %w", err)
	}

	publishEvents(ctx, h.eventBus, quote.PendingEvents(), h.logger)

	h.logger.InfoContext(ctx, "pago registrado",
		"quote_id", cmd.QuoteID,
		"payment_id", payment.ID(),
		"amount_cents", cmd.AmountCents,
		"method", cmd.PaymentMethod,
		"quote_status", quote.Status(),
	)

	return RegisterPaymentResult{
		PaymentID:   payment.ID(),
		QuoteStatus: quote.Status().String(),
	}, nil
}

// ── WaiveLateFee ──────────────────────────────────────────────────

type WaiveLateFeeCommand struct {
	QuoteID   uuid.UUID
	FeeID     uuid.UUID
	WaivedBy  uuid.UUID
	Reason    string
}

type WaiveLateFeeHandler struct {
	repo     repository.QuoteRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewWaiveLateFeeHandler(repo repository.QuoteRepository, eventBus events.Bus) *WaiveLateFeeHandler {
	return &WaiveLateFeeHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "WaiveLateFee")}
}

func (h *WaiveLateFeeHandler) Handle(ctx context.Context, cmd WaiveLateFeeCommand) error {
	quote, err := h.repo.FindByID(ctx, cmd.QuoteID)
	if err != nil {
		return err
	}

	if err := quote.WaiveLateFee(cmd.FeeID, cmd.WaivedBy, cmd.Reason); err != nil {
		return err
	}

	if err := h.repo.Update(ctx, quote); err != nil {
		return fmt.Errorf("WaiveLateFee: update: %w", err)
	}

	publishEvents(ctx, h.eventBus, quote.PendingEvents(), h.logger)
	return nil
}

// ── SetAuthorizationCode ──────────────────────────────────────────

type SetAuthorizationCodeCommand struct {
	AppointmentID     uuid.UUID
	AuthorizationCode string
}

type SetAuthorizationCodeHandler struct {
	repo   repository.QuoteRepository
	logger *slog.Logger
}

func NewSetAuthorizationCodeHandler(repo repository.QuoteRepository) *SetAuthorizationCodeHandler {
	return &SetAuthorizationCodeHandler{repo: repo,
		logger: slog.Default().With("handler", "SetAuthorizationCode")}
}

func (h *SetAuthorizationCodeHandler) Handle(ctx context.Context, cmd SetAuthorizationCodeCommand) error {
	quote, err := h.repo.FindByAppointmentID(ctx, cmd.AppointmentID)
	if err != nil {
		if sharederrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	quote.SetAuthorizationCode(cmd.AuthorizationCode)

	if err := h.repo.Update(ctx, quote); err != nil {
		return fmt.Errorf("SetAuthorizationCode: update: %w", err)
	}
	return nil
}

// ── helper ────────────────────────────────────────────────────────

func publishEvents(ctx context.Context, bus events.Bus, evts []billingevent.DomainEvent, logger *slog.Logger) {
	for _, evt := range evts {
		if err := bus.Publish(ctx, evt); err != nil {
			logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}
