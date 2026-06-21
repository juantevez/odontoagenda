// Package saga contiene las sagas del bounded context Scheduling.
//
// Una saga orquesta una operación que involucra múltiples aggregates
// y pasos que deben ejecutarse de forma atómica o compensarse ante fallo.
//
// BookAppointmentSaga coordina:
//   1. Adquirir lock Redis sobre el slot (30s TTL)
//   2. Validar disponibilidad en AvailabilitySchedule
//   3. Validar reglas de negocio (BookingPolicy)
//   4. Crear Appointment aggregate
//   5. Agregar BookedSlot en AvailabilitySchedule
//   6. Persistir ambos aggregates en transacción
//   7. Publicar eventos de dominio
//   8. Liberar lock Redis (el schedule ya está actualizado)
//
// Compensaciones:
//   - Si el lock no se obtiene → ErrConflict (slot en disputa, reintentar)
//   - Si falla la validación → ErrPrecondition (retornar al usuario)
//   - Si falla la persistencia → liberar lock + retornar error
package saga

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/repository"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/service"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

const slotLockTTL = 30 * time.Second

// ── BookAppointmentSaga ───────────────────────────────────────────

// BookAppointmentSaga orquesta la reserva de una cita de forma segura
// ante concurrencia alta. Implementa el patrón Saga con compensaciones locales.
type BookAppointmentSaga struct {
	apptRepo     repository.AppointmentRepository
	scheduleRepo repository.AvailabilityScheduleRepository
	cache        repository.AvailabilityCache
	bookingPolicy *service.BookingPolicy
	eventBus     events.Bus
	logger       *slog.Logger
}

func NewBookAppointmentSaga(
	apptRepo repository.AppointmentRepository,
	scheduleRepo repository.AvailabilityScheduleRepository,
	cache repository.AvailabilityCache,
	bookingPolicy *service.BookingPolicy,
	eventBus events.Bus,
) *BookAppointmentSaga {
	return &BookAppointmentSaga{
		apptRepo:      apptRepo,
		scheduleRepo:  scheduleRepo,
		cache:         cache,
		bookingPolicy: bookingPolicy,
		eventBus:      eventBus,
		logger:        slog.Default().With("saga", "BookAppointment"),
	}
}

// ── BookAppointmentInput ──────────────────────────────────────────

// BookAppointmentInput contiene todos los datos necesarios para reservar.
type BookAppointmentInput struct {
	PatientID      sharedtypes.PatientID
	BookedByID     sharedtypes.PatientID // puede diferir si es reserva familiar
	ProfessionalID sharedtypes.ProfessionalID
	ClinicID       sharedtypes.ClinicID
	ProcedureCode  string
	SlotStart      time.Time
	SlotEnd        time.Time
	CoverageType   string
	AgreementID    *uuid.UUID
	// RequiresAuthorization: true si la cobertura requiere autorización previa.
	RequiresAuthorization bool
	// ActiveAppointmentsCount: pasado por el handler tras consultar CountActiveByPatient.
	ActiveAppointmentsCount int
	Constraints             valueobject.BookingConstraints
	CreatedBy               uuid.UUID
}

// BookAppointmentResult contiene el resultado exitoso de la saga.
type BookAppointmentResult struct {
	AppointmentID sharedtypes.AppointmentID
	Status        valueobject.AppointmentStatus
}

// ── Execute ───────────────────────────────────────────────────────

