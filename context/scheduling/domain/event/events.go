// Package event define los Domain Events del bounded context Scheduling.
package event

import (
	"time"

	"github.com/google/uuid"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// DomainEvent es la interfaz local del bounded context Scheduling.
type DomainEvent interface {
	EventType() string
	AggregateID() string
	AggregateType() string
	BoundedContext() string
	SchemaVersion() int
}

const boundedContext = "scheduling"

// ── AppointmentBooked ─────────────────────────────────────────────

// AppointmentBooked se publica al crear una reserva exitosa.
// Consumido por:
//   - Notifications: enviar confirmación al paciente y al profesional.
//   - Billing: generar presupuesto y calcular copago según cobertura.
//   - Patient: (no consume este evento, recibe AppointmentCompleted más tarde)
type AppointmentBooked struct {
	AppointmentID  sharedtypes.AppointmentID  `json:"appointment_id"`
	PatientID      sharedtypes.PatientID      `json:"patient_id"`
	BookedByID     sharedtypes.PatientID      `json:"booked_by_id"`
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	ClinicID       sharedtypes.ClinicID       `json:"clinic_id"`
	ProcedureCode  string                     `json:"procedure_code"`
	SlotStart      time.Time                  `json:"slot_start"`
	SlotEnd        time.Time                  `json:"slot_end"`
	Status         string                     `json:"status"` // Confirmed | Pending
	CoverageType   string                     `json:"coverage_type"`
	OccurredAt     time.Time                  `json:"occurred_at"`
}

func (e AppointmentBooked) EventType() string      { return "appointment.booked" }
func (e AppointmentBooked) AggregateID() string    { return e.AppointmentID.String() }
func (e AppointmentBooked) AggregateType() string  { return "Appointment" }
func (e AppointmentBooked) BoundedContext() string { return boundedContext }
func (e AppointmentBooked) SchemaVersion() int     { return 1 }

// ── AppointmentCheckedIn ──────────────────────────────────────────

// AppointmentCheckedIn se publica cuando el paciente llega a la clínica y es
// registrado en sala (transición Confirmed → InProgress).
// Consumido por: Notifications (aviso al profesional de que su próximo paciente está en espera).
type AppointmentCheckedIn struct {
	AppointmentID  sharedtypes.AppointmentID  `json:"appointment_id"`
	PatientID      sharedtypes.PatientID      `json:"patient_id"`
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	ClinicID       sharedtypes.ClinicID       `json:"clinic_id"`
	SlotStart      time.Time                  `json:"slot_start"`
	OccurredAt     time.Time                  `json:"occurred_at"`
}

func (e AppointmentCheckedIn) EventType() string      { return "appointment.checked_in" }
func (e AppointmentCheckedIn) AggregateID() string    { return e.AppointmentID.String() }
func (e AppointmentCheckedIn) AggregateType() string  { return "Appointment" }
func (e AppointmentCheckedIn) BoundedContext() string { return boundedContext }
func (e AppointmentCheckedIn) SchemaVersion() int     { return 1 }

// ── AppointmentConfirmed ──────────────────────────────────────────

// AppointmentConfirmed se publica cuando una cita Pending pasa a Confirmed.
// Consumido por: Notifications (recordatorio programado).
type AppointmentConfirmed struct {
	AppointmentID  sharedtypes.AppointmentID  `json:"appointment_id"`
	PatientID      sharedtypes.PatientID      `json:"patient_id"`
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	ClinicID       sharedtypes.ClinicID       `json:"clinic_id"`
	SlotStart      time.Time                  `json:"slot_start"`
	OccurredAt     time.Time                  `json:"occurred_at"`
}

func (e AppointmentConfirmed) EventType() string      { return "appointment.confirmed" }
func (e AppointmentConfirmed) AggregateID() string    { return e.AppointmentID.String() }
func (e AppointmentConfirmed) AggregateType() string  { return "Appointment" }
func (e AppointmentConfirmed) BoundedContext() string { return boundedContext }
func (e AppointmentConfirmed) SchemaVersion() int     { return 1 }

// ── AppointmentCompleted ──────────────────────────────────────────

// AppointmentCompleted se publica al finalizar una cita.
// Consumido por:
//   - Patient: actualizar DentalHistorySummary (LastVisitDate, MainTreatments).
//   - Billing: generar cobro definitivo.
//   - Notifications: enviar resumen post-consulta al paciente.
type AppointmentCompleted struct {
	AppointmentID  sharedtypes.AppointmentID  `json:"appointment_id"`
	PatientID      sharedtypes.PatientID      `json:"patient_id"`
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	ClinicID       sharedtypes.ClinicID       `json:"clinic_id"`
	ProcedureCode  string                     `json:"procedure_code"`
	// ProcedureDescription se enriquece desde el Treatment Catalog al publicar.
	ProcedureDescription string    `json:"procedure_description"`
	SlotStart            time.Time `json:"slot_start"`
	SlotEnd              time.Time `json:"slot_end"`
	CompletedBy          uuid.UUID `json:"completed_by"`
	OccurredAt           time.Time `json:"occurred_at"`
}

func (e AppointmentCompleted) EventType() string      { return "appointment.completed" }
func (e AppointmentCompleted) AggregateID() string    { return e.AppointmentID.String() }
func (e AppointmentCompleted) AggregateType() string  { return "Appointment" }
func (e AppointmentCompleted) BoundedContext() string { return boundedContext }
func (e AppointmentCompleted) SchemaVersion() int     { return 1 }

// ── AppointmentCancelled ──────────────────────────────────────────

// AppointmentCancelled se publica al cancelar una cita.
// Consumido por:
//   - Notifications: informar al paciente y profesional.
//   - Billing: anular presupuesto o aplicar cargo por cancelación tardía.
type AppointmentCancelled struct {
	AppointmentID      sharedtypes.AppointmentID  `json:"appointment_id"`
	PatientID          sharedtypes.PatientID      `json:"patient_id"`
	ProfessionalID     sharedtypes.ProfessionalID `json:"professional_id"`
	ClinicID           sharedtypes.ClinicID       `json:"clinic_id"`
	SlotStart          time.Time                  `json:"slot_start"`
	Reason             string                     `json:"reason"`
	IsLateCancellation bool                       `json:"is_late_cancellation"`
	CancelledBy        uuid.UUID                  `json:"cancelled_by"`
	OccurredAt         time.Time                  `json:"occurred_at"`
}

func (e AppointmentCancelled) EventType() string      { return "appointment.cancelled" }
func (e AppointmentCancelled) AggregateID() string    { return e.AppointmentID.String() }
func (e AppointmentCancelled) AggregateType() string  { return "Appointment" }
func (e AppointmentCancelled) BoundedContext() string { return boundedContext }
func (e AppointmentCancelled) SchemaVersion() int     { return 1 }

// ── AppointmentNoShow ─────────────────────────────────────────────

// AppointmentNoShow se publica cuando el paciente no se presentó.
// Consumido por: Notifications, Billing (cargo por no-show si aplica).
type AppointmentNoShow struct {
	AppointmentID  sharedtypes.AppointmentID  `json:"appointment_id"`
	PatientID      sharedtypes.PatientID      `json:"patient_id"`
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	ClinicID       sharedtypes.ClinicID       `json:"clinic_id"`
	SlotStart      time.Time                  `json:"slot_start"`
	MarkedBy       uuid.UUID                  `json:"marked_by"`
	OccurredAt     time.Time                  `json:"occurred_at"`
}

func (e AppointmentNoShow) EventType() string      { return "appointment.no_show" }
func (e AppointmentNoShow) AggregateID() string    { return e.AppointmentID.String() }
func (e AppointmentNoShow) AggregateType() string  { return "Appointment" }
func (e AppointmentNoShow) BoundedContext() string { return boundedContext }
func (e AppointmentNoShow) SchemaVersion() int     { return 1 }

// ── AvailabilityScheduleUpdated ───────────────────────────────────

// AvailabilityScheduleUpdated se publica cuando el schedule de disponibilidad
// cambia (por cambio de horario del profesional o bloqueo manual).
// Consumido por: sí mismo (para invalidar cache Redis).
type AvailabilityScheduleUpdated struct {
	ScheduleID     uuid.UUID                  `json:"schedule_id"`
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	ClinicID       sharedtypes.ClinicID       `json:"clinic_id"`
	OccurredAt     time.Time                  `json:"occurred_at"`
}

func (e AvailabilityScheduleUpdated) EventType() string      { return "scheduling.availability.updated" }
func (e AvailabilityScheduleUpdated) AggregateID() string    { return e.ScheduleID.String() }
func (e AvailabilityScheduleUpdated) AggregateType() string  { return "AvailabilitySchedule" }
func (e AvailabilityScheduleUpdated) BoundedContext() string { return boundedContext }
func (e AvailabilityScheduleUpdated) SchemaVersion() int     { return 1 }
