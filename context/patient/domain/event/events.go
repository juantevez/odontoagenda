// Package event define los Domain Events del bounded context Patient.
// Cada evento es inmutable y porta suficiente contexto para que los
// consumidores actúen sin necesidad de consultar de vuelta al publicador.
package event

import (
	"time"

	"github.com/google/uuid"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// DomainEvent es la interfaz local del bounded context Patient.
type DomainEvent interface {
	EventType() string
	AggregateID() string
	AggregateType() string
	BoundedContext() string
	SchemaVersion() int
}

const boundedContext = "patient"

// ── PatientRegistered ─────────────────────────────────────────────

// PatientRegistered se publica cuando se crea un nuevo Patient.
// Consumido por:
//   - Scheduling: habilitar que el paciente pueda hacer reservas.
//   - Notifications: enviar mensaje de bienvenida.
//   - IAM: si el registro es por staff (sin cuenta), para crear credenciales.
type PatientRegistered struct {
	PatientID  sharedtypes.PatientID `json:"patient_id"`
	UserID     *uuid.UUID            `json:"user_id,omitempty"`
	FullName   string                `json:"full_name"`
	BirthDate  string                `json:"birth_date"` // ISO 8601: "2001-05-20"
	Phone      string                `json:"phone"`
	OccurredAt time.Time             `json:"occurred_at"`
}

func (e PatientRegistered) EventType() string      { return "patient.registered" }
func (e PatientRegistered) AggregateID() string    { return e.PatientID.String() }
func (e PatientRegistered) AggregateType() string  { return "Patient" }
func (e PatientRegistered) BoundedContext() string { return boundedContext }
func (e PatientRegistered) SchemaVersion() int     { return 1 }

// ── PatientCoverageUpdated ────────────────────────────────────────

// PatientCoverageUpdated se publica cuando la cobertura del paciente cambia.
// Consumido por:
//   - Billing: actualizar reglas de precio y copago para este paciente.
//   - Scheduling: saber si se requiere autorización para nuevas reservas.
type PatientCoverageUpdated struct {
	PatientID    sharedtypes.PatientID `json:"patient_id"`
	CoverageID   uuid.UUID             `json:"coverage_id"`
	CoverageType string                `json:"coverage_type"`
	AgreementID  *uuid.UUID            `json:"agreement_id,omitempty"`
	Action       string                `json:"action"` // "added" | "suspended" | "expired"
	OccurredAt   time.Time             `json:"occurred_at"`
}

func (e PatientCoverageUpdated) EventType() string      { return "patient.coverage.updated" }
func (e PatientCoverageUpdated) AggregateID() string    { return e.PatientID.String() }
func (e PatientCoverageUpdated) AggregateType() string  { return "Patient" }
func (e PatientCoverageUpdated) BoundedContext() string { return boundedContext }
func (e PatientCoverageUpdated) SchemaVersion() int     { return 1 }

// ── MedicalAlertAdded ─────────────────────────────────────────────

// MedicalAlertAdded se publica cuando se agrega una nueva alerta médica.
// Consumido por:
//   - Scheduling: mostrar alerta al momento de crear una nueva reserva.
//   - Professional Management: alertar al profesional antes de la consulta.
//   - Notifications: avisar al staff si la alerta es de severidad Critical.
type MedicalAlertAdded struct {
	PatientID      sharedtypes.PatientID `json:"patient_id"`
	AlertID        uuid.UUID             `json:"alert_id"`
	AlertType      string                `json:"alert_type"`
	Severity       string                `json:"severity"`
	IsSelfReported bool                  `json:"is_self_reported"`
	OccurredAt     time.Time             `json:"occurred_at"`
}

func (e MedicalAlertAdded) EventType() string      { return "patient.medical_alert.added" }
func (e MedicalAlertAdded) AggregateID() string    { return e.PatientID.String() }
func (e MedicalAlertAdded) AggregateType() string  { return "Patient" }
func (e MedicalAlertAdded) BoundedContext() string { return boundedContext }
func (e MedicalAlertAdded) SchemaVersion() int     { return 1 }

// ── PatientPreferencesUpdated ─────────────────────────────────────

// PatientPreferencesUpdated se publica cuando el paciente actualiza sus preferencias.
// Consumido por:
//   - Scheduling: sugerir sede y profesional preferido al buscar disponibilidad.
type PatientPreferencesUpdated struct {
	PatientID  sharedtypes.PatientID `json:"patient_id"`
	ClinicID   *sharedtypes.ClinicID `json:"preferred_clinic_id,omitempty"`
	OccurredAt time.Time             `json:"occurred_at"`
}

func (e PatientPreferencesUpdated) EventType() string      { return "patient.preferences.updated" }
func (e PatientPreferencesUpdated) AggregateID() string    { return e.PatientID.String() }
func (e PatientPreferencesUpdated) AggregateType() string  { return "Patient" }
func (e PatientPreferencesUpdated) BoundedContext() string { return boundedContext }
func (e PatientPreferencesUpdated) SchemaVersion() int     { return 1 }

// ── PatientMerged ─────────────────────────────────────────────────

// PatientMerged se publica cuando se fusionan dos registros duplicados.
// El paciente fuente (source) queda archivado; el destino (target) absorbe los datos.
// Consumido por: Scheduling, Billing (para reasignar citas y pagos al ID correcto).
type PatientMerged struct {
	TargetPatientID sharedtypes.PatientID `json:"target_patient_id"` // el que sobrevive
	SourcePatientID sharedtypes.PatientID `json:"source_patient_id"` // el que se archiva
	MergedBy        uuid.UUID             `json:"merged_by"`
	OccurredAt      time.Time             `json:"occurred_at"`
}

func (e PatientMerged) EventType() string      { return "patient.merged" }
func (e PatientMerged) AggregateID() string    { return e.TargetPatientID.String() }
func (e PatientMerged) AggregateType() string  { return "Patient" }
func (e PatientMerged) BoundedContext() string { return boundedContext }
func (e PatientMerged) SchemaVersion() int     { return 1 }

// ── PatientArchived ───────────────────────────────────────────────

// PatientArchived se publica en la baja lógica de un paciente.
// Consumido por: Scheduling (cancelar reservas futuras), Notifications.
type PatientArchived struct {
	PatientID  sharedtypes.PatientID `json:"patient_id"`
	Reason     string                `json:"reason"`
	ArchivedBy uuid.UUID             `json:"archived_by"`
	OccurredAt time.Time             `json:"occurred_at"`
}

func (e PatientArchived) EventType() string      { return "patient.archived" }
func (e PatientArchived) AggregateID() string    { return e.PatientID.String() }
func (e PatientArchived) AggregateType() string  { return "Patient" }
func (e PatientArchived) BoundedContext() string { return boundedContext }
func (e PatientArchived) SchemaVersion() int     { return 1 }
