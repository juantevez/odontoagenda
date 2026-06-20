// Package query contiene los Query Handlers del bounded context Patient.
// Implementan el lado de lectura (CQRS): no modifican estado, no publican eventos.
package query

import (
	"context"
	"time"

	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	"github.com/juantevez/odontoagenda/context/patient/domain/repository"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── GetPatientByID ────────────────────────────────────────────────

type GetPatientByIDQuery struct {
	PatientID sharedtypes.PatientID
}

type PatientDetailDTO struct {
	ID             string           `json:"id"`
	FullName       string           `json:"full_name"`
	BirthDate      string           `json:"birth_date"`
	AgeYears       int              `json:"age_years"`
	IsMinor        bool             `json:"is_minor"`
	Gender         string           `json:"gender"`
	NationalID     NationalIDDTO    `json:"national_id"`
	ContactInfo    ContactInfoDTO   `json:"contact_info"`
	ActiveCoverage *CoverageDTO     `json:"active_coverage,omitempty"`
	ActiveAlerts   []AlertDTO       `json:"active_alerts"`
	DentalHistory  DentalHistoryDTO `json:"dental_history"`
	Preferences    PreferencesDTO   `json:"preferences"`
	CreatedAt      time.Time        `json:"created_at"`
}

type NationalIDDTO struct {
	Type   string `json:"type"`
	Number string `json:"number"`
}

type ContactInfoDTO struct {
	Email          *string `json:"email,omitempty"`
	Phone          string  `json:"phone"`
	WhatsApp       *string `json:"whatsapp,omitempty"`
	EmergencyName  string  `json:"emergency_name,omitempty"`
	EmergencyPhone *string `json:"emergency_phone,omitempty"`
}

type CoverageDTO struct {
	ID               string  `json:"id"`
	Type             string  `json:"type"`
	ProviderName     string  `json:"provider_name"`
	PlanCode         string  `json:"plan_code"`
	MembershipNumber string  `json:"membership_number"`
	ValidFrom        string  `json:"valid_from"`
	ValidUntil       *string `json:"valid_until,omitempty"`
	CoPayPercent     *int    `json:"co_pay_percent,omitempty"`
	CoPayFixedCents  *int64  `json:"co_pay_fixed_cents,omitempty"`
	RequiresExtAuth  bool    `json:"requires_external_authorization"`
}

type AlertDTO struct {
	ID             string `json:"id"`
	Type           string `json:"alert_type"`
	Severity       string `json:"severity"`
	Description    string `json:"description"`
	IsSelfReported bool   `json:"is_self_reported"`
	CreatedAt      string `json:"created_at"`
}

type DentalHistoryDTO struct {
	LastVisitDate  *string        `json:"last_visit_date,omitempty"`
	RiskLevel      string         `json:"risk_level"`
	VisitCount     int            `json:"visit_count"`
	MainTreatments []TreatmentDTO `json:"main_treatments"`
}

type TreatmentDTO struct {
	ProcedureCode string `json:"procedure_code"`
	Description   string `json:"description"`
	PerformedAt   string `json:"performed_at"`
}

type PreferencesDTO struct {
	PreferredClinicID    *string `json:"preferred_clinic_id,omitempty"`
	PreferredTimeOfDay   string  `json:"preferred_time_of_day"`
	CommunicationChannel string  `json:"communication_channel"`
}

// GetPatientByIDHandler es el query handler para detalle de paciente.
type GetPatientByIDHandler struct {
	repo repository.PatientRepository
}

func NewGetPatientByIDHandler(repo repository.PatientRepository) *GetPatientByIDHandler {
	return &GetPatientByIDHandler{repo: repo}
}

func (h *GetPatientByIDHandler) Handle(ctx context.Context, q GetPatientByIDQuery) (*PatientDetailDTO, error) {
	patient, err := h.repo.FindByID(ctx, q.PatientID)
	if err != nil {
		return nil, err
	}
	return toPatientDetailDTO(patient), nil
}

// ── SearchPatients ────────────────────────────────────────────────

type SearchPatientsQuery struct {
	Query string // nombre, documento o teléfono
	Page  sharedtypes.Page
}

type PatientSummaryDTO struct {
	ID           string  `json:"id"`
	FullName     string  `json:"full_name"`
	BirthDate    string  `json:"birth_date"`
	AgeYears     int     `json:"age_years"`
	DocumentType string  `json:"document_type"`
	DocumentNum  string  `json:"document_number"`
	Phone        string  `json:"phone"`
	CoverageType *string `json:"coverage_type,omitempty"`
	HasAlerts    bool    `json:"has_alerts"`
	RiskLevel    string  `json:"risk_level"`
}

type SearchPatientsHandler struct {
	repo repository.PatientRepository
}

func NewSearchPatientsHandler(repo repository.PatientRepository) *SearchPatientsHandler {
	return &SearchPatientsHandler{repo: repo}
}

func (h *SearchPatientsHandler) Handle(ctx context.Context, q SearchPatientsQuery) (sharedtypes.PagedResult[PatientSummaryDTO], error) {
	result, err := h.repo.Search(ctx, q.Query, q.Page)
	if err != nil {
		return sharedtypes.PagedResult[PatientSummaryDTO]{}, err
	}

	dtos := make([]PatientSummaryDTO, len(result.Items))
	for i, p := range result.Items {
		dtos[i] = toPatientSummaryDTO(p)
	}

	return sharedtypes.NewPagedResult(dtos, result.Total, q.Page), nil
}

// ── GetPatientForBooking ──────────────────────────────────────────
// Vista liviana pensada para el contexto Scheduling al crear una reserva.
// Solo expone lo necesario: alertas críticas, cobertura activa y preferencias.

type GetPatientForBookingQuery struct {
	PatientID sharedtypes.PatientID
}

type PatientBookingDTO struct {
	ID              string       `json:"id"`
	FullName        string       `json:"full_name"`
	IsMinor         bool         `json:"is_minor"`
	Phone           string       `json:"phone"`
	CriticalAlerts  []AlertDTO   `json:"critical_alerts"`
	ActiveCoverage  *CoverageDTO `json:"active_coverage,omitempty"`
	PreferredClinic *string      `json:"preferred_clinic_id,omitempty"`
}

type GetPatientForBookingHandler struct {
	repo repository.PatientRepository
}

func NewGetPatientForBookingHandler(repo repository.PatientRepository) *GetPatientForBookingHandler {
	return &GetPatientForBookingHandler{repo: repo}
}

func (h *GetPatientForBookingHandler) Handle(ctx context.Context, q GetPatientForBookingQuery) (*PatientBookingDTO, error) {
	patient, err := h.repo.FindByID(ctx, q.PatientID)
	if err != nil {
		return nil, err
	}

	dto := &PatientBookingDTO{
		ID:       patient.ID().String(),
		FullName: patient.FullName().String(),
		IsMinor:  patient.IsMinor(),
		Phone:    patient.ContactInfo().Phone.String(),
	}

	// Solo alertas críticas (las que el profesional DEBE ver antes de atender).
	criticalSeverity := valueobject.AlertSeverityCritical
	for _, a := range patient.ActiveAlerts(&criticalSeverity) {
		dto.CriticalAlerts = append(dto.CriticalAlerts, toAlertDTO(a))
	}

	if cov := patient.ActiveCoverage(); cov != nil {
		dto.ActiveCoverage = toCoverageDTO(cov)
	}

	prefs := patient.Preferences()
	if prefs.PreferredClinicID != nil {
		s := prefs.PreferredClinicID.String()
		dto.PreferredClinic = &s
	}

	return dto, nil
}

// ── DTO mappers ───────────────────────────────────────────────────

func toPatientDetailDTO(p *aggregate.Patient) *PatientDetailDTO {
	dto := &PatientDetailDTO{
		ID:        p.ID().String(),
		FullName:  p.FullName().String(),
		BirthDate: p.BirthDate().String(),
		AgeYears:  p.BirthDate().AgeYears(),
		IsMinor:   p.IsMinor(),
		Gender:    p.Gender().String(),
		NationalID: NationalIDDTO{
			Type:   string(p.NationalID().Type),
			Number: p.NationalID().Number,
		},
		ContactInfo:   toContactInfoDTO(p.ContactInfo()),
		DentalHistory: toDentalHistoryDTO(p.DentalHistory()),
		Preferences:   toPreferencesDTO(p.Preferences()),
		CreatedAt:     p.CreatedAt(),
	}

	if cov := p.ActiveCoverage(); cov != nil {
		dto.ActiveCoverage = toCoverageDTO(cov)
	}

	for _, a := range p.ActiveAlerts(nil) {
		dto.ActiveAlerts = append(dto.ActiveAlerts, toAlertDTO(a))
	}

	return dto
}

func toPatientSummaryDTO(p *aggregate.Patient) PatientSummaryDTO {
	dto := PatientSummaryDTO{
		ID:           p.ID().String(),
		FullName:     p.FullName().String(),
		BirthDate:    p.BirthDate().String(),
		AgeYears:     p.BirthDate().AgeYears(),
		DocumentType: string(p.NationalID().Type),
		DocumentNum:  p.NationalID().Number,
		Phone:        p.ContactInfo().Phone.String(),
		RiskLevel:    string(p.DentalHistory().RiskLevel()),
		HasAlerts:    len(p.ActiveAlerts(nil)) > 0,
	}
	if cov := p.ActiveCoverage(); cov != nil {
		t := string(cov.CoverageType())
		dto.CoverageType = &t
	}
	return dto
}

func toContactInfoDTO(c aggregate.ContactInfo) ContactInfoDTO {
	dto := ContactInfoDTO{
		Phone:         c.Phone.String(),
		EmergencyName: c.EmergencyName,
	}
	if c.Email != nil {
		s := c.Email.String()
		dto.Email = &s
	}
	if c.WhatsApp != nil {
		s := c.WhatsApp.String()
		dto.WhatsApp = &s
	}
	if c.EmergencyPhone != nil {
		s := c.EmergencyPhone.String()
		dto.EmergencyPhone = &s
	}
	return dto
}

func toCoverageDTO(c *coverage.PatientCoverage) *CoverageDTO {
	dto := &CoverageDTO{
		ID:               c.ID().String(),
		Type:             string(c.CoverageType()),
		ProviderName:     c.ProviderName(),
		PlanCode:         c.PlanCode(),
		MembershipNumber: c.MembershipNumber(),
		ValidFrom:        c.ValidFrom().Format("2006-01-02"),
		CoPayPercent:     c.CoPayPercent(),
		CoPayFixedCents:  c.CoPayFixed(),
		RequiresExtAuth:  c.CoverageType().RequiresExternalAuthorization(),
	}
	if c.ValidUntil() != nil {
		s := c.ValidUntil().Format("2006-01-02")
		dto.ValidUntil = &s
	}
	return dto
}

func toAlertDTO(a aggregate.MedicalAlert) AlertDTO {
	return AlertDTO{
		ID:             a.ID().String(),
		Type:           string(a.AlertType()),
		Severity:       string(a.Severity()),
		Description:    a.Description(),
		IsSelfReported: a.IsSelfReported(),
		CreatedAt:      a.CreatedAt().Format(time.RFC3339),
	}
}

func toDentalHistoryDTO(h *aggregate.DentalHistorySummary) DentalHistoryDTO {
	dto := DentalHistoryDTO{
		RiskLevel:      string(h.RiskLevel()),
		VisitCount:     h.VisitCount(),
		MainTreatments: []TreatmentDTO{},
	}
	if h.LastVisitDate() != nil {
		s := h.LastVisitDate().Format("2006-01-02")
		dto.LastVisitDate = &s
	}
	for _, t := range h.MainTreatments() {
		dto.MainTreatments = append(dto.MainTreatments, TreatmentDTO{
			ProcedureCode: t.ProcedureCode,
			Description:   t.Description,
			PerformedAt:   t.PerformedAt.Format("2006-01-02"),
		})
	}
	return dto
}

func toPreferencesDTO(p aggregate.PatientPreferences) PreferencesDTO {
	dto := PreferencesDTO{
		PreferredTimeOfDay:   string(p.PreferredTimeOfDay),
		CommunicationChannel: string(p.CommunicationChannel),
	}
	if p.PreferredClinicID != nil {
		s := p.PreferredClinicID.String()
		dto.PreferredClinicID = &s
	}
	return dto
}

func toAlertDTOFromVO(a aggregate.MedicalAlert) AlertDTO {
	return toAlertDTO(a)
}

// Asegurar que sharedvo es usado (evitar error de compilación si no se usa directamente).
var _ = sharedvo.Email{}
