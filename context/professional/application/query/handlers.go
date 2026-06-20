// Package query contiene los Query Handlers del bounded context Professional.
// Incluye una vista liviana específica para el contexto Scheduling
// que evita cruzar contextos directamente.
package query

import (
	"context"
	"time"

	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/repository"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── DTOs ─────────────────────────────────────────────────────────

type ProfessionalDetailDTO struct {
	ID          string          `json:"id"`
	FullName    string          `json:"full_name"`
	Email       string          `json:"email"`
	Phone       string          `json:"phone"`
	Bio         string          `json:"bio"`
	Status      string          `json:"status"`
	Licenses    []LicenseDTO    `json:"licenses"`
	Assignments []AssignmentDTO `json:"assignments"`
}

type LicenseDTO struct {
	ID            string  `json:"id"`
	SpecialtyCode string  `json:"specialty_code"`
	SpecialtyName string  `json:"specialty_name"`
	LicenseNumber string  `json:"license_number"`
	IssuingBody   string  `json:"issuing_body"`
	IssuedAt      string  `json:"issued_at"`
	ExpiresAt     *string `json:"expires_at,omitempty"`
	Status        string  `json:"status"`
	IsValid       bool    `json:"is_valid"`
}

type AssignmentDTO struct {
	ID                  string            `json:"id"`
	ClinicID            string            `json:"clinic_id"`
	Status              string            `json:"status"`
	AssignedSpecialties []string          `json:"assigned_specialties"`
	WeeklySchedule      []DayScheduleDTO  `json:"weekly_schedule"`
	AssignedFrom        string            `json:"assigned_from"`
	AssignedUntil       *string           `json:"assigned_until,omitempty"`
	ProcedureDurations  []ProcDurationDTO `json:"procedure_durations"`
}

type DayScheduleDTO struct {
	Weekday    string `json:"weekday"`
	WeekdayNum int    `json:"weekday_num"`
	StartHour  int    `json:"start_hour"`
	StartMin   int    `json:"start_min"`
	EndHour    int    `json:"end_hour"`
	EndMin     int    `json:"end_min"`
}

type ProcDurationDTO struct {
	ProcedureCode string `json:"procedure_code"`
	Minutes       int    `json:"minutes"`
	BufferMinutes int    `json:"buffer_minutes"`
	TotalMinutes  int    `json:"total_minutes"`
}

// ProfessionalForSchedulingDTO es la vista liviana diseñada exclusivamente
// para que el contexto Scheduling construya el AvailabilitySchedule.
// Solo expone lo que Scheduling necesita: disponibilidad + duraciones.
type ProfessionalForSchedulingDTO struct {
	ProfessionalID    string                             `json:"professional_id"`
	FullName          string                             `json:"full_name"`
	IsActive          bool                               `json:"is_active"`
	ActiveSpecialties []string                           `json:"active_specialties"`
	ClinicAssignments []ClinicAssignmentForSchedulingDTO `json:"clinic_assignments"`
}

type ClinicAssignmentForSchedulingDTO struct {
	AssignmentID       string            `json:"assignment_id"`
	ClinicID           string            `json:"clinic_id"`
	Specialties        []string          `json:"specialties"`
	WeeklySchedule     []DayScheduleDTO  `json:"weekly_schedule"`
	ExceptionDays      []ExceptionDayDTO `json:"exception_days"`
	ProcedureDurations []ProcDurationDTO `json:"procedure_durations"`
	AssignedFrom       string            `json:"assigned_from"`
	AssignedUntil      *string           `json:"assigned_until,omitempty"`
}

type ExceptionDayDTO struct {
	Date      string `json:"date"` // YYYY-MM-DD
	Reason    string `json:"reason"`
	IsWorking bool   `json:"is_working"`
	StartHour *int   `json:"start_hour,omitempty"`
	StartMin  *int   `json:"start_min,omitempty"`
	EndHour   *int   `json:"end_hour,omitempty"`
	EndMin    *int   `json:"end_min,omitempty"`
}

// ── GetProfessionalByID ───────────────────────────────────────────

type GetProfessionalByIDQuery struct {
	ProfessionalID sharedtypes.ProfessionalID
}

type GetProfessionalByIDHandler struct {
	repo repository.ProfessionalRepository
}

func NewGetProfessionalByIDHandler(repo repository.ProfessionalRepository) *GetProfessionalByIDHandler {
	return &GetProfessionalByIDHandler{repo: repo}
}

func (h *GetProfessionalByIDHandler) Handle(ctx context.Context, q GetProfessionalByIDQuery) (*ProfessionalDetailDTO, error) {
	prof, err := h.repo.FindByID(ctx, q.ProfessionalID)
	if err != nil {
		return nil, err
	}
	return toProfessionalDetailDTO(prof), nil
}

// ── GetProfessionalForScheduling ──────────────────────────────────

// GetProfessionalForSchedulingHandler retorna la vista optimizada para Scheduling.
// Scheduling la consulta al construir o invalidar el AvailabilitySchedule.
type GetProfessionalForSchedulingQuery struct {
	ProfessionalID sharedtypes.ProfessionalID
}

type GetProfessionalForSchedulingHandler struct {
	repo repository.ProfessionalRepository
}

func NewGetProfessionalForSchedulingHandler(repo repository.ProfessionalRepository) *GetProfessionalForSchedulingHandler {
	return &GetProfessionalForSchedulingHandler{repo: repo}
}

func (h *GetProfessionalForSchedulingHandler) Handle(
	ctx context.Context,
	q GetProfessionalForSchedulingQuery,
) (*ProfessionalForSchedulingDTO, error) {
	prof, err := h.repo.FindByID(ctx, q.ProfessionalID)
	if err != nil {
		return nil, err
	}
	return toProfessionalForSchedulingDTO(prof), nil
}

// ── FindByClinic ──────────────────────────────────────────────────

type FindByClinicQuery struct {
	ClinicID  sharedtypes.ClinicID
	Specialty *string // opcional
}

type FindByClinicHandler struct {
	repo repository.ProfessionalRepository
}

func NewFindByClinicHandler(repo repository.ProfessionalRepository) *FindByClinicHandler {
	return &FindByClinicHandler{repo: repo}
}

func (h *FindByClinicHandler) Handle(ctx context.Context, q FindByClinicQuery) ([]*ProfessionalDetailDTO, error) {
	var specialtyFilter *valueobject.SpecialtyCode
	if q.Specialty != nil {
		code := valueobject.SpecialtyCode(*q.Specialty)
		specialtyFilter = &code
	}

	profs, err := h.repo.FindByClinic(ctx, q.ClinicID, specialtyFilter)
	if err != nil {
		return nil, err
	}

	dtos := make([]*ProfessionalDetailDTO, len(profs))
	for i, p := range profs {
		dtos[i] = toProfessionalDetailDTO(p)
	}
	return dtos, nil
}

// ── FindAvailableAt ───────────────────────────────────────────────

// FindAvailableAtQuery es la query de búsqueda de disponibilidad —
// la más frecuente de este contexto, llamada desde Scheduling.
type FindAvailableAtQuery struct {
	ClinicID  sharedtypes.ClinicID
	At        time.Time
	Specialty *string
}

type FindAvailableAtHandler struct {
	repo repository.ProfessionalRepository
}

func NewFindAvailableAtHandler(repo repository.ProfessionalRepository) *FindAvailableAtHandler {
	return &FindAvailableAtHandler{repo: repo}
}

func (h *FindAvailableAtHandler) Handle(
	ctx context.Context,
	q FindAvailableAtQuery,
) ([]*ProfessionalForSchedulingDTO, error) {
	var specialtyFilter *valueobject.SpecialtyCode
	if q.Specialty != nil {
		code := valueobject.SpecialtyCode(*q.Specialty)
		specialtyFilter = &code
	}

	profs, err := h.repo.FindAvailableAt(ctx, q.ClinicID, q.At, specialtyFilter)
	if err != nil {
		return nil, err
	}

	dtos := make([]*ProfessionalForSchedulingDTO, len(profs))
	for i, p := range profs {
		dtos[i] = toProfessionalForSchedulingDTO(p)
	}
	return dtos, nil
}

// ── DTO mappers ───────────────────────────────────────────────────

func toProfessionalDetailDTO(p *aggregate.Professional) *ProfessionalDetailDTO {
	dto := &ProfessionalDetailDTO{
		ID:       p.ID().String(),
		FullName: p.FullName().String(),
		Email:    p.Email().String(),
		Phone:    p.Phone().String(),
		Bio:      p.Bio(),
		Status:   string(p.Status()),
	}

	for _, l := range p.Licenses() {
		ld := LicenseDTO{
			ID:            l.ID().String(),
			SpecialtyCode: string(l.Specialty().Code),
			SpecialtyName: l.Specialty().DisplayName,
			LicenseNumber: l.LicenseNumber(),
			IssuingBody:   l.IssuingBody(),
			IssuedAt:      l.IssuedAt().Format("2006-01-02"),
			Status:        string(l.Status()),
			IsValid:       l.IsCurrentlyValid(),
		}
		if l.ExpiresAt() != nil {
			s := l.ExpiresAt().Format("2006-01-02")
			ld.ExpiresAt = &s
		}
		dto.Licenses = append(dto.Licenses, ld)
	}

	for _, a := range p.ClinicAssignments() {
		dto.Assignments = append(dto.Assignments, toAssignmentDTO(a))
	}

	return dto
}

func toAssignmentDTO(a aggregate.ClinicAssignment) AssignmentDTO {
	dto := AssignmentDTO{
		ID:           a.ID().String(),
		ClinicID:     a.ClinicID().String(),
		Status:       string(a.Status()),
		AssignedFrom: a.AssignedFrom().Format("2006-01-02"),
	}
	if a.AssignedUntil() != nil {
		s := a.AssignedUntil().Format("2006-01-02")
		dto.AssignedUntil = &s
	}
	for _, sc := range a.AssignedSpecialties() {
		dto.AssignedSpecialties = append(dto.AssignedSpecialties, string(sc))
	}
	for _, d := range a.WeeklySchedule() {
		dto.WeeklySchedule = append(dto.WeeklySchedule, toDayScheduleDTO(d))
	}
	for _, d := range a.ProcedureDurations() {
		dto.ProcedureDurations = append(dto.ProcedureDurations, toProcDurationDTO(d))
	}
	return dto
}

func toProfessionalForSchedulingDTO(p *aggregate.Professional) *ProfessionalForSchedulingDTO {
	dto := &ProfessionalForSchedulingDTO{
		ProfessionalID: p.ID().String(),
		FullName:       p.FullName().String(),
		IsActive:       p.Status().IsActive(),
	}
	for _, code := range p.ActiveSpecialties() {
		dto.ActiveSpecialties = append(dto.ActiveSpecialties, string(code))
	}
	for _, a := range p.ClinicAssignments() {
		if !a.Status().IsActive() {
			continue
		}
		caDTO := ClinicAssignmentForSchedulingDTO{
			AssignmentID: a.ID().String(),
			ClinicID:     a.ClinicID().String(),
			AssignedFrom: a.AssignedFrom().Format("2006-01-02"),
		}
		if a.AssignedUntil() != nil {
			s := a.AssignedUntil().Format("2006-01-02")
			caDTO.AssignedUntil = &s
		}
		for _, sc := range a.AssignedSpecialties() {
			caDTO.Specialties = append(caDTO.Specialties, string(sc))
		}
		for _, d := range a.WeeklySchedule() {
			caDTO.WeeklySchedule = append(caDTO.WeeklySchedule, toDayScheduleDTO(d))
		}
		for _, exc := range a.ExceptionDays() {
			caDTO.ExceptionDays = append(caDTO.ExceptionDays, toExceptionDayDTO(exc))
		}
		for _, d := range a.ProcedureDurations() {
			caDTO.ProcedureDurations = append(caDTO.ProcedureDurations, toProcDurationDTO(d))
		}
		dto.ClinicAssignments = append(dto.ClinicAssignments, caDTO)
	}
	return dto
}

func toDayScheduleDTO(d valueobject.DaySchedule) DayScheduleDTO {
	return DayScheduleDTO{
		Weekday:    d.Weekday.String(),
		WeekdayNum: int(d.Weekday),
		StartHour:  d.StartHour,
		StartMin:   d.StartMin,
		EndHour:    d.EndHour,
		EndMin:     d.EndMin,
	}
}

func toProcDurationDTO(d valueobject.ProcedureDuration) ProcDurationDTO {
	return ProcDurationDTO{
		ProcedureCode: d.ProcedureCode,
		Minutes:       d.Minutes,
		BufferMinutes: d.BufferMinutes,
		TotalMinutes:  d.TotalMinutes(),
	}
}

func toExceptionDayDTO(e valueobject.ExceptionDay) ExceptionDayDTO {
	dto := ExceptionDayDTO{
		Date:      e.Date.Format("2006-01-02"),
		Reason:    e.Reason,
		IsWorking: e.IsWorking,
	}
	if e.Schedule != nil {
		dto.StartHour = &e.Schedule.StartHour
		dto.StartMin = &e.Schedule.StartMin
		dto.EndHour = &e.Schedule.EndHour
		dto.EndMin = &e.Schedule.EndMin
	}
	return dto
}
