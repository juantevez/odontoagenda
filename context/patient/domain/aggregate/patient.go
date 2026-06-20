// Package aggregate contiene el Aggregate Root del bounded context Patient.
//
// Aggregate Root: Patient
// Entidades internas:
//   - MedicalAlert    — alertas médicas con lifecycle propio
//   - DentalHistorySummary — resumen del historial clínico (Entity con lifecycle)
//   - PatientPreferences   — preferencias de sede, horario y comunicación
//
// El submódulo Coverage (PatientCoverage) vive en domain/coverage/
// y se compone dentro de Patient como una Entity con reglas propias.
package aggregate

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	"github.com/juantevez/odontoagenda/context/patient/domain/event"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── Patient — Aggregate Root ──────────────────────────────────────

// Patient es el Aggregate Root del bounded context Patient Management.
// Centraliza toda la información del paciente: identidad, cobertura,
// alertas médicas, historial odontológico resumido y preferencias.
//
// Invariantes:
//   - DocumentNumber es único en todo el sistema (enforced a nivel de repositorio).
//   - Un paciente no puede tener dos coberturas activas del mismo CoverageType.
//   - Las MedicalAlerts de severidad Critical siempre son visibles al staff.
//   - El paciente solo puede modificar sus propias preferencias y agregar alertas self-reported.
//   - DentalHistorySummary solo se actualiza por eventos de Scheduling (AppointmentCompleted).
type Patient struct {
	id     sharedtypes.PatientID
	userID *uuid.UUID // vínculo con IAM (nil si creado por staff sin cuenta)

	// Información personal
	fullName     sharedvo.FullName
	birthDate    valueobject.BirthDate
	gender       valueobject.Gender
	nationalID   sharedvo.NationalID
	contactInfo  ContactInfo
	homeLocation *GeoPoint // PostGIS: para búsqueda de sede cercana

	// Entidades internas
	coverages     []*coverage.PatientCoverage
	medicalAlerts []MedicalAlert
	dentalHistory *DentalHistorySummary
	preferences   PatientPreferences

	// Auditoría y control de concurrencia
	createdAt time.Time
	updatedAt time.Time
	createdBy *uuid.UUID
	version   int64

	// Domain Events pendientes de publicar
	pendingEvents []event.DomainEvent
}

// ── ContactInfo — Value Object complejo ───────────────────────────

type ContactInfo struct {
	Email          *sharedvo.Email
	Phone          sharedvo.PhoneNumber
	WhatsApp       *sharedvo.PhoneNumber
	Address        *sharedvo.Address
	EmergencyName  string
	EmergencyPhone *sharedvo.PhoneNumber
}

// ── GeoPoint — Value Object ───────────────────────────────────────

type GeoPoint struct {
	Latitude  float64
	Longitude float64
}

// ── MedicalAlert — Entity ─────────────────────────────────────────

// MedicalAlert representa una alerta médica del paciente.
// Tiene identidad propia para poder ser revocada individualmente.
//
// Invariante: solo staff puede crear alertas con IsSelfReported=false.
// El paciente puede agregar alertas con IsSelfReported=true.
type MedicalAlert struct {
	id             uuid.UUID
	alertType      valueobject.AlertType
	severity       valueobject.AlertSeverity
	description    string
	isSelfReported bool // true si fue cargada por el propio paciente
	isActive       bool
	createdBy      uuid.UUID
	createdAt      time.Time
	revokedAt      *time.Time
	revokedBy      *uuid.UUID
}

func newMedicalAlert(
	alertType valueobject.AlertType,
	severity valueobject.AlertSeverity,
	description string,
	isSelfReported bool,
	createdBy uuid.UUID,
) MedicalAlert {
	return MedicalAlert{
		id:             uuid.New(),
		alertType:      alertType,
		severity:       severity,
		description:    strings.TrimSpace(description),
		isSelfReported: isSelfReported,
		isActive:       true,
		createdBy:      createdBy,
		createdAt:      time.Now().UTC(),
	}
}

