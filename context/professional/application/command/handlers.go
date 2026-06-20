// Package command contiene los Command Handlers del bounded context Professional.
package command

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/repository"
	"github.com/juantevez/odontoagenda/context/professional/domain/service"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── RegisterProfessional ──────────────────────────────────────────

type RegisterProfessionalCommand struct {
	UserID    *uuid.UUID
	FullName  string
	DocType   string
	DocNumber string
	Email     string
	Phone     string
	Bio       string
	CreatedBy *uuid.UUID
}

type RegisterProfessionalHandler struct {
	repo     repository.ProfessionalRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewRegisterProfessionalHandler(
	repo repository.ProfessionalRepository,
	eventBus events.Bus,
) *RegisterProfessionalHandler {
	return &RegisterProfessionalHandler{
		repo:     repo,
		eventBus: eventBus,
		logger:   slog.Default().With("handler", "RegisterProfessional"),
	}
}

func (h *RegisterProfessionalHandler) Handle(ctx context.Context, cmd RegisterProfessionalCommand) (sharedtypes.ProfessionalID, error) {
	fullName, err := sharedvo.NewFullName(cmd.FullName)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("full_name", err.Error())
	}
	nationalID, err := sharedvo.NewNationalID(sharedvo.DocumentType(cmd.DocType), cmd.DocNumber)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("national_id", err.Error())
	}
	email, err := sharedvo.NewEmail(cmd.Email)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("email", err.Error())
	}
	phone, err := sharedvo.NewPhoneNumber(cmd.Phone)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("phone", err.Error())
	}

	exists, err := h.repo.ExistsByNationalID(ctx, cmd.DocNumber)
	if err != nil {
		return uuid.Nil, sharederrors.NewInternal(err)
	}
	if exists {
		return uuid.Nil, sharederrors.NewAlreadyExists("Professional", "national_id", cmd.DocNumber)
	}

	prof := aggregate.NewProfessional(cmd.UserID, fullName, nationalID, email, phone, cmd.Bio, cmd.CreatedBy)

	if err := h.repo.Save(ctx, prof); err != nil {
		return uuid.Nil, fmt.Errorf("RegisterProfessional: save: %w", err)
	}

	h.publishEvents(ctx, prof)

	h.logger.InfoContext(ctx, "profesional registrado",
		"professional_id", prof.ID(),
		"full_name", cmd.FullName,
	)
	return prof.ID(), nil
}

// ── AddLicense ────────────────────────────────────────────────────

type AddLicenseCommand struct {
	ProfessionalID sharedtypes.ProfessionalID
	SpecialtyCode  string
	SpecialtyName  string
	LicenseNumber  string
	IssuingBody    string
	IssuedAt       time.Time
	ExpiresAt      *time.Time
	DocumentRef    string
	AddedBy        uuid.UUID
}

type AddLicenseHandler struct {
	repo     repository.ProfessionalRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewAddLicenseHandler(repo repository.ProfessionalRepository, eventBus events.Bus) *AddLicenseHandler {
	return &AddLicenseHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "AddLicense")}
}

func (h *AddLicenseHandler) Handle(ctx context.Context, cmd AddLicenseCommand) (uuid.UUID, error) {
	prof, err := h.repo.FindByID(ctx, cmd.ProfessionalID)
	if err != nil {
		return uuid.Nil, err
	}

	specialty, err := valueobject.NewSpecialty(
		valueobject.SpecialtyCode(cmd.SpecialtyCode), cmd.SpecialtyName)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("specialty", err.Error())
	}

	if err := prof.AddLicense(
		specialty, cmd.LicenseNumber, cmd.IssuingBody,
		cmd.IssuedAt, cmd.ExpiresAt, cmd.DocumentRef,
	); err != nil {
		return uuid.Nil, err
	}

	if err := h.repo.Update(ctx, prof); err != nil {
		return uuid.Nil, fmt.Errorf("AddLicense: update: %w", err)
	}

	h.publishEvents(ctx, prof)

	licenses := prof.Licenses()
	return licenses[len(licenses)-1].ID(), nil
}

