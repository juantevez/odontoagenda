// Package nats contiene los adaptadores de entrada async del bounded context Notifications.
// Consume eventos de múltiples streams y los traduce a SendNotificationCommand (ACL).
//
// Patrón Anticorruption Layer:
//   Cada handler deserializa el payload externo en una struct local (xyzPayload),
//   extrae solo los campos que Notifications necesita, y construye el Command.
//   Notifications nunca importa tipos de otros bounded contexts.
package nats

import (
	"context"
	"log/slog"
	"time"

	pkgevents "github.com/juantevez/odontoagenda/pkg/events"

	"github.com/juantevez/odontoagenda/context/notifications/application/command"
)

// ── NotificationEventSubscriber ───────────────────────────────────

type NotificationEventSubscriber struct {
	bus     pkgevents.Bus
	handler *command.SendNotificationHandler
	logger  *slog.Logger
}

func NewNotificationEventSubscriber(
	bus pkgevents.Bus,
	handler *command.SendNotificationHandler,
) *NotificationEventSubscriber {
	return &NotificationEventSubscriber{
		bus:     bus,
		handler: handler,
		logger:  slog.Default().With("component", "notifications.nats"),
	}
}

// RegisterAll registra todos los consumers del BC Notifications.
func (s *NotificationEventSubscriber) RegisterAll(ctx context.Context) error {

	// ── APPOINTMENT_EVENTS ────────────────────────────────────────

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "APPOINTMENT_EVENTS",
		Subject:      "appointment.booked",
		ConsumerName: "notifications-appointment-booked",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handleAppointmentBooked); err != nil {
		return err
	}

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "APPOINTMENT_EVENTS",
		Subject:      "appointment.confirmed",
		ConsumerName: "notifications-appointment-confirmed",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handleAppointmentConfirmed); err != nil {
		return err
	}

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "APPOINTMENT_EVENTS",
		Subject:      "appointment.cancelled",
		ConsumerName: "notifications-appointment-cancelled",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handleAppointmentCancelled); err != nil {
		return err
	}

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "APPOINTMENT_EVENTS",
		Subject:      "appointment.completed",
		ConsumerName: "notifications-appointment-completed",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handleAppointmentCompleted); err != nil {
		return err
	}

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "APPOINTMENT_EVENTS",
		Subject:      "appointment.no_show",
		ConsumerName: "notifications-appointment-no-show",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handleAppointmentNoShow); err != nil {
		return err
	}

	// ── PATIENT_EVENTS ────────────────────────────────────────────

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "PATIENT_EVENTS",
		Subject:      "patient.registered",
		ConsumerName: "notifications-patient-registered",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handlePatientRegistered); err != nil {
		return err
	}

	// ── PROFESSIONAL_EVENTS ───────────────────────────────────────

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "PROFESSIONAL_EVENTS",
		Subject:      "professional.license.expiring_soon",
		ConsumerName: "notifications-license-expiring",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handleLicenseExpiringSoon); err != nil {
		return err
	}

	// ── IAM_EVENTS ────────────────────────────────────────────────

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "IAM_EVENTS",
		Subject:      "user.suspended",
		ConsumerName: "notifications-user-suspended",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handleUserSuspended); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "notification subscribers registrados")
	return nil
}

// ── Payloads ACL (structs locales del evento externo) ─────────────

type appointmentBookedPayload struct {
	AppointmentID    string    `json:"appointment_id"`
	PatientID        string    `json:"patient_id"`
	ProfessionalID   string    `json:"professional_id"`
	ClinicID         string    `json:"clinic_id"`
	ProcedureCode    string    `json:"procedure_code"`
	SlotStart        time.Time `json:"slot_start"`
	SlotEnd          time.Time `json:"slot_end"`
	Status           string    `json:"status"`
	CoverageType     string    `json:"coverage_type"`
	// Campos enriquecidos que Scheduling debería incluir en el evento.
	// En el MVP, si no vienen, Notifications loguea con los IDs.
	PatientName      string    `json:"patient_name"`
	PatientPhone     string    `json:"patient_phone"`
	PatientEmail     string    `json:"patient_email"`
	ProfessionalName string    `json:"professional_name"`
	PreferredChannel string    `json:"preferred_channel"`
}

