// Package command contiene los Command Handlers del bounded context Scheduling.
package command

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/repository"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/saga"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── BookAppointment ───────────────────────────────────────────────

type BookAppointmentCommand struct {
	PatientID             sharedtypes.PatientID
	BookedByID            sharedtypes.PatientID // puede diferir del PatientID (reserva familiar)
	ProfessionalID        sharedtypes.ProfessionalID
	ClinicID              sharedtypes.ClinicID
	ProcedureCode         string
	SlotStart             time.Time
	SlotEnd               time.Time
	CoverageType          string
	AgreementID           *uuid.UUID
	RequiresAuthorization bool
	CreatedBy             uuid.UUID
}

type BookAppointmentResult struct {
	AppointmentID sharedtypes.AppointmentID
	Status        string
}

type BookAppointmentHandler struct {
	bookSaga  *saga.BookAppointmentSaga
	apptRepo  repository.AppointmentRepository
	logger    *slog.Logger
}

func NewBookAppointmentHandler(
	bookSaga *saga.BookAppointmentSaga,
	apptRepo repository.AppointmentRepository,
) *BookAppointmentHandler {
	return &BookAppointmentHandler{
		bookSaga: bookSaga,
		apptRepo: apptRepo,
		logger:   slog.Default().With("handler", "BookAppointment"),
	}
}

func (h *BookAppointmentHandler) Handle(ctx context.Context, cmd BookAppointmentCommand) (BookAppointmentResult, error) {
	// Consultar citas activas del paciente para validación de límite.
	activeCount, err := h.apptRepo.CountActiveByPatient(ctx, cmd.PatientID)
	if err != nil {
		return BookAppointmentResult{}, sharederrors.NewInternal(err)
	}

	result, err := h.bookSaga.Execute(ctx, saga.BookAppointmentInput{
		PatientID:               cmd.PatientID,
		BookedByID:              cmd.BookedByID,
		ProfessionalID:          cmd.ProfessionalID,
		ClinicID:                cmd.ClinicID,
		ProcedureCode:           cmd.ProcedureCode,
		SlotStart:               cmd.SlotStart,
		SlotEnd:                 cmd.SlotEnd,
		CoverageType:            cmd.CoverageType,
		AgreementID:             cmd.AgreementID,
		RequiresAuthorization:   cmd.RequiresAuthorization,
		ActiveAppointmentsCount: activeCount,
		Constraints:             valueobject.DefaultBookingConstraints(),
		CreatedBy:               cmd.CreatedBy,
	})
	if err != nil {
		return BookAppointmentResult{}, err
	}

	return BookAppointmentResult{
		AppointmentID: result.AppointmentID,
		Status:        result.Status.String(),
	}, nil
}

// ── CancelAppointment ─────────────────────────────────────────────

type CancelAppointmentCommand struct {
	AppointmentID  sharedtypes.AppointmentID
	Reason         string
	Note           string
	CancelledBy    uuid.UUID
}

type CancelAppointmentHandler struct {
	apptRepo     repository.AppointmentRepository
	scheduleRepo repository.AvailabilityScheduleRepository
	cache        repository.AvailabilityCache
	eventBus     events.Bus
	logger       *slog.Logger
}

func NewCancelAppointmentHandler(
	apptRepo repository.AppointmentRepository,
	scheduleRepo repository.AvailabilityScheduleRepository,
	cache repository.AvailabilityCache,
	eventBus events.Bus,
) *CancelAppointmentHandler {
	return &CancelAppointmentHandler{
		apptRepo:     apptRepo,
		scheduleRepo: scheduleRepo,
		cache:        cache,
		eventBus:     eventBus,
		logger:       slog.Default().With("handler", "CancelAppointment"),
	}
}

