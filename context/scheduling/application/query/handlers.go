// Package query contiene los Query Handlers del bounded context Scheduling.
package query

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/repository"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/service"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── DTOs ─────────────────────────────────────────────────────────

type AppointmentDTO struct {
	ID             string `json:"id"`
	PatientID      string `json:"patient_id"`
	BookedByID     string `json:"booked_by_id"`
	ProfessionalID string `json:"professional_id"`
	ClinicID       string `json:"clinic_id"`
	ProcedureCode  string `json:"procedure_code"`
	SlotStart      string `json:"slot_start"` // RFC3339
	SlotEnd        string `json:"slot_end"`
	DurationMins   int    `json:"duration_mins"`
	Status         string `json:"status"`
	CoverageType   string `json:"coverage_type"`
	ClinicalNotes  string `json:"clinical_notes,omitempty"`
	IsLateCancelled bool  `json:"is_late_cancellation,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type FreeSlotDTO struct {
	ProfessionalID string `json:"professional_id"`
	ClinicID       string `json:"clinic_id"`
	ProcedureCode  string `json:"procedure_code"`
	SlotStart      string `json:"slot_start"` // RFC3339
	SlotEnd        string `json:"slot_end"`
	DurationMins   int    `json:"duration_mins"`
}

type AvailabilityResultDTO struct {
	ProfessionalID string       `json:"professional_id"`
	ClinicID       string       `json:"clinic_id"`
	Date           string       `json:"date"` // YYYY-MM-DD
	FreeSlots      []FreeSlotDTO `json:"free_slots"`
	TotalFree      int          `json:"total_free"`
}

// ── GetAvailability ───────────────────────────────────────────────

// GetAvailabilityQuery busca slots libres para un profesional/sede/procedimiento/fecha.
type GetAvailabilityQuery struct {
	ProfessionalID sharedtypes.ProfessionalID
	ClinicID       sharedtypes.ClinicID
	ProcedureCode  string
	Date           time.Time
}

type GetAvailabilityHandler struct {
	scheduleRepo   repository.AvailabilityScheduleRepository
	cache          repository.AvailabilityCache
	slotCalculator *service.SlotCalculator
}

func NewGetAvailabilityHandler(
	scheduleRepo repository.AvailabilityScheduleRepository,
	cache repository.AvailabilityCache,
	slotCalculator *service.SlotCalculator,
) *GetAvailabilityHandler {
	return &GetAvailabilityHandler{
		scheduleRepo:   scheduleRepo,
		cache:          cache,
		slotCalculator: slotCalculator,
	}
}

func (h *GetAvailabilityHandler) Handle(ctx context.Context, q GetAvailabilityQuery) (*AvailabilityResultDTO, error) {
	// 1. Intentar desde cache (Redis).
	cached, err := h.cache.GetSlots(ctx,
		q.ProfessionalID, q.ClinicID, q.Date, q.ProcedureCode,
	)
	if err == nil && cached != nil {
		return toAvailabilityResultDTO(q.ProfessionalID, q.ClinicID, q.Date, cached), nil
	}

	// 2. Cache miss: calcular desde el AvailabilitySchedule.
	schedule, err := h.scheduleRepo.FindByProfessionalAndClinic(ctx, q.ProfessionalID, q.ClinicID)
	if err != nil {
		return nil, err
	}

	durationMins, ok := schedule.DurationForProcedure(q.ProcedureCode)
	if !ok {
		// Sin duración específica: usar default de 30 minutos.
		durationMins = 30
	}

	slots, err := h.slotCalculator.CalculateForDate(schedule, q.Date, q.ProcedureCode, durationMins)
	if err != nil {
		return nil, err
	}

	// 3. Guardar en cache para próximas consultas.
	_ = h.cache.SetSlots(ctx, q.ProfessionalID, q.ClinicID, q.Date, q.ProcedureCode, slots)

	return toAvailabilityResultDTO(q.ProfessionalID, q.ClinicID, q.Date, slots), nil
}

// ── GetAvailabilityRange ──────────────────────────────────────────

// GetAvailabilityRangeQuery busca los próximos N slots disponibles en un rango de fechas.
type GetAvailabilityRangeQuery struct {
	ProfessionalID sharedtypes.ProfessionalID
	ClinicID       sharedtypes.ClinicID
	ProcedureCode  string
	From           time.Time
	To             time.Time
	MaxResults     int
}

type GetAvailabilityRangeHandler struct {
	scheduleRepo   repository.AvailabilityScheduleRepository
	slotCalculator *service.SlotCalculator
}

func NewGetAvailabilityRangeHandler(
	scheduleRepo repository.AvailabilityScheduleRepository,
	slotCalculator *service.SlotCalculator,
) *GetAvailabilityRangeHandler {
	return &GetAvailabilityRangeHandler{
		scheduleRepo:   scheduleRepo,
		slotCalculator: slotCalculator,
	}
}

func (h *GetAvailabilityRangeHandler) Handle(ctx context.Context, q GetAvailabilityRangeQuery) ([]FreeSlotDTO, error) {
	schedule, err := h.scheduleRepo.FindByProfessionalAndClinic(ctx, q.ProfessionalID, q.ClinicID)
	if err != nil {
		return nil, err
	}

	durationMins, ok := schedule.DurationForProcedure(q.ProcedureCode)
	if !ok {
		durationMins = 30
	}

	slots, err := h.slotCalculator.CalculateForRange(
		schedule, q.From, q.To, q.ProcedureCode, durationMins, q.MaxResults,
	)
	if err != nil {
		return nil, err
	}

	dtos := make([]FreeSlotDTO, len(slots))
	for i, s := range slots {
		dtos[i] = toFreeSlotDTO(s)
	}
	return dtos, nil
}

// ── GetDaySchedule ────────────────────────────────────────────────

// GetDayScheduleQuery retorna las citas del día para un profesional/sede.
// Vista usada por recepcionistas y el propio profesional.
type GetDayScheduleQuery struct {
	ProfessionalID sharedtypes.ProfessionalID
	ClinicID       sharedtypes.ClinicID
	Date           time.Time
}

type DayScheduleDTO struct {
	ProfessionalID string           `json:"professional_id"`
	ClinicID       string           `json:"clinic_id"`
	Date           string           `json:"date"`
	Appointments   []AppointmentDTO `json:"appointments"`
	FreeSlots      []FreeSlotDTO    `json:"free_slots"`
	TotalBooked    int              `json:"total_booked"`
	TotalFree      int              `json:"total_free"`
}

type GetDayScheduleHandler struct {
	apptRepo       repository.AppointmentRepository
	scheduleRepo   repository.AvailabilityScheduleRepository
	slotCalculator *service.SlotCalculator
}

func NewGetDayScheduleHandler(
	apptRepo repository.AppointmentRepository,
	scheduleRepo repository.AvailabilityScheduleRepository,
	slotCalculator *service.SlotCalculator,
) *GetDayScheduleHandler {
	return &GetDayScheduleHandler{
		apptRepo:       apptRepo,
		scheduleRepo:   scheduleRepo,
		slotCalculator: slotCalculator,
	}
}

func (h *GetDayScheduleHandler) Handle(ctx context.Context, q GetDayScheduleQuery) (*DayScheduleDTO, error) {
	dayStart := time.Date(q.Date.Year(), q.Date.Month(), q.Date.Day(), 0, 0, 0, 0, time.UTC)

	// Citas del día: usar vista de sede cuando no se filtra por profesional.
	var appointments []*aggregate.Appointment
	var err error
	if q.ProfessionalID == sharedtypes.ProfessionalID(uuid.Nil) {
		appointments, err = h.apptRepo.FindByClinicAndDate(ctx, q.ClinicID, dayStart)
	} else {
		dayEnd := dayStart.Add(24 * time.Hour)
		appointments, err = h.apptRepo.FindByProfessionalAndDate(
			ctx, q.ProfessionalID, q.ClinicID, dayStart, dayEnd,
		)
	}
	if err != nil {
		return nil, err
	}

	// Slots libres del día (procedimiento default: 30 min para la vista general).
	schedule, err := h.scheduleRepo.FindByProfessionalAndClinic(ctx, q.ProfessionalID, q.ClinicID)
	var freeSlots []aggregate.FreeSlot
	if err == nil {
		freeSlots, _ = h.slotCalculator.CalculateForDate(schedule, q.Date, "", 30)
	}

	dto := &DayScheduleDTO{
		ProfessionalID: q.ProfessionalID.String(),
		ClinicID:       q.ClinicID.String(),
		Date:           q.Date.Format("2006-01-02"),
		Appointments:   make([]AppointmentDTO, len(appointments)),
		FreeSlots:      make([]FreeSlotDTO, len(freeSlots)),
		TotalBooked:    len(appointments),
		TotalFree:      len(freeSlots),
	}
	for i, a := range appointments {
		dto.Appointments[i] = toAppointmentDTO(a)
	}
	for i, s := range freeSlots {
		dto.FreeSlots[i] = toFreeSlotDTO(s)
	}
	return dto, nil
}

// ── GetPatientAppointments ────────────────────────────────────────

type GetPatientAppointmentsQuery struct {
	PatientID    sharedtypes.PatientID
	OnlyActive   bool
}

type GetPatientAppointmentsHandler struct {
	apptRepo repository.AppointmentRepository
}

func NewGetPatientAppointmentsHandler(apptRepo repository.AppointmentRepository) *GetPatientAppointmentsHandler {
	return &GetPatientAppointmentsHandler{apptRepo: apptRepo}
}

func (h *GetPatientAppointmentsHandler) Handle(ctx context.Context, q GetPatientAppointmentsQuery) ([]AppointmentDTO, error) {
	appts, err := h.apptRepo.FindActiveByPatient(ctx, q.PatientID)
	if err != nil {
		return nil, err
	}

	dtos := make([]AppointmentDTO, 0, len(appts))
	for _, a := range appts {
		if q.OnlyActive && !a.Status().IsActive() {
			continue
		}
		dtos = append(dtos, toAppointmentDTO(a))
	}
	return dtos, nil
}

// ── DTO Mappers ───────────────────────────────────────────────────

func toAppointmentDTO(a *aggregate.Appointment) AppointmentDTO {
	return AppointmentDTO{
		ID:             a.ID().String(),
		PatientID:      a.PatientID().String(),
		BookedByID:     a.BookedByID().String(),
		ProfessionalID: a.ProfessionalID().String(),
		ClinicID:       a.ClinicID().String(),
		ProcedureCode:  a.ProcedureCode(),
		SlotStart:      a.Slot().Start.Format(time.RFC3339),
		SlotEnd:        a.Slot().End.Format(time.RFC3339),
		DurationMins:   a.Slot().DurationMinutes(),
		Status:         a.Status().String(),
		CoverageType:   a.CoverageType(),
		ClinicalNotes:  a.ClinicalNotes(),
		IsLateCancelled: a.IsLateCancellation(),
		CreatedAt:      a.CreatedAt().Format(time.RFC3339),
	}
}

func toFreeSlotDTO(s aggregate.FreeSlot) FreeSlotDTO {
	return FreeSlotDTO{
		ProfessionalID: s.ProfessionalID.String(),
		ClinicID:       s.ClinicID.String(),
		ProcedureCode:  s.ProcedureCode,
		SlotStart:      s.Slot.Start.Format(time.RFC3339),
		SlotEnd:        s.Slot.End.Format(time.RFC3339),
		DurationMins:   s.DurationMins,
	}
}

func toAvailabilityResultDTO(
	profID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	date time.Time,
	slots []aggregate.FreeSlot,
) *AvailabilityResultDTO {
	dtos := make([]FreeSlotDTO, len(slots))
	for i, s := range slots {
		dtos[i] = toFreeSlotDTO(s)
	}
	return &AvailabilityResultDTO{
		ProfessionalID: profID.String(),
		ClinicID:       clinicID.String(),
		Date:           date.Format("2006-01-02"),
		FreeSlots:      dtos,
		TotalFree:      len(dtos),
	}
}

// Suppress unused import warnings.
var _ = valueobject.StatusConfirmed
