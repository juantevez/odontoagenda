// Package nats contiene los adaptadores de entrada async del bounded context Patient.
// Subscriben a Domain Events publicados por otros bounded contexts y los traducen
// a Commands del contexto Patient.
package nats

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/application/command"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── Subscribers ───────────────────────────────────────────────────

// PatientEventSubscriber registra todos los consumers NATS del contexto Patient.
type PatientEventSubscriber struct {
	bus                pkgevents.Bus
	recordVisitHandler *command.RecordCompletedVisitHandler
	logger             *slog.Logger
}

func NewPatientEventSubscriber(
	bus pkgevents.Bus,
	recordVisitHandler *command.RecordCompletedVisitHandler,
) *PatientEventSubscriber {
	return &PatientEventSubscriber{
		bus:                bus,
		recordVisitHandler: recordVisitHandler,
		logger:             slog.Default().With("component", "patient.nats"),
	}
}

// RegisterAll registra todos los consumers del contexto Patient.
// Debe llamarse durante el startup del servicio, después de conectar al bus.
func (s *PatientEventSubscriber) RegisterAll(ctx context.Context) error {
	// Subscriber: AppointmentCompleted → actualizar DentalHistorySummary
	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "APPOINTMENT_EVENTS",
		Subject:      "appointment.completed",
		ConsumerName: "patient-record-visit",
		MaxRetries:   5,
		RetryBackoff: 10 * time.Second,
	}, s.handleAppointmentCompleted); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "patient subscribers registrados")
	return nil
}

// ── appointmentCompletedPayload es el contrato del evento de Scheduling ──

// appointmentCompletedPayload define el payload esperado del evento
// appointment.completed publicado por el contexto Scheduling.
// Es la representación local (ACL) del evento externo.
type appointmentCompletedPayload struct {
	AppointmentID  string `json:"appointment_id"`
	PatientID      string `json:"patient_id"`
	ProfessionalID string `json:"professional_id"`
	ClinicID       string `json:"clinic_id"`
	ProcedureCode  string `json:"procedure_code"`
	Description    string `json:"procedure_description"`
	CompletedAt    string `json:"completed_at"` // RFC3339
}

// handleAppointmentCompleted procesa el evento appointment.completed.
// Patrón: Anticorruption Layer → traduce el evento externo al Command interno.
func (s *PatientEventSubscriber) handleAppointmentCompleted(ctx context.Context, env pkgevents.Envelope) error {
	payload, err := pkgevents.UnmarshalPayload[appointmentCompletedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando appointment.completed",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry // payload malformado: no tiene sentido reintentar
	}

	patientID, err := uuid.Parse(payload.PatientID)
	if err != nil {
		s.logger.ErrorContext(ctx, "patient_id inválido en appointment.completed",
			"raw", payload.PatientID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	professionalID, _ := uuid.Parse(payload.ProfessionalID)
	clinicID, _ := uuid.Parse(payload.ClinicID)

	completedAt, err := time.Parse(time.RFC3339, payload.CompletedAt)
	if err != nil {
		completedAt = time.Now().UTC()
	}

	cmd := command.RecordCompletedVisitCommand{
		PatientID:      sharedtypes.PatientID(patientID),
		ProcedureCode:  payload.ProcedureCode,
		Description:    payload.Description,
		PerformedAt:    completedAt,
		ClinicID:       sharedtypes.ClinicID(clinicID),
		ProfessionalID: sharedtypes.ProfessionalID(professionalID),
		SourceEventID:  env.EventID,
	}

	if err := s.recordVisitHandler.Handle(ctx, cmd); err != nil {
		s.logger.ErrorContext(ctx, "error registrando visita completada",
			"patient_id", payload.PatientID,
			"appointment_id", payload.AppointmentID,
			"error", err)
		return err // causará retry con backoff
	}

	s.logger.InfoContext(ctx, "visita registrada desde evento",
		"patient_id", payload.PatientID,
		"procedure_code", payload.ProcedureCode,
		"event_id", env.EventID,
	)
	return nil
}