func (a MedicalAlert) ID() uuid.UUID                       { return a.id }
func (a MedicalAlert) AlertType() valueobject.AlertType    { return a.alertType }
func (a MedicalAlert) Severity() valueobject.AlertSeverity { return a.severity }
func (a MedicalAlert) Description() string                 { return a.description }
func (a MedicalAlert) IsSelfReported() bool                { return a.isSelfReported }
func (a MedicalAlert) IsActive() bool                      { return a.isActive }
func (a MedicalAlert) CreatedBy() uuid.UUID                { return a.createdBy }
func (a MedicalAlert) CreatedAt() time.Time                { return a.createdAt }
func (a MedicalAlert) RevokedAt() *time.Time               { return a.revokedAt }

// ── DentalHistorySummary — Entity ────────────────────────────────

// DentalHistorySummary es una Entity con su propio ciclo de vida
// dentro del Aggregate Patient. Se actualiza exclusivamente por eventos
// provenientes del contexto Scheduling (AppointmentCompleted).
//
// Decisión: es Entity (no Value Object) porque tiene identidad propia,
// evoluciona con el tiempo, y sus cambios se pueden rastrear individualmente.
type DentalHistorySummary struct {
	id             uuid.UUID
	patientID      sharedtypes.PatientID
	lastVisitDate  *time.Time
	riskLevel      valueobject.RiskLevel
	mainTreatments []TreatmentSummary
	visitCount     int
	updatedAt      time.Time
	updatedByEvent string // nombre del evento que disparó la última actualización
}

// TreatmentSummary es un resumen compacto de un tratamiento realizado.
// No es el historial clínico completo (eso vive en context/clinical).
type TreatmentSummary struct {
	ProcedureCode  string
	Description    string
	PerformedAt    time.Time
	ClinicID       sharedtypes.ClinicID
	ProfessionalID sharedtypes.ProfessionalID
}

func newDentalHistorySummary(patientID sharedtypes.PatientID) *DentalHistorySummary {
	return &DentalHistorySummary{
		id:             uuid.New(),
		patientID:      patientID,
		riskLevel:      valueobject.RiskLevelLow,
		mainTreatments: []TreatmentSummary{},
		visitCount:     0,
		updatedAt:      time.Now().UTC(),
	}
}

// RecordVisit actualiza el historial con la información de una visita completada.
// Llamado desde el handler del evento AppointmentCompleted.
func (d *DentalHistorySummary) RecordVisit(treatment TreatmentSummary, sourcEvent string) {
	now := time.Now().UTC()
	d.lastVisitDate = &now
	d.visitCount++
	d.updatedByEvent = sourcEvent
	d.updatedAt = now

	// Mantener los últimos 20 tratamientos en el resumen.
	d.mainTreatments = append(d.mainTreatments, treatment)
	if len(d.mainTreatments) > 20 {
		d.mainTreatments = d.mainTreatments[len(d.mainTreatments)-20:]
	}
}

// UpdateRiskLevel permite que el staff actualice el nivel de riesgo.
func (d *DentalHistorySummary) UpdateRiskLevel(level valueobject.RiskLevel) {
	d.riskLevel = level
	d.updatedAt = time.Now().UTC()
}

func (d *DentalHistorySummary) ID() uuid.UUID                      { return d.id }
func (d *DentalHistorySummary) LastVisitDate() *time.Time          { return d.lastVisitDate }
func (d *DentalHistorySummary) RiskLevel() valueobject.RiskLevel   { return d.riskLevel }
func (d *DentalHistorySummary) MainTreatments() []TreatmentSummary { return d.mainTreatments }
func (d *DentalHistorySummary) VisitCount() int                    { return d.visitCount }
func (d *DentalHistorySummary) UpdatedAt() time.Time               { return d.updatedAt }

// ── PatientPreferences — Value Object ────────────────────────────

// PatientPreferences encapsula las preferencias de atención del paciente.
// El propio paciente puede modificarlas (no requiere staff).
type PatientPreferences struct {
	PreferredClinicID    *sharedtypes.ClinicID
	PreferredProfIDs     []sharedtypes.ProfessionalID
	PreferredTimeOfDay   valueobject.PreferredTimeOfDay
	CommunicationChannel valueobject.CommunicationChannel
}

// ── Constructor ───────────────────────────────────────────────────

