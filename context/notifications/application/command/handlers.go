// Package command contiene los Command Handlers del bounded context Notifications.
//
// Cada handler recibe un comando construido desde un evento NATS (ACL del subscriber),
// elige el canal correcto para el destinatario, renderiza el template y despacha.
//
// Decisión de diseño: los handlers NO conocen los senders concretos.
// Solo conocen RouterSender y TemplateService (puertos de dominio).
// La selección de canal es responsabilidad del handler basándose en los datos
// del comando (el canal preferido viene en el payload del evento original).
package command

import (
	"context"
	"log/slog"
	"time"

	"github.com/juantevez/odontoagenda/context/notifications/domain/service"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
	"github.com/juantevez/odontoagenda/context/notifications/infrastructure/sender"
)

// ── SendNotificationCommand — comando genérico ────────────────────

// SendNotificationCommand encapsula todo lo necesario para enviar una notificación.
// Es el objeto que cruza la frontera entre el subscriber NATS y el handler.
type SendNotificationCommand struct {
	Type    valueobject.NotificationType
	Channel valueobject.Channel
	Data    service.TemplateData
}

// ── SendNotificationHandler ───────────────────────────────────────

// SendNotificationHandler orquesta el envío de cualquier notificación.
// Es el único handler del BC: renderiza + despacha.
type SendNotificationHandler struct {
	templates *service.TemplateService
	router    *sender.RouterSender
	logger    *slog.Logger
}

func NewSendNotificationHandler(
	templates *service.TemplateService,
	router *sender.RouterSender,
) *SendNotificationHandler {
	return &SendNotificationHandler{
		templates: templates,
		router:    router,
		logger:    slog.Default().With("handler", "SendNotification"),
	}
}

func (h *SendNotificationHandler) Handle(ctx context.Context, cmd SendNotificationCommand) error {
	// 1. Renderizar template.
	msg, err := h.templates.Render(cmd.Type, cmd.Channel, cmd.Data)
	if err != nil {
		h.logger.WarnContext(ctx, "no hay template para este tipo/canal",
			"type", cmd.Type,
			"channel", cmd.Channel,
			"error", err,
		)
		// No es error fatal: simplemente no enviamos.
		return nil
	}

	// 2. Despachar por el canal correspondiente.
	if err := h.router.Send(ctx, cmd.Channel, msg); err != nil {
		h.logger.ErrorContext(ctx, "error enviando notificación",
			"type", cmd.Type,
			"channel", cmd.Channel,
			"to", msg.To,
			"error", err,
		)
		return err // causará retry en el subscriber NATS
	}

	h.logger.InfoContext(ctx, "notificación enviada",
		"type", cmd.Type,
		"channel", cmd.Channel,
		"to", msg.To,
	)
	return nil
}

// ── Helpers de construcción de comandos (ACL) ─────────────────────
// Estos helpers viven aquí para que el subscriber NATS sea lo más delgado posible.
// Traducen payloads de eventos externos a SendNotificationCommand.

// preferredChannel convierte el string del evento al Channel interno.
// Si el canal es desconocido o vacío, usa WhatsApp como fallback.
func PreferredChannel(raw string) valueobject.Channel {
	switch raw {
	case "Email":
		return valueobject.ChannelEmail
	case "SMS":
		return valueobject.ChannelSMS
	default:
		return valueobject.ChannelWhatsApp
	}
}

// BuildAppointmentBookedCmd construye el comando para appointment.booked.
func BuildAppointmentBookedCmd(
	patientName, patientPhone, patientEmail string,
	professionalName string,
	procedureCode string,
	slotStart, slotEnd time.Time,
	preferredChannel string,
) SendNotificationCommand {
	return SendNotificationCommand{
		Type:    valueobject.TypeAppointmentBooked,
		Channel: PreferredChannel(preferredChannel),
		Data: service.TemplateData{
			PatientName:      patientName,
			PatientPhone:     patientPhone,
			PatientEmail:     patientEmail,
			ProfessionalName: professionalName,
			ProcedureCode:    procedureCode,
			SlotStart:        slotStart,
			SlotEnd:          slotEnd,
		},
	}
}

// BuildAppointmentConfirmedCmd construye el comando para appointment.confirmed.
func BuildAppointmentConfirmedCmd(
	patientName, patientPhone, patientEmail string,
	professionalName string,
	slotStart, slotEnd time.Time,
	preferredChannel string,
) SendNotificationCommand {
	return SendNotificationCommand{
		Type:    valueobject.TypeAppointmentConfirmed,
		Channel: PreferredChannel(preferredChannel),
		Data: service.TemplateData{
			PatientName:      patientName,
			PatientPhone:     patientPhone,
			PatientEmail:     patientEmail,
			ProfessionalName: professionalName,
			SlotStart:        slotStart,
			SlotEnd:          slotEnd,
		},
	}
}