// ── AssignToClinic ────────────────────────────────────────────────

type AssignToClinicCommand struct {
	ProfessionalID sharedtypes.ProfessionalID
	ClinicID       sharedtypes.ClinicID
	Specialties    []string // SpecialtyCode strings
	WeeklySchedule []DayScheduleInput
	AssignedFrom   time.Time
	AssignedBy     uuid.UUID
}

type DayScheduleInput struct {
	Weekday   int // 0=Dom, 1=Lun, ..., 6=Sáb
	StartHour int
	StartMin  int
	EndHour   int
	EndMin    int
}

type AssignToClinicHandler struct {
	repo            repository.ProfessionalRepository
	conflictChecker *service.ScheduleConflictChecker
	eventBus        events.Bus
	logger          *slog.Logger
}

func NewAssignToClinicHandler(
	repo repository.ProfessionalRepository,
	conflictChecker *service.ScheduleConflictChecker,
	eventBus events.Bus,
) *AssignToClinicHandler {
	return &AssignToClinicHandler{
		repo:            repo,
		conflictChecker: conflictChecker,
		eventBus:        eventBus,
		logger:          slog.Default().With("handler", "AssignToClinic"),
	}
}

func (h *AssignToClinicHandler) Handle(ctx context.Context, cmd AssignToClinicCommand) (uuid.UUID, error) {
	prof, err := h.repo.FindByID(ctx, cmd.ProfessionalID)
	if err != nil {
		return uuid.Nil, err
	}

	// Construir el horario semanal.
	weeklySchedule := make([]valueobject.DaySchedule, 0, len(cmd.WeeklySchedule))
	for _, d := range cmd.WeeklySchedule {
		ds, err := valueobject.NewDaySchedule(
			valueobject.Weekday(d.Weekday),
			d.StartHour, d.StartMin, d.EndHour, d.EndMin,
		)
		if err != nil {
			return uuid.Nil, sharederrors.NewInvalidArgument("weekly_schedule", err.Error())
		}
		weeklySchedule = append(weeklySchedule, ds)
	}

	// Construir especialidades.
	specialties := make([]valueobject.SpecialtyCode, 0, len(cmd.Specialties))
	for _, s := range cmd.Specialties {
		code := valueobject.SpecialtyCode(s)
		if !code.IsValid() {
			return uuid.Nil, sharederrors.NewInvalidArgument("specialties",
				fmt.Sprintf("código de especialidad inválido: '%s'", s))
		}
		specialties = append(specialties, code)
	}

	// Validar conflictos de horario ANTES de modificar el aggregate.
	conflicts, err := h.conflictChecker.CheckNewAssignment(prof, cmd.ClinicID, weeklySchedule)
	if err != nil {
		return uuid.Nil, sharederrors.NewInternal(err)
	}
	if len(conflicts) > 0 {
		desc := conflicts[0].Description
		if len(conflicts) > 1 {
			desc = fmt.Sprintf("%s (y %d conflicto(s) más)", desc, len(conflicts)-1)
		}
		return uuid.Nil, sharederrors.NewConflict(
			fmt.Sprintf("conflicto de horario: %s", desc), nil)
	}

	// Aplicar en el aggregate.
	if err := prof.AssignToClinic(cmd.ClinicID, specialties, weeklySchedule,
		cmd.AssignedFrom, cmd.AssignedBy); err != nil {
		return uuid.Nil, err
	}

	if err := h.repo.Update(ctx, prof); err != nil {
		return uuid.Nil, fmt.Errorf("AssignToClinic: update: %w", err)
	}

	h.publishEvents(ctx, prof)

	// Retornar ID de la nueva asignación (última en la lista).
	assignments := prof.ClinicAssignments()
	return assignments[len(assignments)-1].ID(), nil
}

// ── UpdateClinicSchedule ──────────────────────────────────────────

