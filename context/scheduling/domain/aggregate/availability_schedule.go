// Package aggregate contiene los Aggregates del bounded context Scheduling.
//
// Aggregates:
//   - AvailabilitySchedule: proyección de disponibilidad por (Professional, Clinic)
//   - Appointment: reserva de cita (en appointment.go)
package aggregate

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/event"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── AvailabilitySchedule — Aggregate Root ────────────────────────

// AvailabilitySchedule es la proyección de disponibilidad de un profesional
// en una sede específica. Es el AR que Scheduling mantiene como "read model
// de escritura": se construye a partir de eventos del contexto Professional
// y es la fuente de verdad para calcular slots libres.
//
// Clave de negocio: (ProfessionalID, ClinicID) — única en el sistema.
//
// Invariantes:
//   - Un BlockedSlot no puede solaparse con otro BlockedSlot existente.
//   - WorkingHours define el horario base; las excepciones lo sobreescriben.
//   - Los BookedSlots son proyecciones de Appointments confirmados/pendientes;
//     se agregan al confirmar y se eliminan al cancelar una cita.
//   - Version se incrementa en cada modificación para optimistic locking.
type AvailabilitySchedule struct {
	id             uuid.UUID
	professionalID sharedtypes.ProfessionalID
	clinicID       sharedtypes.ClinicID

	// Horario recurrente semanal (refleja Professional.ClinicAssignment.WeeklySchedule)
	workingHours []WorkingHour

	// Días de excepción (refleja Professional.ClinicAssignment.ExceptionDays)
	exceptionDays []ExceptionDay

	// Slots bloqueados manualmente (vacaciones, reuniones, etc.)
	blockedSlots []BlockedSlot

	// Slots ya reservados: proyección de Appointments activos.
	// Se usa para verificar disponibilidad sin ir a la tabla de appointments.
	bookedSlots []BookedSlot

	// Duraciones de procedimientos para este profesional en esta sede.
	// Refleja Professional.ClinicAssignment.ProcedureDurations.
	procedureDurations map[string]int // ProcedureCode → minutos totales (incluye buffer)

	isActive  bool
	updatedAt time.Time
	version   int64

	pendingEvents []event.DomainEvent
}

// ── WorkingHour — Value Object ────────────────────────────────────

// WorkingHour define el horario de un día de la semana.
type WorkingHour struct {
	Weekday   time.Weekday
	StartHour int
	StartMin  int
	EndHour   int
	EndMin    int
}

func (w WorkingHour) StartMinutes() int { return w.StartHour*60 + w.StartMin }
func (w WorkingHour) EndMinutes() int   { return w.EndHour*60 + w.EndMin }

// ContainsTime verifica si un momento cae dentro de este horario.
func (w WorkingHour) ContainsTime(t time.Time) bool {
	if t.Weekday() != w.Weekday {
		return false
	}
	tMins := t.Hour()*60 + t.Minute()
	return tMins >= w.StartMinutes() && tMins < w.EndMinutes()
}

// ── ExceptionDay — Value Object ───────────────────────────────────

type ExceptionDay struct {
	Date      time.Time // solo año/mes/día
	IsWorking bool
	StartHour int
	StartMin  int
	EndHour   int
	EndMin    int
	Reason    string
}

func (e ExceptionDay) MatchesDate(t time.Time) bool {
	return e.Date.Year() == t.Year() &&
		e.Date.Month() == t.Month() &&
		e.Date.Day() == t.Day()
}

// ── BlockedSlot — Entity ──────────────────────────────────────────

// BlockedSlot es un intervalo de tiempo bloqueado manualmente.
type BlockedSlot struct {
	ID     uuid.UUID
	Slot   valueobject.TimeSlot
	Reason valueobject.BlockedSlotReason
	Note   string
}

// ── BookedSlot — Value Object (proyección de Appointments) ────────

// BookedSlot es la proyección de un Appointment activo sobre la agenda.
// Se mantiene sincronizado vía eventos de dominio del mismo contexto.
type BookedSlot struct {
	AppointmentID sharedtypes.AppointmentID
	Slot          valueobject.TimeSlot
	PatientID     sharedtypes.PatientID
	ProcedureCode string
	Status        valueobject.AppointmentStatus
}

// ── Constructor ───────────────────────────────────────────────────

// NewAvailabilitySchedule crea un AvailabilitySchedule inicial.
// Típicamente creado al recibir el evento ProfessionalAssignedToClinic.
func NewAvailabilitySchedule(
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	workingHours []WorkingHour,
	procedureDurations map[string]int,
) *AvailabilitySchedule {
	return &AvailabilitySchedule{
		id:                 uuid.New(),
		professionalID:     professionalID,
		clinicID:           clinicID,
		workingHours:       workingHours,
		exceptionDays:      []ExceptionDay{},
		blockedSlots:       []BlockedSlot{},
		bookedSlots:        []BookedSlot{},
		procedureDurations: procedureDurations,
		isActive:           true,
		updatedAt:          time.Now().UTC(),
		version:            1,
		pendingEvents:      []event.DomainEvent{},
	}
}

