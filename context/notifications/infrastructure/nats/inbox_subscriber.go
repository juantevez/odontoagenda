package nats

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	notifcmd "github.com/juantevez/odontoagenda/context/notifications/application/command"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
)

// InboxEventSubscriber escucha los 4 eventos accionables y los persiste
// en notifications.inbox para la bandeja de la recepcionista / admin.
type InboxEventSubscriber struct {
	bus     pkgevents.Bus
	handler *notifcmd.WriteInboxHandler
	logger  *slog.Logger
}

func NewInboxEventSubscriber(
	bus pkgevents.Bus,
	handler *notifcmd.WriteInboxHandler,
) *InboxEventSubscriber {
	return &InboxEventSubscriber{
		bus:     bus,
		handler: handler,
		logger:  slog.Default().With("component", "notifications.inbox"),
	}
}

// RegisterAll registra los consumers que escriben en la bandeja del staff.
func (s *InboxEventSubscriber) RegisterAll(ctx context.Context) error {
	type sub struct {
		stream, subject, consumer string
		fn                        func(context.Context, pkgevents.Envelope) error
	}

	subs := []sub{
		{"APPOINTMENT_EVENTS", "appointment.booked", "inbox-appt-booked", s.handleBooked},
		{"APPOINTMENT_EVENTS", "appointment.cancelled", "inbox-appt-cancelled", s.handleCancelled},
		{"APPOINTMENT_EVENTS", "appointment.no_show", "inbox-appt-no-show", s.handleNoShow},
		{"PROFESSIONAL_EVENTS", "professional.license.expiring_soon", "inbox-license-expiring", s.handleLicenseExpiring},
	}

	for _, sub := range subs {
		if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
			Stream:       sub.stream,
			Subject:      sub.subject,
			ConsumerName: sub.consumer,
			MaxRetries:   3,
			RetryBackoff: 5 * time.Second,
		}, sub.fn); err != nil {
			return fmt.Errorf("inbox subscriber %s: %w", sub.subject, err)
		}
	}

	s.logger.InfoContext(ctx, "inbox subscribers registrados")
	return nil
}

func (s *InboxEventSubscriber) handleBooked(ctx context.Context, env pkgevents.Envelope) error {
	p, err := pkgevents.UnmarshalPayload[appointmentBookedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "inbox: error deserializando appointment.booked", "error", err)
		return pkgevents.ErrSkipRetry
	}
	clinicID := parseOptionalUUID(p.ClinicID)
	patient := fallback(p.PatientName, "Paciente")
	prof := fallback(p.ProfessionalName, "Profesional")
	return s.handler.Handle(ctx, notifcmd.WriteInboxCommand{
		Type:        valueobject.TypeAppointmentBooked,
		ClinicID:    clinicID,
		ReferenceID: p.AppointmentID,
		Title:       "Nuevo turno reservado",
		Body:        fmt.Sprintf("%s reservó un turno con %s para el %s", patient, prof, formatSlot(p.SlotStart)),
	})
}

func (s *InboxEventSubscriber) handleCancelled(ctx context.Context, env pkgevents.Envelope) error {
	p, err := pkgevents.UnmarshalPayload[appointmentCancelledPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "inbox: error deserializando appointment.cancelled", "error", err)
		return pkgevents.ErrSkipRetry
	}
	clinicID := parseOptionalUUID(p.ClinicID)
	patient := fallback(p.PatientName, "Paciente")
	reason := p.Reason
	if reason == "" {
		reason = "sin motivo indicado"
	}
	return s.handler.Handle(ctx, notifcmd.WriteInboxCommand{
		Type:        valueobject.TypeAppointmentCancelled,
		ClinicID:    clinicID,
		ReferenceID: p.AppointmentID,
		Title:       "Turno cancelado",
		Body:        fmt.Sprintf("%s canceló su turno del %s. Motivo: %s", patient, formatSlot(p.SlotStart), reason),
	})
}

func (s *InboxEventSubscriber) handleNoShow(ctx context.Context, env pkgevents.Envelope) error {
	p, err := pkgevents.UnmarshalPayload[appointmentNoShowPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "inbox: error deserializando appointment.no_show", "error", err)
		return pkgevents.ErrSkipRetry
	}
	clinicID := parseOptionalUUID(p.ClinicID)
	patient := fallback(p.PatientName, "Paciente")
	prof := fallback(p.ProfessionalName, "Profesional")
	return s.handler.Handle(ctx, notifcmd.WriteInboxCommand{
		Type:        valueobject.TypeAppointmentNoShow,
		ClinicID:    clinicID,
		ReferenceID: p.AppointmentID,
		Title:       "Paciente no se presentó",
		Body:        fmt.Sprintf("%s no se presentó al turno del %s con %s", patient, formatSlot(p.SlotStart), prof),
	})
}

func (s *InboxEventSubscriber) handleLicenseExpiring(ctx context.Context, env pkgevents.Envelope) error {
	p, err := pkgevents.UnmarshalPayload[licenseExpiringSoonPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "inbox: error deserializando license.expiring_soon", "error", err)
		return pkgevents.ErrSkipRetry
	}
	prof := fallback(p.ProfessionalName, "Profesional")
	return s.handler.Handle(ctx, notifcmd.WriteInboxCommand{
		Type:        valueobject.TypeLicenseExpiringSoon,
		ClinicID:    nil, // visible en todas las sedes
		ReferenceID: p.LicenseID,
		Title:       "Matrícula por vencer",
		Body:        fmt.Sprintf("La matrícula de %s (%s) vence en %d días (%s)", prof, p.SpecialtyCode, p.DaysRemaining, p.ExpiresAt.Format("02/01/2006")),
	})
}

// ── helpers ───────────────────────────────────────────────────────

func parseOptionalUUID(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}

func formatSlot(t time.Time) string {
	return t.Format("02/01 15:04")
}
