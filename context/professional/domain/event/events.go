// Package event define los Domain Events del bounded context Professional.
package event

import (
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// DomainEvent es la interfaz local del bounded context Professional.
type DomainEvent interface {
	EventType() string
	AggregateID() string
	AggregateType() string
	BoundedContext() string
	SchemaVersion() int
}

const boundedContext = "professional"

// ── ProfessionalRegistered ────────────────────────────────────────

// ProfessionalRegistered se publica al dar de alta un nuevo profesional.
// Consumido por: IAM (crear cuenta si no tiene), Notifications (bienvenida).
type ProfessionalRegistered struct {
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	FullName       string                     `json:"full_name"`
	OccurredAt     time.Time                  `json:"occurred_at"`
}

func (e ProfessionalRegistered) EventType() string      { return "professional.registered" }
func (e ProfessionalRegistered) AggregateID() string    { return e.ProfessionalID.String() }
func (e ProfessionalRegistered) AggregateType() string  { return "Professional" }
func (e ProfessionalRegistered) BoundedContext() string { return boundedContext }
func (e ProfessionalRegistered) SchemaVersion() int     { return 1 }

// ── ProfessionalLicenseAdded ──────────────────────────────────────

// ProfessionalLicenseAdded se publica al agregar una matrícula.
// Consumido por: Notifications (confirmar habilitación), Audit log.
type ProfessionalLicenseAdded struct {
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	LicenseID      uuid.UUID                  `json:"license_id"`
	SpecialtyCode  string                     `json:"specialty_code"`
	LicenseNumber  string                     `json:"license_number"`
	ExpiresAt      *time.Time                 `json:"expires_at,omitempty"`
	OccurredAt     time.Time                  `json:"occurred_at"`
}

func (e ProfessionalLicenseAdded) EventType() string      { return "professional.license.added" }
func (e ProfessionalLicenseAdded) AggregateID() string    { return e.ProfessionalID.String() }
func (e ProfessionalLicenseAdded) AggregateType() string  { return "Professional" }
func (e ProfessionalLicenseAdded) BoundedContext() string { return boundedContext }
func (e ProfessionalLicenseAdded) SchemaVersion() int     { return 1 }

// ── ProfessionalAssignedToClinic ──────────────────────────────────

// ProfessionalAssignedToClinic se publica al asignar un profesional a una sede.
// Consumido por:
//   - Scheduling: crear o actualizar el AvailabilitySchedule para (Professional, Clinic).
//   - Clinic Management: actualizar la lista de profesionales de la sede.
//   - Notifications: avisar al profesional de la nueva asignación.
type ProfessionalAssignedToClinic struct {
	ProfessionalID sharedtypes.ProfessionalID  `json:"professional_id"`
	AssignmentID   uuid.UUID                   `json:"assignment_id"`
	ClinicID       sharedtypes.ClinicID        `json:"clinic_id"`
	Specialties    []valueobject.SpecialtyCode `json:"specialties"`
	AssignedFrom   time.Time                   `json:"assigned_from"`
	OccurredAt     time.Time                   `json:"occurred_at"`
}

func (e ProfessionalAssignedToClinic) EventType() string      { return "professional.assigned_to_clinic" }
func (e ProfessionalAssignedToClinic) AggregateID() string    { return e.ProfessionalID.String() }
func (e ProfessionalAssignedToClinic) AggregateType() string  { return "Professional" }
func (e ProfessionalAssignedToClinic) BoundedContext() string { return boundedContext }
func (e ProfessionalAssignedToClinic) SchemaVersion() int     { return 1 }

// ── ProfessionalScheduleUpdated ───────────────────────────────────

// ProfessionalScheduleUpdated se publica cuando cambia el horario de una sede:
// nuevo horario recurrente, excepción agregada/removida, o asignación finalizada.
//
// Consumido por:
//   - Scheduling: INVALIDAR cache de disponibilidad para (Professional, Clinic).
//     Este es el evento más crítico para la consistencia del sistema.
//   - Notifications: avisar a pacientes con reservas futuras si hay conflicto.
type ProfessionalScheduleUpdated struct {
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	ClinicID       sharedtypes.ClinicID       `json:"clinic_id"`
	// ChangeType describe qué cambió para que el consumidor decida el alcance de la invalidación.
	ChangeType ScheduleChangeType `json:"change_type"`
	// AffectedDate es la fecha afectada si ChangeType es ExceptionAdded/Removed.
	// Nil si el cambio es estructural (nuevo horario recurrente).
	AffectedDate *time.Time `json:"affected_date,omitempty"`
	OccurredAt   time.Time  `json:"occurred_at"`
}

type ScheduleChangeType string

const (
	ScheduleChangeWeekly           ScheduleChangeType = "weekly_schedule_updated"
	ScheduleChangeExceptionAdded   ScheduleChangeType = "exception_added"
	ScheduleChangeExceptionRemoved ScheduleChangeType = "exception_removed"
	ScheduleChangeAssignmentEnded  ScheduleChangeType = "assignment_ended"
)

func (e ProfessionalScheduleUpdated) EventType() string      { return "professional.schedule.updated" }
func (e ProfessionalScheduleUpdated) AggregateID() string    { return e.ProfessionalID.String() }
func (e ProfessionalScheduleUpdated) AggregateType() string  { return "Professional" }
func (e ProfessionalScheduleUpdated) BoundedContext() string { return boundedContext }
func (e ProfessionalScheduleUpdated) SchemaVersion() int     { return 1 }

// ── ProfessionalSuspended ─────────────────────────────────────────

// ProfessionalSuspended se publica al suspender un profesional.
// Consumido por:
//   - Scheduling: cancelar o reasignar reservas futuras del profesional.
//   - Notifications: avisar al profesional y a los pacientes afectados.
type ProfessionalSuspended struct {
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	Reason         string                     `json:"reason"`
	SuspendedBy    uuid.UUID                  `json:"suspended_by"`
	OccurredAt     time.Time                  `json:"occurred_at"`
}

func (e ProfessionalSuspended) EventType() string      { return "professional.suspended" }
func (e ProfessionalSuspended) AggregateID() string    { return e.ProfessionalID.String() }
func (e ProfessionalSuspended) AggregateType() string  { return "Professional" }
func (e ProfessionalSuspended) BoundedContext() string { return boundedContext }
func (e ProfessionalSuspended) SchemaVersion() int     { return 1 }

// ── ProfessionalLicenseExpiringSoon ───────────────────────────────

// ProfessionalLicenseExpiringSoon se publica por un job scheduler
// cuando una matrícula vence en los próximos 30 días.
// Consumido por: Notifications (recordatorio al profesional y admin).
type ProfessionalLicenseExpiringSoon struct {
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	LicenseID      uuid.UUID                  `json:"license_id"`
	SpecialtyCode  string                     `json:"specialty_code"`
	ExpiresAt      time.Time                  `json:"expires_at"`
	DaysRemaining  int                        `json:"days_remaining"`
	OccurredAt     time.Time                  `json:"occurred_at"`
}

func (e ProfessionalLicenseExpiringSoon) EventType() string {
	return "professional.license.expiring_soon"
}
func (e ProfessionalLicenseExpiringSoon) AggregateID() string    { return e.ProfessionalID.String() }
func (e ProfessionalLicenseExpiringSoon) AggregateType() string  { return "Professional" }
func (e ProfessionalLicenseExpiringSoon) BoundedContext() string { return boundedContext }
func (e ProfessionalLicenseExpiringSoon) SchemaVersion() int     { return 1 }