type UpdateClinicScheduleCommand struct {
	ProfessionalID sharedtypes.ProfessionalID
	AssignmentID   uuid.UUID
	NewSchedule    []DayScheduleInput
	UpdatedBy      uuid.UUID
}

type UpdateClinicScheduleHandler struct {
	repo            repository.ProfessionalRepository
	conflictChecker *service.ScheduleConflictChecker
	eventBus        events.Bus
	logger          *slog.Logger
}

func NewUpdateClinicScheduleHandler(
	repo repository.ProfessionalRepository,
	conflictChecker *service.ScheduleConflictChecker,
	eventBus events.Bus,
) *UpdateClinicScheduleHandler {
	return &UpdateClinicScheduleHandler{
		repo: repo, conflictChecker: conflictChecker, eventBus: eventBus,
		logger: slog.Default().With("handler", "UpdateClinicSchedule"),
	}
}

func (h *UpdateClinicScheduleHandler) Handle(ctx context.Context, cmd UpdateClinicScheduleCommand) error {
	prof, err := h.repo.FindByID(ctx, cmd.ProfessionalID)
	if err != nil {
		return err
	}

	newSchedule := make([]valueobject.DaySchedule, 0, len(cmd.NewSchedule))
	for _, d := range cmd.NewSchedule {
		ds, err := valueobject.NewDaySchedule(
			valueobject.Weekday(d.Weekday),
			d.StartHour, d.StartMin, d.EndHour, d.EndMin,
		)
		if err != nil {
			return sharederrors.NewInvalidArgument("schedule", err.Error())
		}
		newSchedule = append(newSchedule, ds)
	}

	// Verificar conflictos con las otras sedes.
	conflicts, err := h.conflictChecker.CheckScheduleUpdate(prof, cmd.AssignmentID, newSchedule)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return sharederrors.NewConflict(
			fmt.Sprintf("conflicto de horario: %s", conflicts[0].Description), nil)
	}

	if err := prof.UpdateClinicSchedule(cmd.AssignmentID, newSchedule); err != nil {
		return err
	}

	if err := h.repo.Update(ctx, prof); err != nil {
		return fmt.Errorf("UpdateClinicSchedule: update: %w", err)
	}

	h.publishEvents(ctx, prof)
	return nil
}

// ── AddException ──────────────────────────────────────────────────

type AddExceptionCommand struct {
	ProfessionalID sharedtypes.ProfessionalID
	AssignmentID   uuid.UUID
	Date           time.Time
	Reason         string
	IsWorking      bool
	// Si IsWorking=true: horario especial para ese día.
	SpecialStartHour, SpecialStartMin int
	SpecialEndHour, SpecialEndMin     int
	AddedBy                           uuid.UUID
}

type AddExceptionHandler struct {
	repo     repository.ProfessionalRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewAddExceptionHandler(repo repository.ProfessionalRepository, eventBus events.Bus) *AddExceptionHandler {
	return &AddExceptionHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "AddException")}
}

func (h *AddExceptionHandler) Handle(ctx context.Context, cmd AddExceptionCommand) error {
	prof, err := h.repo.FindByID(ctx, cmd.ProfessionalID)
	if err != nil {
		return err
	}

	var specialSchedule *valueobject.DaySchedule
	if cmd.IsWorking {
		ds, err := valueobject.NewDaySchedule(
			valueobject.Weekday(cmd.Date.Weekday()),
			cmd.SpecialStartHour, cmd.SpecialStartMin,
			cmd.SpecialEndHour, cmd.SpecialEndMin,
		)
		if err != nil {
			return sharederrors.NewInvalidArgument("special_schedule", err.Error())
		}
		specialSchedule = &ds
	}

	exc, err := valueobject.NewExceptionDay(cmd.Date, cmd.Reason, cmd.IsWorking, specialSchedule)
	if err != nil {
		return sharederrors.NewInvalidArgument("exception_day", err.Error())
	}

	if err := prof.AddExceptionToClinic(cmd.AssignmentID, exc); err != nil {
		return err
	}

	if err := h.repo.Update(ctx, prof); err != nil {
		return fmt.Errorf("AddException: update: %w", err)
	}

	h.publishEvents(ctx, prof)
	return nil
}