func (h *CancelAppointmentHandler) Handle(ctx context.Context, cmd CancelAppointmentCommand) error {
	appt, err := h.apptRepo.FindByID(ctx, cmd.AppointmentID)
	if err != nil {
		return err
	}

	reason := valueobject.CancellationReason(cmd.Reason)
	if err := appt.Cancel(reason, cmd.Note, cmd.CancelledBy, 24); err != nil {
		return err
	}

	if err := h.apptRepo.Update(ctx, appt); err != nil {
		return fmt.Errorf("CancelAppointment: update: %w", err)
	}

	// Liberar el slot en el schedule (proyección).
	schedule, err := h.scheduleRepo.FindByProfessionalAndClinic(
		ctx, appt.ProfessionalID(), appt.ClinicID(),
	)
	if err == nil {
		schedule.ReleaseBookedSlot(appt.ID())
		if updateErr := h.scheduleRepo.Update(ctx, schedule); updateErr != nil {
			h.logger.WarnContext(ctx, "error actualizando schedule tras cancelación", "error", updateErr)
		}
		// Invalidar cache: el slot vuelve a estar disponible.
		_ = h.cache.InvalidateSchedule(ctx, appt.ProfessionalID(), appt.ClinicID())
	}

	// Publicar eventos.
	for _, evt := range appt.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento", "event_type", evt.EventType(), "error", err)
		}
	}

	return nil
}

// ── CompleteAppointment ───────────────────────────────────────────

type CompleteAppointmentCommand struct {
	AppointmentID sharedtypes.AppointmentID
	ClinicalNotes string
	CompletedBy   uuid.UUID
}

type CompleteAppointmentHandler struct {
	apptRepo repository.AppointmentRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewCompleteAppointmentHandler(
	apptRepo repository.AppointmentRepository,
	eventBus events.Bus,
) *CompleteAppointmentHandler {
	return &CompleteAppointmentHandler{
		apptRepo: apptRepo,
		eventBus: eventBus,
		logger:   slog.Default().With("handler", "CompleteAppointment"),
	}
}

func (h *CompleteAppointmentHandler) Handle(ctx context.Context, cmd CompleteAppointmentCommand) error {
	appt, err := h.apptRepo.FindByID(ctx, cmd.AppointmentID)
	if err != nil {
		return err
	}

	if err := appt.Complete(cmd.ClinicalNotes, cmd.CompletedBy); err != nil {
		return err
	}

	if err := h.apptRepo.Update(ctx, appt); err != nil {
		return fmt.Errorf("CompleteAppointment: update: %w", err)
	}

	for _, evt := range appt.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento", "event_type", evt.EventType(), "error", err)
		}
	}

	h.logger.InfoContext(ctx, "cita completada",
		"appointment_id", cmd.AppointmentID,
		"completed_by", cmd.CompletedBy,
	)
	return nil
}

// ── CheckInAppointment ────────────────────────────────────────────

type CheckInAppointmentCommand struct {
	AppointmentID sharedtypes.AppointmentID
	CheckedInBy   uuid.UUID
}

type CheckInAppointmentHandler struct {
	apptRepo repository.AppointmentRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewCheckInAppointmentHandler(apptRepo repository.AppointmentRepository, eventBus events.Bus) *CheckInAppointmentHandler {
	return &CheckInAppointmentHandler{
		apptRepo: apptRepo,
		eventBus: eventBus,
		logger:   slog.Default().With("handler", "CheckInAppointment"),
	}
}

func (h *CheckInAppointmentHandler) Handle(ctx context.Context, cmd CheckInAppointmentCommand) error {
	appt, err := h.apptRepo.FindByID(ctx, cmd.AppointmentID)
	if err != nil {
		return err
	}

	if err := appt.CheckIn(); err != nil {
		return err
	}

	if err := h.apptRepo.Update(ctx, appt); err != nil {
		return fmt.Errorf("CheckInAppointment: update: %w", err)
	}

	for _, evt := range appt.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento", "event_type", evt.EventType(), "error", err)
		}
	}

	h.logger.InfoContext(ctx, "check-in registrado", "appointment_id", cmd.AppointmentID)
	return nil
}

// ── MarkNoShow ────────────────────────────────────────────────────

type MarkNoShowCommand struct {
	AppointmentID sharedtypes.AppointmentID
	MarkedBy      uuid.UUID
}