type appointmentConfirmedPayload struct {
	AppointmentID    string    `json:"appointment_id"`
	PatientID        string    `json:"patient_id"`
	ProfessionalID   string    `json:"professional_id"`
	SlotStart        time.Time `json:"slot_start"`
	PatientName      string    `json:"patient_name"`
	PatientPhone     string    `json:"patient_phone"`
	PatientEmail     string    `json:"patient_email"`
	ProfessionalName string    `json:"professional_name"`
	SlotEnd          time.Time `json:"slot_end"`
	PreferredChannel string    `json:"preferred_channel"`
}

type appointmentCancelledPayload struct {
	AppointmentID      string    `json:"appointment_id"`
	PatientID          string    `json:"patient_id"`
	ProfessionalID     string    `json:"professional_id"`
	SlotStart          time.Time `json:"slot_start"`
	SlotEnd            time.Time `json:"slot_end"`
	Reason             string    `json:"reason"`
	IsLateCancellation bool      `json:"is_late_cancellation"`
	PatientName        string    `json:"patient_name"`
	PatientPhone       string    `json:"patient_phone"`
	PatientEmail       string    `json:"patient_email"`
	ProfessionalName   string    `json:"professional_name"`
	PreferredChannel   string    `json:"preferred_channel"`
}

type appointmentCompletedPayload struct {
	AppointmentID    string    `json:"appointment_id"`
	PatientID        string    `json:"patient_id"`
	ProfessionalID   string    `json:"professional_id"`
	ProcedureCode    string    `json:"procedure_code"`
	SlotStart        time.Time `json:"slot_start"`
	SlotEnd          time.Time `json:"slot_end"`
	PatientName      string    `json:"patient_name"`
	PatientPhone     string    `json:"patient_phone"`
	PatientEmail     string    `json:"patient_email"`
	ProfessionalName string    `json:"professional_name"`
	PreferredChannel string    `json:"preferred_channel"`
}

type appointmentNoShowPayload struct {
	AppointmentID  string    `json:"appointment_id"`
	PatientID      string    `json:"patient_id"`
	ProfessionalID string    `json:"professional_id"`
	SlotStart      time.Time `json:"slot_start"`
	SlotEnd        time.Time `json:"slot_end"`
	PatientName    string    `json:"patient_name"`
	PatientPhone   string    `json:"patient_phone"`
	PatientEmail   string    `json:"patient_email"`
	ProfessionalName string  `json:"professional_name"`
}

type patientRegisteredPayload struct {
	PatientID        string    `json:"patient_id"`
	FullName         string    `json:"full_name"`
	Phone            string    `json:"phone"`
	Email            string    `json:"email"`
	PreferredChannel string    `json:"preferred_channel"`
	OccurredAt       time.Time `json:"occurred_at"`
}

type licenseExpiringSoonPayload struct {
	ProfessionalID   string    `json:"professional_id"`
	LicenseID        string    `json:"license_id"`
	SpecialtyCode    string    `json:"specialty_code"`
	LicenseNumber    string    `json:"license_number"`
	ExpiresAt        time.Time `json:"expires_at"`
	DaysRemaining    int       `json:"days_remaining"`
	ProfessionalName  string   `json:"professional_name"`
	ProfessionalEmail string   `json:"professional_email"`
	ProfessionalPhone string   `json:"professional_phone"`
}

type userSuspendedPayload struct {
	UserID    string    `json:"user_id"`
	Reason    string    `json:"reason"`
	UserEmail string    `json:"user_email"`
	OccurredAt time.Time `json:"occurred_at"`
}

// ── Handlers ──────────────────────────────────────────────────────

