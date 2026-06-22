// Package command contiene los Command Handlers del bounded context Coverage.
package command

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/event"
	"github.com/juantevez/odontoagenda/context/coverage/domain/repository"
	"github.com/juantevez/odontoagenda/context/coverage/domain/service"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── CreateAgreement ───────────────────────────────────────────────

type CreateAgreementCommand struct {
	AgreementCode          string
	ProviderName           string
	ProviderType           string
	ValidFrom              time.Time
	ValidUntil             *time.Time
	ContactEmail           string
	ContactPhone           string
	CancellationNoticeDays int
	// Primer plan (obligatorio para convenios no-Privado)
	FirstPlanCode            string
	FirstPlanName            string
	FirstPlanCoPayType       string
	FirstPlanCoPayValue      int
	FirstPlanRequiresPreAuth bool
	FirstPlanMaxAnnualVisits *int
	CreatedBy                *uuid.UUID
}

type CreateAgreementHandler struct {
	repo     repository.AgreementRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewCreateAgreementHandler(repo repository.AgreementRepository, eventBus events.Bus) *CreateAgreementHandler {
	return &CreateAgreementHandler{
		repo:     repo,
		eventBus: eventBus,
		logger:   slog.Default().With("handler", "CreateAgreement"),
	}
}

func (h *CreateAgreementHandler) Handle(ctx context.Context, cmd CreateAgreementCommand) (uuid.UUID, error) {
	providerType, err := valueobject.ParseProviderType(cmd.ProviderType)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("provider_type", err.Error())
	}

	email, err := sharedvo.NewEmail(cmd.ContactEmail)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("contact_email", err.Error())
	}
	phone, err := sharedvo.NewPhoneNumber(cmd.ContactPhone)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("contact_phone", err.Error())
	}

	exists, err := h.repo.ExistsByCode(ctx, cmd.AgreementCode)
	if err != nil {
		return uuid.Nil, sharederrors.NewInternal(err)
	}
	if exists {
		return uuid.Nil, sharederrors.NewAlreadyExists("Agreement", "agreement_code", cmd.AgreementCode)
	}

	var firstPlan *aggregate.Plan
	if !providerType.IsPrivado() {
		coPayType, err := valueobject.ParseCoPayType(cmd.FirstPlanCoPayType)
		if err != nil {
			return uuid.Nil, sharederrors.NewInvalidArgument("first_plan_co_pay_type", err.Error())
		}
		plan, err := aggregate.NewPlan(
			cmd.FirstPlanCode, cmd.FirstPlanName,
			coPayType, cmd.FirstPlanCoPayValue,
			cmd.FirstPlanRequiresPreAuth,
			cmd.FirstPlanMaxAnnualVisits,
		)
		if err != nil {
			return uuid.Nil, err
		}
		firstPlan = plan
	}

	agreement, err := aggregate.NewAgreement(
		cmd.AgreementCode, cmd.ProviderName,
		providerType, cmd.ValidFrom, cmd.ValidUntil,
		email, phone,
		cmd.CancellationNoticeDays,
		firstPlan, cmd.CreatedBy,
	)
	if err != nil {
		return uuid.Nil, err
	}

	if err := h.repo.Save(ctx, agreement); err != nil {
		return uuid.Nil, fmt.Errorf("CreateAgreement: save: %w", err)
	}

	publishEvents(ctx, h.eventBus, agreement.PendingEvents(), h.logger)

	h.logger.InfoContext(ctx, "convenio creado",
		"agreement_id", agreement.ID(),
		"code", cmd.AgreementCode,
	)
	return agreement.ID(), nil
}

// ── AddPlan ───────────────────────────────────────────────────────

type AddPlanCommand struct {
	AgreementID     uuid.UUID
	PlanCode        string
	PlanName        string
	CoPayType       string
	CoPayValue      int
	RequiresPreAuth bool
	MaxAnnualVisits *int
}

type AddPlanHandler struct {
	repo     repository.AgreementRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewAddPlanHandler(repo repository.AgreementRepository, eventBus events.Bus) *AddPlanHandler {
	return &AddPlanHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "AddPlan")}
}