// NewPatient crea un Patient nuevo con los datos mínimos requeridos.
// El DentalHistorySummary se inicializa vacío; se enriquece vía eventos.
func NewPatient(
	userID *uuid.UUID,
	fullName sharedvo.FullName,
	birthDate valueobject.BirthDate,
	gender valueobject.Gender,
	nationalID sharedvo.NationalID,
	phone sharedvo.PhoneNumber,
	createdBy *uuid.UUID,
) (*Patient, error) {
	id := sharedtypes.NewID()
	now := time.Now().UTC()

	p := &Patient{
		id:         id,
		userID:     userID,
		fullName:   fullName,
		birthDate:  birthDate,
		gender:     gender,
		nationalID: nationalID,
		contactInfo: ContactInfo{
			Phone: phone,
		},
		coverages:     []*coverage.PatientCoverage{},
		medicalAlerts: []MedicalAlert{},
		dentalHistory: newDentalHistorySummary(id),
		preferences: PatientPreferences{
			PreferredTimeOfDay:   valueobject.TimeOfDayAny,
			CommunicationChannel: valueobject.ChannelWhatsApp,
		},
		createdAt: now,
		updatedAt: now,
		createdBy: createdBy,
		version:   1,
	}

	p.pendingEvents = append(p.pendingEvents, event.PatientRegistered{
		PatientID:  id,
		UserID:     userID,
		FullName:   fullName.String(),
		BirthDate:  birthDate.String(),
		Phone:      phone.String(),
		OccurredAt: now,
	})

	return p, nil
}

// Reconstitute reconstruye un Patient desde persistencia sin disparar eventos.
func Reconstitute(
	id sharedtypes.PatientID,
	userID *uuid.UUID,
	fullName sharedvo.FullName,
	birthDate valueobject.BirthDate,
	gender valueobject.Gender,
	nationalID sharedvo.NationalID,
	contactInfo ContactInfo,
	homeLocation *GeoPoint,
	coverages []*coverage.PatientCoverage,
	alerts []MedicalAlert,
	history *DentalHistorySummary,
	prefs PatientPreferences,
	createdAt, updatedAt time.Time,
	createdBy *uuid.UUID,
	version int64,
) *Patient {
	return &Patient{
		id:            id,
		userID:        userID,
		fullName:      fullName,
		birthDate:     birthDate,
		gender:        gender,
		nationalID:    nationalID,
		contactInfo:   contactInfo,
		homeLocation:  homeLocation,
		coverages:     coverages,
		medicalAlerts: alerts,
		dentalHistory: history,
		preferences:   prefs,
		createdAt:     createdAt,
		updatedAt:     updatedAt,
		createdBy:     createdBy,
		version:       version,
		pendingEvents: []event.DomainEvent{},
	}
}

// ── Comportamiento: Información Personal ─────────────────────────

// UpdateContactInfo actualiza los datos de contacto del paciente.
// El propio paciente puede hacerlo (no requiere staff).
func (p *Patient) UpdateContactInfo(info ContactInfo) {
	p.contactInfo = info
	p.updatedAt = time.Now().UTC()
}

// SetHomeLocation actualiza las coordenadas del domicilio (para búsqueda de sede cercana).
func (p *Patient) SetHomeLocation(lat, lng float64) error {
	if lat < -90 || lat > 90 {
		return sharederrors.NewInvalidArgument("latitude", "debe estar entre -90 y 90")
	}
	if lng < -180 || lng > 180 {
		return sharederrors.NewInvalidArgument("longitude", "debe estar entre -180 y 180")
	}
	p.homeLocation = &GeoPoint{Latitude: lat, Longitude: lng}
	p.updatedAt = time.Now().UTC()
	return nil
}

// UpdatePreferences actualiza las preferencias del paciente.
// El propio paciente puede invocar este método.
func (p *Patient) UpdatePreferences(prefs PatientPreferences) {
	p.preferences = prefs
	p.updatedAt = time.Now().UTC()

	p.pendingEvents = append(p.pendingEvents, event.PatientPreferencesUpdated{
		PatientID:  p.id,
		ClinicID:   prefs.PreferredClinicID,
		OccurredAt: time.Now().UTC(),
	})
}

// ── Comportamiento: Coverage ──────────────────────────────────────