type MarkNoShowHandler struct {
	apptRepo     repository.AppointmentRepository
	scheduleRepo repository.AvailabilityScheduleRepository
	cache        repository.AvailabilityCache
	eventBus     events.Bus
	logger       *slog.Logger
}

func NewMarkNoShowHandler(
	apptRepo repository.AppointmentRepository,
	scheduleRepo repository.AvailabilityScheduleRepository,
	cache repository.AvailabilityCache,
	eventBus events.Bus,
) *MarkNoShowHandler {
	return &MarkNoShowHandler{
		apptRepo:     apptRepo,
		scheduleRepo: scheduleRepo,
		cache:        cache,
		eventBus:     eventBus,
		logger:       slog.Default().With("handler", "MarkNoShow"),
	}
}

func (h *MarkNoShowHandler) Handle(ctx context.Context, cmd MarkNoShowCommand) error {
	appt, err := h.apptRepo.FindByID(ctx, cmd.AppointmentID)
	if err != nil {
		return err
	}

	if err := appt.MarkNoShow(cmd.MarkedBy); err != nil {
		return err
	}

	if err := h.apptRepo.Update(ctx, appt); err != nil {
		return fmt.Errorf("MarkNoShow: update: %w", err)
	}

	// Liberar slot en el schedule.
	if schedule, err := h.scheduleRepo.FindByProfessionalAndClinic(
		ctx, appt.ProfessionalID(), appt.ClinicID(),
	); err == nil {
		schedule.ReleaseBookedSlot(appt.ID())
		_ = h.scheduleRepo.Update(ctx, schedule)
		_ = h.cache.InvalidateSchedule(ctx, appt.ProfessionalID(), appt.ClinicID())
	}

	for _, evt := range appt.PendingEvents() {
		_ = h.eventBus.Publish(ctx, evt)
	}

	return nil
}

// ── BlockSlot ────────────────────────────────────────────────────

type BlockSlotCommand struct {
	ProfessionalID sharedtypes.ProfessionalID
	ClinicID       sharedtypes.ClinicID
	SlotStart      time.Time
	SlotEnd        time.Time
	Reason         string
	Note           string
	BlockedBy      uuid.UUID
}

type BlockSlotHandler struct {
	scheduleRepo repository.AvailabilityScheduleRepository
	cache        repository.AvailabilityCache
	eventBus     events.Bus
	logger       *slog.Logger
}

func NewBlockSlotHandler(
	scheduleRepo repository.AvailabilityScheduleRepository,
	cache repository.AvailabilityCache,
	eventBus events.Bus,
) *BlockSlotHandler {
	return &BlockSlotHandler{
		scheduleRepo: scheduleRepo,
		cache:        cache,
		eventBus:     eventBus,
		logger:       slog.Default().With("handler", "BlockSlot"),
	}
}

func (h *BlockSlotHandler) Handle(ctx context.Context, cmd BlockSlotCommand) error {
	schedule, err := h.scheduleRepo.FindByProfessionalAndClinic(ctx, cmd.ProfessionalID, cmd.ClinicID)
	if err != nil {
		return err
	}

	slot, err := valueobject.NewTimeSlot(cmd.SlotStart, cmd.SlotEnd)
	if err != nil {
		return sharederrors.NewInvalidArgument("slot", err.Error())
	}

	reason := valueobject.BlockedSlotReason(cmd.Reason)
	if !reason.IsValid() {
		return sharederrors.NewInvalidArgument("reason", fmt.Sprintf("motivo inválido: '%s'", cmd.Reason))
	}

	if err := schedule.AddBlockedSlot(slot, reason, cmd.Note); err != nil {
		return err
	}

	if err := h.scheduleRepo.Update(ctx, schedule); err != nil {
		return fmt.Errorf("BlockSlot: update: %w", err)
	}

	_ = h.cache.InvalidateSchedule(ctx, cmd.ProfessionalID, cmd.ClinicID)

	h.logger.InfoContext(ctx, "slot bloqueado",
		"professional_id", cmd.ProfessionalID,
		"clinic_id", cmd.ClinicID,
		"slot_start", cmd.SlotStart,
	)
	return nil
}