// ── SetProcedureDuration ──────────────────────────────────────────

type SetProcedureDurationCommand struct {
	ProfessionalID sharedtypes.ProfessionalID
	AssignmentID   *uuid.UUID // nil = setear como default del profesional
	ProcedureCode  string
	Minutes        int
	BufferMinutes  int
	SetBy          uuid.UUID
}

type SetProcedureDurationHandler struct {
	repo     repository.ProfessionalRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewSetProcedureDurationHandler(repo repository.ProfessionalRepository, eventBus events.Bus) *SetProcedureDurationHandler {
	return &SetProcedureDurationHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "SetProcedureDuration")}
}

func (h *SetProcedureDurationHandler) Handle(ctx context.Context, cmd SetProcedureDurationCommand) error {
	prof, err := h.repo.FindByID(ctx, cmd.ProfessionalID)
	if err != nil {
		return err
	}

	duration, err := valueobject.NewProcedureDuration(cmd.ProcedureCode, cmd.Minutes, cmd.BufferMinutes)
	if err != nil {
		return sharederrors.NewInvalidArgument("duration", err.Error())
	}

	if cmd.AssignmentID == nil {
		// Default del profesional (propaga a todas las sedes como base).
		prof.SetDefaultDuration(duration)
	} else {
		// Override específico para una sede.
		if err := prof.SetClinicDuration(*cmd.AssignmentID, duration); err != nil {
			return err
		}
	}

	if err := h.repo.Update(ctx, prof); err != nil {
		return fmt.Errorf("SetProcedureDuration: update: %w", err)
	}

	return nil
}

// ── SuspendProfessional ───────────────────────────────────────────

type SuspendProfessionalCommand struct {
	ProfessionalID sharedtypes.ProfessionalID
	Reason         string
	SuspendedBy    uuid.UUID
}

type SuspendProfessionalHandler struct {
	repo     repository.ProfessionalRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewSuspendProfessionalHandler(repo repository.ProfessionalRepository, eventBus events.Bus) *SuspendProfessionalHandler {
	return &SuspendProfessionalHandler{repo: repo, eventBus: eventBus,
		logger: slog.Default().With("handler", "SuspendProfessional")}
}

func (h *SuspendProfessionalHandler) Handle(ctx context.Context, cmd SuspendProfessionalCommand) error {
	prof, err := h.repo.FindByID(ctx, cmd.ProfessionalID)
	if err != nil {
		return err
	}

	if err := prof.Suspend(cmd.Reason, cmd.SuspendedBy); err != nil {
		return err
	}

	if err := h.repo.Update(ctx, prof); err != nil {
		return fmt.Errorf("SuspendProfessional: update: %w", err)
	}

	h.publishEvents(ctx, prof)

	h.logger.InfoContext(ctx, "profesional suspendido",
		"professional_id", cmd.ProfessionalID,
		"reason", cmd.Reason,
	)
	return nil
}

// ── helper ───────────────────────────────────────────────────────

func (h *RegisterProfessionalHandler) publishEvents(ctx context.Context, p *aggregate.Professional) {
	for _, evt := range p.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}

func (h *AddLicenseHandler) publishEvents(ctx context.Context, p *aggregate.Professional) {
	for _, evt := range p.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}

func (h *AssignToClinicHandler) publishEvents(ctx context.Context, p *aggregate.Professional) {
	for _, evt := range p.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}

func (h *UpdateClinicScheduleHandler) publishEvents(ctx context.Context, p *aggregate.Professional) {
	for _, evt := range p.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}

func (h *AddExceptionHandler) publishEvents(ctx context.Context, p *aggregate.Professional) {
	for _, evt := range p.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}

func (h *SuspendProfessionalHandler) publishEvents(ctx context.Context, p *aggregate.Professional) {
	for _, evt := range p.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}