// BuildAppointmentCancelledCmd construye el comando para appointment.cancelled.
func BuildAppointmentCancelledCmd(
	patientName, patientPhone, patientEmail string,
	professionalName string,
	slotStart, slotEnd time.Time,
	reason string,
	isLate bool,
	preferredChannel string,
) SendNotificationCommand {
	return SendNotificationCommand{
		Type:    valueobject.TypeAppointmentCancelled,
		Channel: PreferredChannel(preferredChannel),
		Data: service.TemplateData{
			PatientName:        patientName,
			PatientPhone:       patientPhone,
			PatientEmail:       patientEmail,
			ProfessionalName:   professionalName,
			SlotStart:          slotStart,
			SlotEnd:            slotEnd,
			CancellationReason: reason,
			IsLateCancellation: isLate,
		},
	}
}

// BuildAppointmentCompletedCmd construye el comando para appointment.completed.
func BuildAppointmentCompletedCmd(
	patientName, patientPhone, patientEmail string,
	professionalName string,
	procedureCode string,
	preferredChannel string,
) SendNotificationCommand {
	return SendNotificationCommand{
		Type:    valueobject.TypeAppointmentCompleted,
		Channel: PreferredChannel(preferredChannel),
		Data: service.TemplateData{
			PatientName:      patientName,
			PatientPhone:     patientPhone,
			PatientEmail:     patientEmail,
			ProfessionalName: professionalName,
			ProcedureCode:    procedureCode,
		},
	}
}

// BuildAppointmentNoShowCmd construye el comando para appointment.no_show.
// Va dirigido al staff (email interno), no al paciente.
func BuildAppointmentNoShowCmd(
	patientName, patientPhone, patientEmail string,
	professionalName string,
	appointmentID string,
	slotStart, slotEnd time.Time,
) SendNotificationCommand {
	return SendNotificationCommand{
		Type:    valueobject.TypeAppointmentNoShow,
		Channel: valueobject.ChannelLog, // en el MVP: solo log; en producción: email interno al staff
		Data: service.TemplateData{
			PatientName:      patientName,
			PatientPhone:     patientPhone,
			PatientEmail:     patientEmail,
			ProfessionalName: professionalName,
			AppointmentID:    appointmentID,
			SlotStart:        slotStart,
			SlotEnd:          slotEnd,
		},
	}
}

// BuildPatientWelcomeCmd construye el comando para patient.registered.
func BuildPatientWelcomeCmd(
	patientName, patientPhone, patientEmail string,
	preferredChannel string,
) SendNotificationCommand {
	return SendNotificationCommand{
		Type:    valueobject.TypePatientWelcome,
		Channel: PreferredChannel(preferredChannel),
		Data: service.TemplateData{
			PatientName:  patientName,
			PatientPhone: patientPhone,
			PatientEmail: patientEmail,
		},
	}
}

// BuildLicenseExpiringSoonCmd construye el comando para professional.license.expiring_soon.
func BuildLicenseExpiringSoonCmd(
	professionalName, professionalPhone, professionalEmail string,
	licenseNumber, specialtyCode string,
	expiresAt time.Time,
	daysRemaining int,
) SendNotificationCommand {
	return SendNotificationCommand{
		Type:    valueobject.TypeLicenseExpiringSoon,
		Channel: valueobject.ChannelEmail, // los profesionales reciben por email
		Data: service.TemplateData{
			ProfessionalName:  professionalName,
			ProfessionalPhone: professionalPhone,
			ProfessionalEmail: professionalEmail,
			LicenseNumber:     licenseNumber,
			SpecialtyCode:     specialtyCode,
			ExpiresAt:         expiresAt,
			DaysRemaining:     daysRemaining,
		},
	}
}

// BuildAccountSuspendedCmd construye el comando para user.suspended.
func BuildAccountSuspendedCmd(
	userEmail string,
	reason string,
) SendNotificationCommand {
	return SendNotificationCommand{
		Type:    valueobject.TypeAccountSuspended,
		Channel: valueobject.ChannelEmail,
		Data: service.TemplateData{
			UserEmail:        userEmail,
			SuspensionReason: reason,
		},
	}
}