func (h *AddPlanHandler) Handle(ctx context.Context, cmd AddPlanCommand) (uuid.UUID, error) {
	agreement, err := h.repo.FindByID(ctx, cmd.AgreementID)
	if err != nil {
		return uuid.Nil, err
	}

	coPayType, err := valueobject.ParseCoPayType(cmd.CoPayType)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("co_pay_type", err.Error())
	}

	plan, err := aggregate.NewPlan(
		cmd.PlanCode, cmd.PlanName,
		coPayType, cmd.CoPayValue,
		cmd.RequiresPreAuth, cmd.MaxAnnualVisits,
	)
	if err != nil {
		return uuid.Nil, err
	}

	if err := agreement.AddPlan(*plan); err != nil {
		return uuid.Nil, err
	}

	if err := h.repo.Update(ctx, agreement); err != nil {
		return uuid.Nil, fmt.Errorf("AddPlan: update: %w", err)
	}

	publishEvents(ctx, h.eventBus, agreement.PendingEvents(), h.logger)
	return plan.ID(), nil
}

// ── UpsertProcedureRule ───────────────────────────────────────────

type UpsertProcedureRuleCommand struct {
	AgreementID           uuid.UUID
	PlanID                uuid.UUID
	ProcedureCode         string
	CoveragePercent       int
	CoPayOverride         *valueobject.CoPayOverride
	RequiresAuthorization bool
	MaxPerYear            *int
	MaxAmountCents        *int64
	WaitingPeriodDays     int
	AgeMin                *int
	AgeMax                *int
}

type UpsertProcedureRuleHandler struct {
	repo     repository.AgreementRepository
	cache    repository.CoverageCache
	eventBus events.Bus
	logger   *slog.Logger
}

func NewUpsertProcedureRuleHandler(
	repo repository.AgreementRepository,
	cache repository.CoverageCache,
	eventBus events.Bus,
) *UpsertProcedureRuleHandler {
	return &UpsertProcedureRuleHandler{repo: repo, cache: cache, eventBus: eventBus,
		logger: slog.Default().With("handler", "UpsertProcedureRule")}
}

func (h *UpsertProcedureRuleHandler) Handle(ctx context.Context, cmd UpsertProcedureRuleCommand) error {
	agreement, err := h.repo.FindByID(ctx, cmd.AgreementID)
	if err != nil {
		return err
	}

	rule := aggregate.ProcedureRule{
		ProcedureCode:         cmd.ProcedureCode,
		CoveragePercent:       cmd.CoveragePercent,
		CoPayOverride:         cmd.CoPayOverride,
		RequiresAuthorization: cmd.RequiresAuthorization,
		MaxPerYear:            cmd.MaxPerYear,
		MaxAmountCents:        cmd.MaxAmountCents,
		WaitingPeriodDays:     cmd.WaitingPeriodDays,
		AgeMin:                cmd.AgeMin,
		AgeMax:                cmd.AgeMax,
	}

	if err := agreement.UpsertProcedureRule(cmd.PlanID, rule); err != nil {
		return err
	}

	if err := h.repo.Update(ctx, agreement); err != nil {
		return fmt.Errorf("UpsertProcedureRule: update: %w", err)
	}

	// Invalidar cache del plan al modificar sus reglas.
	if err := h.cache.InvalidatePlan(ctx, cmd.PlanID); err != nil {
		h.logger.WarnContext(ctx, "error invalidando cache del plan",
			"plan_id", cmd.PlanID, "error", err)
	}

	publishEvents(ctx, h.eventBus, agreement.PendingEvents(), h.logger)
	return nil
}

// ── UpdateAgreementStatus ─────────────────────────────────────────

type UpdateAgreementStatusCommand struct {
	AgreementID uuid.UUID
	NewStatus   string
	Reason      string
	UpdatedBy   uuid.UUID
}

type UpdateAgreementStatusHandler struct {
	repo     repository.AgreementRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewUpdateAgreementStatusHandler(repo repository.AgreementRepository, eventBus events.Bus) *UpdateAgreementStatusHandler {
	return &UpdateAgreementStatusHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "UpdateAgreementStatus")}
}