// Reconstitute reconstruye desde persistencia.
func ReconstituteSchedule(
	id uuid.UUID,
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	workingHours []WorkingHour,
	exceptionDays []ExceptionDay,
	blockedSlots []BlockedSlot,
	bookedSlots []BookedSlot,
	procedureDurations map[string]int,
	isActive bool,
	updatedAt time.Time,
	version int64,
) *AvailabilitySchedule {
	return &AvailabilitySchedule{
		id:                 id,
		professionalID:     professionalID,
		clinicID:           clinicID,
		workingHours:       workingHours,
		exceptionDays:      exceptionDays,
		blockedSlots:       blockedSlots,
		bookedSlots:        bookedSlots,
		procedureDurations: procedureDurations,
		isActive:           isActive,
		updatedAt:          updatedAt,
		version:            version,
		pendingEvents:      []event.DomainEvent{},
	}
}

// ── Comportamiento: Schedule Management ──────────────────────────

// UpdateWorkingHours reemplaza el horario recurrente completo.
// Llamado al recibir ProfessionalScheduleUpdated con ChangeType=weekly.
func (s *AvailabilitySchedule) UpdateWorkingHours(hours []WorkingHour) {
	s.workingHours = hours
	s.updatedAt = time.Now().UTC()
	s.version++
}

// SetExceptionDay agrega o reemplaza una excepción para una fecha.
func (s *AvailabilitySchedule) SetExceptionDay(exc ExceptionDay) {
	for i, existing := range s.exceptionDays {
		if existing.MatchesDate(exc.Date) {
			s.exceptionDays[i] = exc
			s.updatedAt = time.Now().UTC()
			s.version++
			return
		}
	}
	s.exceptionDays = append(s.exceptionDays, exc)
	s.updatedAt = time.Now().UTC()
	s.version++
}

// RemoveExceptionDay elimina la excepción de una fecha.
func (s *AvailabilitySchedule) RemoveExceptionDay(date time.Time) {
	filtered := s.exceptionDays[:0]
	for _, exc := range s.exceptionDays {
		if !exc.MatchesDate(date) {
			filtered = append(filtered, exc)
		}
	}
	s.exceptionDays = filtered
	s.updatedAt = time.Now().UTC()
	s.version++
}

// AddBlockedSlot bloquea un intervalo de tiempo manualmente.
// Invariante: no puede solaparse con otro blocked slot existente.
func (s *AvailabilitySchedule) AddBlockedSlot(
	slot valueobject.TimeSlot,
	reason valueobject.BlockedSlotReason,
	note string,
) error {
	for _, existing := range s.blockedSlots {
		if existing.Slot.Overlaps(slot) {
			return sharederrors.NewConflict(
				fmt.Sprintf("el slot %s solapa con un bloqueo existente %s", slot, existing.Slot),
				nil,
			)
		}
	}

	s.blockedSlots = append(s.blockedSlots, BlockedSlot{
		ID:     uuid.New(),
		Slot:   slot,
		Reason: reason,
		Note:   note,
	})
	s.updatedAt = time.Now().UTC()
	s.version++
	return nil
}

// RemoveBlockedSlot elimina un bloqueo por ID.
func (s *AvailabilitySchedule) RemoveBlockedSlot(blockID uuid.UUID) error {
	for i, b := range s.blockedSlots {
		if b.ID == blockID {
			s.blockedSlots = append(s.blockedSlots[:i], s.blockedSlots[i+1:]...)
			s.updatedAt = time.Now().UTC()
			s.version++
			return nil
		}
	}
	return sharederrors.NewNotFound("BlockedSlot", blockID.String())
}

// UpdateProcedureDurations actualiza el mapa de duraciones.
func (s *AvailabilitySchedule) UpdateProcedureDurations(durations map[string]int) {
	s.procedureDurations = durations
	s.updatedAt = time.Now().UTC()
	s.version++
}

// Deactivate marca el schedule como inactivo (profesional desasignado de la sede).
func (s *AvailabilitySchedule) Deactivate() {
	s.isActive = false
	s.updatedAt = time.Now().UTC()
	s.version++
}

// ── Comportamiento: Booked Slots (proyección) ─────────────────────

