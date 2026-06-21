// Package nats contiene los adaptadores de entrada async del bounded context Scheduling.
// Recibe eventos de Professional y Patient para mantener sincronizado el AvailabilitySchedule.
package nats

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/repository"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── SchedulingEventSubscriber ─────────────────────────────────────

// SchedulingEventSubscriber registra todos los consumers NATS del contexto Scheduling.
// Mantiene los AvailabilitySchedules sincronizados con los eventos de Professional.
type SchedulingEventSubscriber struct {
	bus          pkgevents.Bus
	scheduleRepo repository.AvailabilityScheduleRepository
	cache        repository.AvailabilityCache
	apptRepo     repository.AppointmentRepository
	logger       *slog.Logger
}

func NewSchedulingEventSubscriber(
	bus pkgevents.Bus,
	scheduleRepo repository.AvailabilityScheduleRepository,
	cache repository.AvailabilityCache,
	apptRepo repository.AppointmentRepository,
) *SchedulingEventSubscriber {
	return &SchedulingEventSubscriber{
		bus:          bus,
		scheduleRepo: scheduleRepo,
		cache:        cache,
		apptRepo:     apptRepo,
		logger:       slog.Default().With("component", "scheduling.nats"),
	}
}

// RegisterAll registra todos los consumers del contexto Scheduling.
func (s *SchedulingEventSubscriber) RegisterAll(ctx context.Context) error {

	// 1. ProfessionalAssignedToClinic → crear AvailabilitySchedule
	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "PROFESSIONAL_EVENTS",
		Subject:      "professional.assigned_to_clinic",
		ConsumerName: "scheduling-create-schedule",
		MaxRetries:   5,
		RetryBackoff: 10 * time.Second,
	}, s.handleProfessionalAssigned); err != nil {
		return err
	}

	// 2. ProfessionalScheduleUpdated → actualizar horarios + invalidar cache
	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "PROFESSIONAL_EVENTS",
		Subject:      "professional.schedule.updated",
		ConsumerName: "scheduling-update-schedule",
		MaxRetries:   5,
		RetryBackoff: 10 * time.Second,
	}, s.handleScheduleUpdated); err != nil {
		return err
	}

	// 3. ProfessionalSuspended → desactivar schedules del profesional
	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "PROFESSIONAL_EVENTS",
		Subject:      "professional.suspended",
		ConsumerName: "scheduling-suspend-professional",
		MaxRetries:   5,
		RetryBackoff: 10 * time.Second,
	}, s.handleProfessionalSuspended); err != nil {
		return err
	}

	// 4. PatientArchived → cancelar citas futuras del paciente
	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "PATIENT_EVENTS",
		Subject:      "patient.archived",
		ConsumerName: "scheduling-patient-archived",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handlePatientArchived); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "scheduling subscribers registrados")
	return nil
}

// ── payloads ACL (Anticorruption Layer) ──────────────────────────

type professionalAssignedPayload struct {
	ProfessionalID string   `json:"professional_id"`
	AssignmentID   string   `json:"assignment_id"`
	ClinicID       string   `json:"clinic_id"`
	Specialties    []string `json:"specialties"`
	AssignedFrom   string   `json:"assigned_from"` // RFC3339
}

type scheduleUpdatedPayload struct {
	ProfessionalID string `json:"professional_id"`
	ClinicID       string `json:"clinic_id"`
	ChangeType     string `json:"change_type"`
	AffectedDate   string `json:"affected_date,omitempty"` // YYYY-MM-DD
}

type professionalSuspendedPayload struct {
	ProfessionalID string `json:"professional_id"`
	Reason         string `json:"reason"`
}

type patientArchivedPayload struct {
	PatientID  string `json:"patient_id"`
	Reason     string `json:"reason"`
	ArchivedBy string `json:"archived_by"`
}

// ── Handlers ──────────────────────────────────────────────────────

