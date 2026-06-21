// Package service contiene los Domain Services del bounded context Scheduling.
package service

import (
	"fmt"
	"time"

	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── SlotCalculator — Domain Service ──────────────────────────────

// SlotCalculator genera todos los slots libres de un AvailabilitySchedule
// para una fecha y procedimiento dados.
//
// Es Domain Service porque coordina múltiples piezas del aggregate
// (horario, excepciones, bloqueados, reservados) para producir un resultado
// que no pertenece a ninguna entidad en particular.
type SlotCalculator struct{}

func NewSlotCalculator() *SlotCalculator { return &SlotCalculator{} }

// CalculateForDate genera todos los slots libres para una fecha específica.
// Parámetros:
//   - schedule: el AvailabilitySchedule del profesional en la sede
//   - date: la fecha para la que calcular (solo se usa año/mes/día; hora ignorada)
//   - procedureCode: el procedimiento a agendar
//   - durationMins: duración total del procedimiento en minutos (incluye buffer)
//
// Algoritmo:
//  1. Determinar el horario del día (excepción o recurrente)
//  2. Iterar en intervalos de `durationMins` desde inicio hasta fin del horario
//  3. Para cada slot candidato, verificar que está libre
//  4. Filtrar slots ya pasados (no se puede reservar en el pasado)
func (sc *SlotCalculator) CalculateForDate(
	schedule *aggregate.AvailabilitySchedule,
	date time.Time,
	procedureCode string,
	durationMins int,
) ([]aggregate.FreeSlot, error) {
	if durationMins < 5 {
		return nil, sharederrors.NewInvalidArgument("duration_mins", "mínimo 5 minutos")
	}
	if !schedule.IsActive() {
		return []aggregate.FreeSlot{}, nil
	}

	// Normalizar la fecha a medianoche UTC.
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	// Obtener el horario efectivo del día.
	startMins, endMins, isWorking := sc.effectiveHoursForDay(schedule, dayStart)
	if !isWorking {
		return []aggregate.FreeSlot{}, nil
	}

	now := time.Now().UTC()
	duration := time.Duration(durationMins) * time.Minute
	freeSlots := make([]aggregate.FreeSlot, 0, 16)

	// Generar candidatos en intervalos de durationMins.
	for startOffset := startMins; startOffset+durationMins <= endMins; startOffset += durationMins {
		slotStart := dayStart.Add(time.Duration(startOffset) * time.Minute)
		slotEnd := slotStart.Add(duration)

		// Ignorar slots en el pasado.
		if slotStart.Before(now) {
			continue
		}

		slot, err := valueobject.NewTimeSlot(slotStart, slotEnd)
		if err != nil {
			continue
		}

		if schedule.IsAvailableAt(slot) {
			freeSlots = append(freeSlots, aggregate.FreeSlot{
				ProfessionalID: schedule.ProfessionalID(),
				ClinicID:       schedule.ClinicID(),
				ProcedureCode:  procedureCode,
				Slot:           slot,
				DurationMins:   durationMins,
			})
		}
	}

	return freeSlots, nil
}

// CalculateForRange genera slots libres para un rango de fechas.
// Útil para búsquedas del tipo "próximos turnos disponibles".
func (sc *SlotCalculator) CalculateForRange(
	schedule *aggregate.AvailabilitySchedule,
	from, to time.Time,
	procedureCode string,
	durationMins int,
	maxResults int,
) ([]aggregate.FreeSlot, error) {
	if to.Before(from) {
		return nil, sharederrors.NewInvalidArgument("to", "debe ser posterior a from")
	}
	if maxResults <= 0 {
		maxResults = 50
	}

	all := make([]aggregate.FreeSlot, 0, maxResults)

	for d := from; !d.After(to) && len(all) < maxResults; d = d.AddDate(0, 0, 1) {
		slots, err := sc.CalculateForDate(schedule, d, procedureCode, durationMins)
		if err != nil {
			return nil, err
		}
		all = append(all, slots...)
	}

	if len(all) > maxResults {
		all = all[:maxResults]
	}
	return all, nil
}

// effectiveHoursForDay retorna el horario efectivo de un día:
// prioriza excepciones sobre el horario recurrente.
// Retorna (startMins, endMins, isWorking).
func (sc *SlotCalculator) effectiveHoursForDay(
	schedule *aggregate.AvailabilitySchedule,
	day time.Time,
) (startMins, endMins int, isWorking bool) {
	// Primero verificar excepciones.
	for _, exc := range schedule.ExceptionDays() {
		if exc.MatchesDate(day) {
			if !exc.IsWorking {
				return 0, 0, false
			}
			return exc.StartHour*60 + exc.StartMin,
				exc.EndHour*60 + exc.EndMin,
				true
		}
	}

	// Sin excepción: horario recurrente del día de la semana.
	for _, wh := range schedule.WorkingHours() {
		if wh.Weekday == day.Weekday() {
			return wh.StartMinutes(), wh.EndMinutes(), true
		}
	}

	return 0, 0, false
}

// ── BookingPolicy — Domain Service ───────────────────────────────

// BookingPolicy valida las reglas de negocio previas a confirmar una reserva.
// Centraliza todas las pre-condiciones de booking en un único servicio
// para que sean fácilmente testeables y configurables.
type BookingPolicy struct{}

func NewBookingPolicy() *BookingPolicy { return &BookingPolicy{} }

// BookingViolation describe una regla de negocio que no se cumple.
type BookingViolation struct {
	Rule    string
	Message string
}

// Validate evalúa todas las reglas de negocio para una reserva propuesta.
// Retorna la lista de violaciones (vacía si todo está OK).
func (p *BookingPolicy) Validate(
	slot valueobject.TimeSlot,
	constraints valueobject.BookingConstraints,
	activeAppointmentsCount int,
	patientID sharedtypes.PatientID,
	bookedByID sharedtypes.PatientID,
) []BookingViolation {
	violations := make([]BookingViolation, 0)
	now := time.Now().UTC()

	// Regla 1: No se puede reservar en el pasado.
	if slot.Start.Before(now) {
		violations = append(violations, BookingViolation{
			Rule:    "no_past_booking",
			Message: "no se puede reservar un turno en el pasado",
		})
	}

	// Regla 2: Anticipación mínima.
	if constraints.MinAdvanceHours > 0 {
		minStart := now.Add(time.Duration(constraints.MinAdvanceHours) * time.Hour)
		if slot.Start.Before(minStart) {
			violations = append(violations, BookingViolation{
				Rule: "min_advance",
				Message: fmt.Sprintf(
					"se requieren al menos %d hora(s) de anticipación para reservar",
					constraints.MinAdvanceHours,
				),
			})
		}
	}

	// Regla 3: No reservar con demasiada anticipación.
	if constraints.MaxAdvanceDays > 0 {
		maxStart := now.AddDate(0, 0, constraints.MaxAdvanceDays)
		if slot.Start.After(maxStart) {
			violations = append(violations, BookingViolation{
				Rule: "max_advance",
				Message: fmt.Sprintf(
					"no se puede reservar con más de %d día(s) de anticipación",
					constraints.MaxAdvanceDays,
				),
			})
		}
	}

	// Regla 4: Límite de citas activas por paciente.
	if constraints.MaxActiveAppointmentsPerPatient > 0 &&
		activeAppointmentsCount >= constraints.MaxActiveAppointmentsPerPatient {
		violations = append(violations, BookingViolation{
			Rule: "max_active_appointments",
			Message: fmt.Sprintf(
				"el paciente ya tiene %d cita(s) activa(s) (máximo %d)",
				activeAppointmentsCount,
				constraints.MaxActiveAppointmentsPerPatient,
			),
		})
	}

	return violations
}

// IsCancellationFree determina si la cancelación de una cita es gratuita.
// Se usa tanto en Cancel (para registrar el flag) como para informar al usuario antes.
func (p *BookingPolicy) IsCancellationFree(
	slotStart time.Time,
	cancellationFreeHours int,
) bool {
	hoursUntilSlot := time.Until(slotStart).Hours()
	return hoursUntilSlot >= float64(cancellationFreeHours)
}