// AddCoverage agrega una nueva cobertura al paciente.
// Solo staff puede invocar este método (enforced en application layer).
//
// Invariante: no puede haber dos coberturas activas del mismo CoverageType.
func (p *Patient) AddCoverage(c *coverage.PatientCoverage, addedBy uuid.UUID) error {
	// Verificar unicidad por tipo activo.
	for _, existing := range p.coverages {
		if existing.CoverageType() == c.CoverageType() && existing.IsActive() {
			return sharederrors.NewPrecondition("duplicate_active_coverage",
				fmt.Sprintf("ya existe una cobertura activa de tipo '%s'", c.CoverageType()))
		}
	}

	p.coverages = append(p.coverages, c)
	p.updatedAt = time.Now().UTC()

	p.pendingEvents = append(p.pendingEvents, event.PatientCoverageUpdated{
		PatientID:    p.id,
		CoverageID:   c.ID(),
		CoverageType: string(c.CoverageType()),
		AgreementID:  c.AgreementID(),
		Action:       "added",
		OccurredAt:   time.Now().UTC(),
	})

	return nil
}

// SuspendCoverage suspende la cobertura activa de un tipo dado.
func (p *Patient) SuspendCoverage(coverageID uuid.UUID, reason string, by uuid.UUID) error {
	for _, c := range p.coverages {
		if c.ID() == coverageID {
			if err := c.Suspend(reason, by); err != nil {
				return err
			}
			p.updatedAt = time.Now().UTC()

			p.pendingEvents = append(p.pendingEvents, event.PatientCoverageUpdated{
				PatientID:    p.id,
				CoverageID:   c.ID(),
				CoverageType: string(c.CoverageType()),
				AgreementID:  c.AgreementID(),
				Action:       "suspended",
				OccurredAt:   time.Now().UTC(),
			})
			return nil
		}
	}
	return sharederrors.NewNotFound("PatientCoverage", coverageID.String())
}

// ActiveCoverage retorna la cobertura activa de mayor prioridad.
// Privado es el fallback cuando no hay otra cobertura activa.
func (p *Patient) ActiveCoverage() *coverage.PatientCoverage {
	// Prioridad: PrepagaExterna > ObraSocial > Corporativo > PrepagaPropia > Especial > Privado
	priority := map[valueobject.CoverageType]int{
		valueobject.CoverageTypeExtPrepaid: 6,
		valueobject.CoverageTypeObraSocial: 5,
		valueobject.CoverageTypeCorporate:  4,
		valueobject.CoverageTypeOwnPrepaid: 3,
		valueobject.CoverageTypeSpecial:    2,
		valueobject.CoverageTypePrivate:    1,
	}

	var best *coverage.PatientCoverage
	bestPrio := -1
	for _, c := range p.coverages {
		if !c.IsActive() {
			continue
		}
		if prio := priority[c.CoverageType()]; prio > bestPrio {
			bestPrio = prio
			best = c
		}
	}
	return best
}

// ── Comportamiento: Medical Alerts ───────────────────────────────

// AddMedicalAlertByStaff agrega una alerta médica creada por el staff.
func (p *Patient) AddMedicalAlertByStaff(
	alertType valueobject.AlertType,
	severity valueobject.AlertSeverity,
	description string,
	staffID uuid.UUID,
) error {
	if strings.TrimSpace(description) == "" {
		return sharederrors.NewInvalidArgument("description", "la descripción es requerida")
	}

	alert := newMedicalAlert(alertType, severity, description, false, staffID)
	p.medicalAlerts = append(p.medicalAlerts, alert)
	p.updatedAt = time.Now().UTC()

	p.pendingEvents = append(p.pendingEvents, event.MedicalAlertAdded{
		PatientID:      p.id,
		AlertID:        alert.ID(),
		AlertType:      string(alertType),
		Severity:       string(severity),
		IsSelfReported: false,
		OccurredAt:     time.Now().UTC(),
	})

	return nil
}

// AddSelfReportedAlert agrega una alerta reportada por el propio paciente.
// Solo puede crear alertas de tipo Info o Warning (no Critical).
func (p *Patient) AddSelfReportedAlert(
	alertType valueobject.AlertType,
	description string,
	patientUserID uuid.UUID,
) error {
	if strings.TrimSpace(description) == "" {
		return sharederrors.NewInvalidArgument("description", "la descripción es requerida")
	}

	// El paciente no puede crear alertas críticas: eso es exclusivo del staff.
	alert := newMedicalAlert(alertType, valueobject.AlertSeverityWarning, description, true, patientUserID)
	p.medicalAlerts = append(p.medicalAlerts, alert)
	p.updatedAt = time.Now().UTC()

	p.pendingEvents = append(p.pendingEvents, event.MedicalAlertAdded{
		PatientID:      p.id,
		AlertID:        alert.ID(),
		AlertType:      string(alertType),
		Severity:       string(valueobject.AlertSeverityWarning),
		IsSelfReported: true,
		OccurredAt:     time.Now().UTC(),
	})

	return nil
}