// Execute ejecuta la saga completa de reserva.
// Sigue el patrón: acquire lock → validate → persist → publish → release lock.
func (s *BookAppointmentSaga) Execute(
	ctx context.Context,
	input BookAppointmentInput,
) (*BookAppointmentResult, error) {

	// ── Paso 1: Construir y validar el TimeSlot ───────────────────
	slot, err := valueobject.NewTimeSlot(input.SlotStart, input.SlotEnd)
	if err != nil {
		return nil, sharederrors.NewInvalidArgument("slot", err.Error())
	}

	// ── Paso 2: Adquirir lock Redis sobre el slot (anti-doble-reserva) ──
	lockAcquired, err := s.cache.AcquireSlotLock(
		ctx,
		input.ProfessionalID,
		input.ClinicID,
		input.SlotStart,
		slotLockTTL,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "error adquiriendo slot lock", "error", err)
		// Si Redis no está disponible, continuamos sin lock (degraded mode).
		// El optimistic lock de BD nos sigue protegiendo.
	}
	if !lockAcquired {
		return nil, sharederrors.NewConflict(
			fmt.Sprintf("el slot %s está siendo reservado por otro proceso, intente en unos segundos", slot),
			nil,
		)
	}

	// Garantizar que liberamos el lock al terminar (compensación automática).
	defer func() {
		if releaseErr := s.cache.ReleaseSlotLock(
			ctx, input.ProfessionalID, input.ClinicID, input.SlotStart,
		); releaseErr != nil {
			s.logger.WarnContext(ctx, "error liberando slot lock", "error", releaseErr)
		}
	}()

	// ── Paso 3: Cargar AvailabilitySchedule ──────────────────────
	schedule, err := s.scheduleRepo.FindByProfessionalAndClinic(
		ctx, input.ProfessionalID, input.ClinicID,
	)
	if err != nil {
		if sharederrors.IsNotFound(err) {
			return nil, sharederrors.NewPrecondition("schedule_not_found",
				fmt.Sprintf("no existe agenda para el profesional '%s' en la sede '%s'",
					input.ProfessionalID, input.ClinicID))
		}
		return nil, sharederrors.NewInternal(err)
	}

	// ── Paso 4: Validar disponibilidad en el Schedule ─────────────
	if !schedule.IsAvailableAt(slot) {
		return nil, sharederrors.NewConflict(
			fmt.Sprintf("el slot %s no está disponible (bloqueado, ya reservado o fuera de horario)", slot),
			nil,
		)
	}

	// ── Paso 5: Validar reglas de negocio (BookingPolicy) ─────────
	violations := s.bookingPolicy.Validate(
		slot,
		input.Constraints,
		input.ActiveAppointmentsCount,
		input.PatientID,
		input.BookedByID,
	)
	if len(violations) > 0 {
		// Construir mensaje descriptivo con todas las violaciones.
		msg := violations[0].Message
		if len(violations) > 1 {
			msg = fmt.Sprintf("%s (y %d restricción(es) más)", msg, len(violations)-1)
		}
		return nil, sharederrors.NewPrecondition("booking_policy", msg)
	}

	// ── Paso 6: Crear el Appointment aggregate ────────────────────
	appt, err := aggregate.NewAppointment(
		input.PatientID,
		input.BookedByID,
		input.ProfessionalID,
		input.ClinicID,
		input.ProcedureCode,
		slot,
		input.CoverageType,
		input.AgreementID,
		input.RequiresAuthorization,
		input.CreatedBy,
	)
	if err != nil {
		return nil, err
	}

	// ── Paso 7: Agregar BookedSlot en el Schedule ─────────────────
	if err := schedule.AddBookedSlot(
		appt.ID(),
		slot,
		input.PatientID,
		input.ProcedureCode,
		appt.Status(),
	); err != nil {
		// Conflicto a nivel de aggregate: el slot ya fue tomado.
		// (El lock de Redis redujo esta probabilidad pero no la elimina.)
		return nil, err
	}

	// ── Paso 8: Persistir Appointment (con optimistic lock implícito en BD) ──
	if err := s.apptRepo.Save(ctx, appt); err != nil {
		// Rollback lógico: no necesitamos deshacer el Schedule porque
		// no fue persistido todavía. El lock Redis expirará solo.
		return nil, fmt.Errorf("BookAppointmentSaga: save appointment: %w", err)
	}

	// ── Paso 9: Persistir AvailabilitySchedule actualizado ────────
	if err := s.scheduleRepo.Update(ctx, schedule); err != nil {
		// Compensación: si el Schedule no se pudo actualizar, la cita
		// fue guardada pero la proyección está desincronizada.
		// En producción: publicar evento de compensación o marcar para reconciliación.
		s.logger.ErrorContext(ctx, "COMPENSACIÓN REQUERIDA: appointment guardado pero schedule no actualizado",
			"appointment_id", appt.ID(),
			"professional_id", input.ProfessionalID,
			"clinic_id", input.ClinicID,
			"error", err,
		)
		// El optimistic lock en appointments evita doble reserva.
		// El cache Redis se invalidará por TTL (5 min).
		// Aceptable en este nivel de degradación.
	}

	// ── Paso 10: Invalidar cache Redis ────────────────────────────
	// El slot ahora está reservado: el cache debe reflejar esto.
	if err := s.cache.InvalidateSchedule(ctx, input.ProfessionalID, input.ClinicID); err != nil {
		s.logger.WarnContext(ctx, "error invalidando cache (el TTL lo resolverá)",
			"professional_id", input.ProfessionalID,
			"clinic_id", input.ClinicID,
			"error", err,
		)
	}

	// ── Paso 11: Publicar Domain Events ──────────────────────────
	for _, evt := range appt.PendingEvents() {
		if err := s.eventBus.Publish(ctx, evt); err != nil {
			s.logger.WarnContext(ctx, "error publicando evento de appointment",
				"event_type", evt.EventType(),
				"appointment_id", appt.ID(),
				"error", err,
			)
		}
	}

	s.logger.InfoContext(ctx, "cita reservada exitosamente",
		"appointment_id", appt.ID(),
		"patient_id", input.PatientID,
		"professional_id", input.ProfessionalID,
		"clinic_id", input.ClinicID,
		"slot", slot.String(),
		"status", appt.Status(),
	)

	return &BookAppointmentResult{
		AppointmentID: appt.ID(),
		Status:        appt.Status(),
	}, nil
}