// AddBookedSlot registra un appointment confirmado/pendiente en la proyección.
// Llamado internamente al confirmar una reserva exitosa.
func (s *AvailabilitySchedule) AddBookedSlot(
	appointmentID sharedtypes.AppointmentID,
	slot valueobject.TimeSlot,
	patientID sharedtypes.PatientID,
	procedureCode string,
	status valueobject.AppointmentStatus,
) error {
	// Verificar que no solapa con slots ya reservados activos.
	for _, existing := range s.bookedSlots {
		if existing.Status.IsActive() && existing.Slot.Overlaps(slot) {
			return sharederrors.NewConflict(
				fmt.Sprintf("slot %s ya está ocupado por appointment %s",
					slot, existing.AppointmentID),
				nil,
			)
		}
	}

	s.bookedSlots = append(s.bookedSlots, BookedSlot{
		AppointmentID: appointmentID,
		Slot:          slot,
		PatientID:     patientID,
		ProcedureCode: procedureCode,
		Status:        status,
	})
	s.updatedAt = time.Now().UTC()
	s.version++
	return nil
}

// ReleaseBookedSlot libera un slot cuando un appointment es cancelado o no-show.
func (s *AvailabilitySchedule) ReleaseBookedSlot(appointmentID sharedtypes.AppointmentID) {
	for i, b := range s.bookedSlots {
		if b.AppointmentID == appointmentID {
			s.bookedSlots[i].Status = valueobject.StatusCancelled
			s.updatedAt = time.Now().UTC()
			s.version++
			return
		}
	}
}

// ── Disponibilidad ────────────────────────────────────────────────

// IsAvailableAt verifica si hay disponibilidad en un TimeSlot dado.
// Considera: horario recurrente, excepciones, blocked slots y booked slots.
func (s *AvailabilitySchedule) IsAvailableAt(slot valueobject.TimeSlot) bool {
	if !s.isActive {
		return false
	}

	// 1. Verificar que el slot cae dentro del horario de trabajo.
	if !s.isWithinWorkingHours(slot) {
		return false
	}

	// 2. Verificar que no hay blocked slots solapados.
	for _, b := range s.blockedSlots {
		if b.Slot.Overlaps(slot) {
			return false
		}
	}

	// 3. Verificar que no hay booked slots activos solapados.
	for _, b := range s.bookedSlots {
		if b.Status.IsActive() && b.Slot.Overlaps(slot) {
			return false
		}
	}

	return true
}

// isWithinWorkingHours verifica si un slot cae dentro del horario laboral.
// Prioriza excepciones sobre el horario recurrente.
func (s *AvailabilitySchedule) isWithinWorkingHours(slot valueobject.TimeSlot) bool {
	// Verificar excepción para la fecha del slot.
	for _, exc := range s.exceptionDays {
		if exc.MatchesDate(slot.Start) {
			if !exc.IsWorking {
				return false // día no laborable por excepción
			}
			// Horario especial del día de excepción.
			excStart := exc.StartHour*60 + exc.StartMin
			excEnd := exc.EndHour*60 + exc.EndMin
			slotStartMins := slot.Start.Hour()*60 + slot.Start.Minute()
			slotEndMins := slot.End.Hour()*60 + slot.End.Minute()
			return slotStartMins >= excStart && slotEndMins <= excEnd
		}
	}

	// Sin excepción: verificar horario recurrente.
	for _, wh := range s.workingHours {
		if wh.Weekday == slot.Start.Weekday() {
			slotStartMins := slot.Start.Hour()*60 + slot.Start.Minute()
			slotEndMins := slot.End.Hour()*60 + slot.End.Minute()
			if slotStartMins >= wh.StartMinutes() && slotEndMins <= wh.EndMinutes() {
				return true
			}
		}
	}
	return false
}

// DurationForProcedure retorna la duración en minutos para un procedimiento.
func (s *AvailabilitySchedule) DurationForProcedure(procedureCode string) (int, bool) {
	d, ok := s.procedureDurations[procedureCode]
	return d, ok
}

// ── Getters ───────────────────────────────────────────────────────

func (s *AvailabilitySchedule) ID() uuid.UUID                              { return s.id }
func (s *AvailabilitySchedule) ProfessionalID() sharedtypes.ProfessionalID { return s.professionalID }
func (s *AvailabilitySchedule) ClinicID() sharedtypes.ClinicID             { return s.clinicID }
func (s *AvailabilitySchedule) WorkingHours() []WorkingHour                { return s.workingHours }
func (s *AvailabilitySchedule) ExceptionDays() []ExceptionDay              { return s.exceptionDays }
func (s *AvailabilitySchedule) BlockedSlots() []BlockedSlot                { return s.blockedSlots }
func (s *AvailabilitySchedule) BookedSlots() []BookedSlot                  { return s.bookedSlots }
func (s *AvailabilitySchedule) ProcedureDurations() map[string]int         { return s.procedureDurations }
func (s *AvailabilitySchedule) IsActive() bool                             { return s.isActive }
func (s *AvailabilitySchedule) UpdatedAt() time.Time                       { return s.updatedAt }
func (s *AvailabilitySchedule) Version() int64                             { return s.version }

func (s *AvailabilitySchedule) PendingEvents() []event.DomainEvent {
	evts := s.pendingEvents
	s.pendingEvents = nil
	return evts
}
