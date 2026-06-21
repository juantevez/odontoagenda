// Package valueobject define los Value Objects propios del bounded context Scheduling.
package valueobject

import (
	"fmt"
	"time"
)

// ── AppointmentStatus ─────────────────────────────────────────────

// AppointmentStatus es el estado del ciclo de vida de una cita.
// Las transiciones válidas están enforced en el Aggregate Appointment.
type AppointmentStatus string

const (
	// StatusPending: creada, esperando confirmación (ej: requiere autorización de prepaga)
	StatusPending    AppointmentStatus = "Pending"
	// StatusConfirmed: confirmada, el paciente tiene el turno asegurado
	StatusConfirmed  AppointmentStatus = "Confirmed"
	// StatusInProgress: el paciente llegó y está siendo atendido
	StatusInProgress AppointmentStatus = "InProgress"
	// StatusCompleted: cita finalizada correctamente
	StatusCompleted  AppointmentStatus = "Completed"
	// StatusCancelled: cancelada (por paciente, staff o sistema)
	StatusCancelled  AppointmentStatus = "Cancelled"
	// StatusNoShow: el paciente no se presentó
	StatusNoShow     AppointmentStatus = "NoShow"
)

func ParseAppointmentStatus(s string) (AppointmentStatus, error) {
	switch AppointmentStatus(s) {
	case StatusPending, StatusConfirmed, StatusInProgress,
		StatusCompleted, StatusCancelled, StatusNoShow:
		return AppointmentStatus(s), nil
	}
	return "", fmt.Errorf("estado de cita inválido: '%s'", s)
}

func (s AppointmentStatus) IsActive() bool {
	return s == StatusPending || s == StatusConfirmed || s == StatusInProgress
}

func (s AppointmentStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled || s == StatusNoShow
}

func (s AppointmentStatus) String() string { return string(s) }

// ── CancellationReason ────────────────────────────────────────────

type CancellationReason string

const (
	CancelByPatient      CancellationReason = "patient_request"
	CancelByStaff        CancellationReason = "staff_request"
	CancelBySystem       CancellationReason = "system"           // baja del profesional, cierre de sede
	CancelLateNotice     CancellationReason = "late_notice"      // cancelación tardía (<24h)
	CancelNoAuthorization CancellationReason = "no_authorization" // prepaga no autorizó
)

func (r CancellationReason) IsValid() bool {
	switch r {
	case CancelByPatient, CancelByStaff, CancelBySystem,
		CancelLateNotice, CancelNoAuthorization:
		return true
	}
	return false
}

// ── TimeSlot ─────────────────────────────────────────────────────

// TimeSlot representa un intervalo de tiempo con inicio y fin.
// Es el bloque fundamental de disponibilidad y de la cita misma.
type TimeSlot struct {
	Start time.Time
	End   time.Time
}

func NewTimeSlot(start, end time.Time) (TimeSlot, error) {
	if !end.After(start) {
		return TimeSlot{}, fmt.Errorf("TimeSlot: end (%s) debe ser posterior a start (%s)", end, start)
	}
	if end.Sub(start) < 5*time.Minute {
		return TimeSlot{}, fmt.Errorf("TimeSlot: duración mínima 5 minutos")
	}
	if end.Sub(start) > 8*time.Hour {
		return TimeSlot{}, fmt.Errorf("TimeSlot: duración máxima 8 horas")
	}
	return TimeSlot{
		Start: start.UTC(),
		End:   end.UTC(),
	}, nil
}

func (s TimeSlot) Duration() time.Duration { return s.End.Sub(s.Start) }
func (s TimeSlot) DurationMinutes() int    { return int(s.Duration().Minutes()) }

// Overlaps reporta si este slot se solapa con otro.
// Dos slots se solapan si start_A < end_B && start_B < end_A.
func (s TimeSlot) Overlaps(other TimeSlot) bool {
	return s.Start.Before(other.End) && other.Start.Before(s.End)
}

// Contains reporta si un instante de tiempo cae dentro del slot.
func (s TimeSlot) Contains(t time.Time) bool {
	return !t.Before(s.Start) && t.Before(s.End)
}

func (s TimeSlot) String() string {
	return fmt.Sprintf("[%s → %s]", s.Start.Format("2006-01-02 15:04"), s.End.Format("15:04"))
}

// ── BlockedSlotReason ─────────────────────────────────────────────

type BlockedSlotReason string

const (
	BlockedVacation    BlockedSlotReason = "vacation"
	BlockedMeeting     BlockedSlotReason = "meeting"
	BlockedMaintenance BlockedSlotReason = "maintenance"
	BlockedPersonal    BlockedSlotReason = "personal"
	BlockedOther       BlockedSlotReason = "other"
)

func (r BlockedSlotReason) IsValid() bool {
	switch r {
	case BlockedVacation, BlockedMeeting, BlockedMaintenance, BlockedPersonal, BlockedOther:
		return true
	}
	return false
}

// ── BookingConstraints ────────────────────────────────────────────

// BookingConstraints encapsula las reglas de negocio configurables
// para la creación de reservas. Viven a nivel de sede (ClinicPolicy).
type BookingConstraints struct {
	// MinAdvanceHours: horas mínimas de anticipación para reservar.
	// Ejemplo: 1 = no se puede reservar con menos de 1 hora de anticipación.
	MinAdvanceHours int

	// MaxAdvanceDays: máximos días hacia el futuro que se puede reservar.
	// Ejemplo: 60 = no se puede reservar con más de 60 días de anticipación.
	MaxAdvanceDays int

	// CancellationFreeHours: horas antes de la cita en que la cancelación es gratuita.
	// Pasado ese umbral se aplica cargo o penalización.
	CancellationFreeHours int

	// MaxActiveAppointmentsPerPatient: máximo de citas activas simultáneas por paciente.
	// 0 = sin límite.
	MaxActiveAppointmentsPerPatient int
}

// DefaultBookingConstraints retorna restricciones razonables para producción.
func DefaultBookingConstraints() BookingConstraints {
	return BookingConstraints{
		MinAdvanceHours:                 1,
		MaxAdvanceDays:                  60,
		CancellationFreeHours:           24,
		MaxActiveAppointmentsPerPatient: 5,
	}
}