// handleProfessionalAssigned crea un AvailabilitySchedule vacío al asignar un profesional.
// El horario detallado llega en el evento schedule.updated subsiguiente.
func (s *SchedulingEventSubscriber) handleProfessionalAssigned(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	payload, err := pkgevents.UnmarshalPayload[professionalAssignedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando professional.assigned_to_clinic",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	profID, err := uuid.Parse(payload.ProfessionalID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}
	clinicID, err := uuid.Parse(payload.ClinicID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	// Verificar si ya existe (idempotencia).
	existing, err := s.scheduleRepo.FindByProfessionalAndClinic(
		ctx,
		sharedtypes.ProfessionalID(profID),
		sharedtypes.ClinicID(clinicID),
	)
	if err == nil && existing != nil {
		s.logger.InfoContext(ctx, "AvailabilitySchedule ya existe, ignorando",
			"professional_id", profID, "clinic_id", clinicID)
		return nil // idempotente
	}

	// Crear schedule vacío (el horario llegará en schedule.updated).
	schedule := aggregate.NewAvailabilitySchedule(
		sharedtypes.ProfessionalID(profID),
		sharedtypes.ClinicID(clinicID),
		[]aggregate.WorkingHour{}, // vacío hasta recibir schedule.updated
		map[string]int{},
	)

	if err := s.scheduleRepo.Save(ctx, schedule); err != nil {
		s.logger.ErrorContext(ctx, "error guardando AvailabilitySchedule",
			"professional_id", profID, "clinic_id", clinicID, "error", err)
		return err
	}

	s.logger.InfoContext(ctx, "AvailabilitySchedule creado",
		"professional_id", profID, "clinic_id", clinicID,
		"event_id", env.EventID,
	)
	return nil
}

// handleScheduleUpdated actualiza el horario e invalida el cache.
// Este es el evento más crítico: cualquier cambio en la agenda del profesional
// debe reflejarse inmediatamente en la disponibilidad.
func (s *SchedulingEventSubscriber) handleScheduleUpdated(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	payload, err := pkgevents.UnmarshalPayload[scheduleUpdatedPayload](env)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	profID, err := uuid.Parse(payload.ProfessionalID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}
	clinicID, err := uuid.Parse(payload.ClinicID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	// Invalidar cache inmediatamente (antes de actualizar el schedule).
	// Esto fuerza un recálculo en la próxima consulta de disponibilidad.
	if err := s.cache.InvalidateSchedule(
		ctx,
		sharedtypes.ProfessionalID(profID),
		sharedtypes.ClinicID(clinicID),
	); err != nil {
		s.logger.WarnContext(ctx, "error invalidando cache", "error", err)
		// No bloqueamos por error de cache; el TTL de 5 min lo resolverá.
	}

	s.logger.InfoContext(ctx, "cache de disponibilidad invalidado",
		"professional_id", profID,
		"clinic_id", clinicID,
		"change_type", payload.ChangeType,
		"event_id", env.EventID,
	)

	// Nota: La actualización detallada del horario (WorkingHours, ExceptionDays)
	// requeriría una consulta al servicio Professional, lo que rompería el
	// aislamiento entre BCs. Estrategia alternativa:
	// - El cache miss forzará una recalculación desde el AvailabilitySchedule.
	// - El AvailabilitySchedule se actualiza cuando Professional envía el detalle
	//   completo (en una versión futura con payload enriquecido del evento).
	// Por ahora: invalidar cache es suficiente para consistencia eventual.

	return nil
}

// handleProfessionalSuspended desactiva todos los schedules del profesional
// y puede disparar cancelación de citas futuras.
func (s *SchedulingEventSubscriber) handleProfessionalSuspended(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	payload, err := pkgevents.UnmarshalPayload[professionalSuspendedPayload](env)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	profID, err := uuid.Parse(payload.ProfessionalID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	// Obtener todos los schedules del profesional.
	// (FindByClinic retorna todos; necesitamos un FindByProfessional — simplificado)
	// En implementación completa: scheduleRepo.FindByProfessional(ctx, profID)

	s.logger.WarnContext(ctx, "profesional suspendido: schedules serán desactivados",
		"professional_id", profID,
		"reason", payload.Reason,
		"event_id", env.EventID,
	)

	// TODO en implementación completa:
	// 1. Obtener todos los schedules del profesional.
	// 2. Llamar schedule.Deactivate() en cada uno.
	// 3. Cancelar todas las citas futuras con reason=CancelBySystem.
	// 4. Publicar eventos de cancelación para Notifications.

	return nil
}

// handlePatientArchived cancela las citas futuras de un paciente archivado.
func (s *SchedulingEventSubscriber) handlePatientArchived(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	payload, err := pkgevents.UnmarshalPayload[patientArchivedPayload](env)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	patientID, err := uuid.Parse(payload.PatientID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	activeAppts, err := s.apptRepo.FindActiveByPatient(
		ctx, sharedtypes.PatientID(patientID),
	)
	if err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "cancelando citas de paciente archivado",
		"patient_id", patientID,
		"active_appointments", len(activeAppts),
		"event_id", env.EventID,
	)

	// En implementación completa: cancelar cada cita con CancelBySystem.
	// Simplificado aquí; el CancelAppointmentHandler se usaría en un loop.

	return nil
}