// RevokeAlert desactiva una alerta médica. Solo staff puede revocar.
func (p *Patient) RevokeAlert(alertID uuid.UUID, revokedBy uuid.UUID) error {
	now := time.Now().UTC()
	for i := range p.medicalAlerts {
		if p.medicalAlerts[i].id == alertID {
			if !p.medicalAlerts[i].isActive {
				return sharederrors.NewPrecondition("alert_active", "la alerta ya está inactiva")
			}
			p.medicalAlerts[i].isActive = false
			p.medicalAlerts[i].revokedAt = &now
			p.medicalAlerts[i].revokedBy = &revokedBy
			p.updatedAt = now
			return nil
		}
	}
	return sharederrors.NewNotFound("MedicalAlert", alertID.String())
}

// ActiveAlerts retorna solo las alertas activas, opcionalmente filtradas por severidad mínima.
func (p *Patient) ActiveAlerts(minSeverity *valueobject.AlertSeverity) []MedicalAlert {
	severityOrder := map[valueobject.AlertSeverity]int{
		valueobject.AlertSeverityInfo:     1,
		valueobject.AlertSeverityWarning:  2,
		valueobject.AlertSeverityCritical: 3,
	}

	minOrder := 0
	if minSeverity != nil {
		minOrder = severityOrder[*minSeverity]
	}

	result := make([]MedicalAlert, 0)
	for _, a := range p.medicalAlerts {
		if a.isActive && severityOrder[a.severity] >= minOrder {
			result = append(result, a)
		}
	}
	return result
}

// ── Comportamiento: Dental History ───────────────────────────────

// RecordCompletedVisit actualiza el historial clínico resumido.
// Invocado por el subscriber del evento AppointmentCompleted de Scheduling.
func (p *Patient) RecordCompletedVisit(treatment TreatmentSummary, sourceEvent string) {
	p.dentalHistory.RecordVisit(treatment, sourceEvent)
	p.updatedAt = time.Now().UTC()
}

// UpdateRiskLevel permite al staff actualizar el nivel de riesgo del paciente.
func (p *Patient) UpdateRiskLevel(level valueobject.RiskLevel, by uuid.UUID) {
	p.dentalHistory.UpdateRiskLevel(level)
	p.updatedAt = time.Now().UTC()
}

// ── Getters ───────────────────────────────────────────────────────

func (p *Patient) ID() sharedtypes.PatientID              { return p.id }
func (p *Patient) UserID() *uuid.UUID                     { return p.userID }
func (p *Patient) FullName() sharedvo.FullName            { return p.fullName }
func (p *Patient) BirthDate() valueobject.BirthDate       { return p.birthDate }
func (p *Patient) Gender() valueobject.Gender             { return p.gender }
func (p *Patient) NationalID() sharedvo.NationalID        { return p.nationalID }
func (p *Patient) ContactInfo() ContactInfo               { return p.contactInfo }
func (p *Patient) HomeLocation() *GeoPoint                { return p.homeLocation }
func (p *Patient) Coverages() []*coverage.PatientCoverage { return p.coverages }
func (p *Patient) MedicalAlerts() []MedicalAlert          { return p.medicalAlerts }
func (p *Patient) DentalHistory() *DentalHistorySummary   { return p.dentalHistory }
func (p *Patient) Preferences() PatientPreferences        { return p.preferences }
func (p *Patient) CreatedAt() time.Time                   { return p.createdAt }
func (p *Patient) UpdatedAt() time.Time                   { return p.updatedAt }
func (p *Patient) CreatedBy() *uuid.UUID                  { return p.createdBy }
func (p *Patient) Version() int64                         { return p.version }
func (p *Patient) IsMinor() bool                          { return p.birthDate.IsMinor() }

// PendingEvents retorna los eventos acumulados y los limpia.
func (p *Patient) PendingEvents() []event.DomainEvent {
	evts := p.pendingEvents
	p.pendingEvents = nil
	return evts
}