func (h *UpdateAgreementStatusHandler) Handle(ctx context.Context, cmd UpdateAgreementStatusCommand) error {
	agreement, err := h.repo.FindByID(ctx, cmd.AgreementID)
	if err != nil {
		return err
	}

	newStatus, err := valueobject.ParseAgreementStatus(cmd.NewStatus)
	if err != nil {
		return sharederrors.NewInvalidArgument("status", err.Error())
	}

	switch newStatus {
	case valueobject.AgreementStatusSuspended:
		if err := agreement.Suspend(cmd.Reason, cmd.UpdatedBy); err != nil {
			return err
		}
	case valueobject.AgreementStatusActive:
		if err := agreement.Activate(cmd.UpdatedBy); err != nil {
			return err
		}
	default:
		return sharederrors.NewInvalidArgument("status",
			"solo se puede transicionar a Active o Suspended manualmente")
	}

	if err := h.repo.Update(ctx, agreement); err != nil {
		return fmt.Errorf("UpdateAgreementStatus: update: %w", err)
	}

	publishEvents(ctx, h.eventBus, agreement.PendingEvents(), h.logger)
	return nil
}

// ── RequestAuthorization ──────────────────────────────────────────

type RequestAuthorizationCommand struct {
	AgreementID      uuid.UUID
	PlanID           uuid.UUID
	PatientID        sharedtypes.PatientID
	MembershipNumber string
	ProcedureCode    string
	AppointmentID    *uuid.UUID
}

type RequestAuthorizationHandler struct {
	authSvc  *service.AuthorizationService
	eventBus events.Bus
	logger   *slog.Logger
}

func NewRequestAuthorizationHandler(
	authSvc *service.AuthorizationService,
	eventBus events.Bus,
) *RequestAuthorizationHandler {
	return &RequestAuthorizationHandler{authSvc: authSvc, eventBus: eventBus,
		logger: slog.Default().With("handler", "RequestAuthorization")}
}

func (h *RequestAuthorizationHandler) Handle(ctx context.Context, cmd RequestAuthorizationCommand) (uuid.UUID, error) {
	ar, err := h.authSvc.RequestAuthorization(
		ctx,
		cmd.AgreementID, cmd.PlanID,
		cmd.PatientID, cmd.MembershipNumber,
		cmd.ProcedureCode, cmd.AppointmentID,
	)
	if err != nil {
		return uuid.Nil, err
	}

	publishEvents(ctx, h.eventBus, ar.PendingEvents(), h.logger)
	return ar.ID(), nil
}

// ── ResolveAuthorization ──────────────────────────────────────────

type ResolveAuthorizationCommand struct {
	AuthorizationID   uuid.UUID
	Status            string
	AuthorizationCode string
	RejectionReason   string
	ResolvedBy        uuid.UUID
}

type ResolveAuthorizationHandler struct {
	authSvc  *service.AuthorizationService
	eventBus events.Bus
	logger   *slog.Logger
}

func NewResolveAuthorizationHandler(
	authSvc *service.AuthorizationService,
	eventBus events.Bus,
) *ResolveAuthorizationHandler {
	return &ResolveAuthorizationHandler{authSvc: authSvc, eventBus: eventBus,
		logger: slog.Default().With("handler", "ResolveAuthorization")}
}

func (h *ResolveAuthorizationHandler) Handle(ctx context.Context, cmd ResolveAuthorizationCommand) error {
	status, err := valueobject.ParseAuthorizationStatus(cmd.Status)
	if err != nil {
		return sharederrors.NewInvalidArgument("status", err.Error())
	}

	ar, err := h.authSvc.ResolveAuthorization(
		ctx,
		cmd.AuthorizationID, status,
		cmd.AuthorizationCode, cmd.RejectionReason,
		cmd.ResolvedBy,
	)
	if err != nil {
		return err
	}

	publishEvents(ctx, h.eventBus, ar.PendingEvents(), h.logger)
	return nil
}

// ── helper ────────────────────────────────────────────────────────

func publishEvents(ctx context.Context, bus events.Bus, evts []event.DomainEvent, logger *slog.Logger) {
	for _, evt := range evts {
		if err := bus.Publish(ctx, evt); err != nil {
			logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}