func (s *NotificationEventSubscriber) handleAppointmentBooked(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[appointmentBookedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando appointment.booked",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	cmd := command.BuildAppointmentBookedCmd(
		fallback(p.PatientName, "Paciente "+p.PatientID),
		p.PatientPhone,
		p.PatientEmail,
		fallback(p.ProfessionalName, "Profesional "+p.ProfessionalID),
		p.ProcedureCode,
		p.SlotStart,
		p.SlotEnd,
		p.PreferredChannel,
	)
	return s.handler.Handle(ctx, cmd)
}

func (s *NotificationEventSubscriber) handleAppointmentConfirmed(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[appointmentConfirmedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando appointment.confirmed",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	cmd := command.BuildAppointmentConfirmedCmd(
		fallback(p.PatientName, "Paciente "+p.PatientID),
		p.PatientPhone,
		p.PatientEmail,
		fallback(p.ProfessionalName, "Profesional "+p.ProfessionalID),
		p.SlotStart,
		p.SlotEnd,
		p.PreferredChannel,
	)
	return s.handler.Handle(ctx, cmd)
}

func (s *NotificationEventSubscriber) handleAppointmentCancelled(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[appointmentCancelledPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando appointment.cancelled",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	cmd := command.BuildAppointmentCancelledCmd(
		fallback(p.PatientName, "Paciente "+p.PatientID),
		p.PatientPhone,
		p.PatientEmail,
		fallback(p.ProfessionalName, "Profesional "+p.ProfessionalID),
		p.SlotStart,
		p.SlotEnd,
		p.Reason,
		p.IsLateCancellation,
		p.PreferredChannel,
	)
	return s.handler.Handle(ctx, cmd)
}

func (s *NotificationEventSubscriber) handleAppointmentCompleted(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[appointmentCompletedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando appointment.completed",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	cmd := command.BuildAppointmentCompletedCmd(
		fallback(p.PatientName, "Paciente "+p.PatientID),
		p.PatientPhone,
		p.PatientEmail,
		fallback(p.ProfessionalName, "Profesional "+p.ProfessionalID),
		p.ProcedureCode,
		p.PreferredChannel,
	)
	return s.handler.Handle(ctx, cmd)
}

func (s *NotificationEventSubscriber) handleAppointmentNoShow(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[appointmentNoShowPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando appointment.no_show",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	cmd := command.BuildAppointmentNoShowCmd(
		fallback(p.PatientName, "Paciente "+p.PatientID),
		p.PatientPhone,
		p.PatientEmail,
		fallback(p.ProfessionalName, "Profesional "+p.ProfessionalID),
		p.AppointmentID,
		p.SlotStart,
		p.SlotEnd,
	)
	return s.handler.Handle(ctx, cmd)
}

func (s *NotificationEventSubscriber) handlePatientRegistered(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[patientRegisteredPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando patient.registered",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	cmd := command.BuildPatientWelcomeCmd(
		p.FullName,
		p.Phone,
		p.Email,
		p.PreferredChannel,
	)
	return s.handler.Handle(ctx, cmd)
}

func (s *NotificationEventSubscriber) handleLicenseExpiringSoon(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[licenseExpiringSoonPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando professional.license.expiring_soon",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	cmd := command.BuildLicenseExpiringSoonCmd(
		fallback(p.ProfessionalName, "Profesional "+p.ProfessionalID),
		p.ProfessionalPhone,
		p.ProfessionalEmail,
		p.LicenseNumber,
		p.SpecialtyCode,
		p.ExpiresAt,
		p.DaysRemaining,
	)
	return s.handler.Handle(ctx, cmd)
}

func (s *NotificationEventSubscriber) handleUserSuspended(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[userSuspendedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando user.suspended",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	if p.UserEmail == "" {
		// Sin email no podemos notificar la suspensión.
		s.logger.WarnContext(ctx, "user.suspended sin email, omitiendo notificación",
			"user_id", p.UserID)
		return nil
	}

	cmd := command.BuildAccountSuspendedCmd(p.UserEmail, p.Reason)
	return s.handler.Handle(ctx, cmd)
}

// ── helpers ───────────────────────────────────────────────────────

// fallback retorna val si no está vacío, o def en caso contrario.
func fallback(val, def string) string {
	if val != "" {
		return val
	}
	return def
}
