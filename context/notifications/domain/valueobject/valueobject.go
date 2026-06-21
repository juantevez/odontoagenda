// Package valueobject define los Value Objects del bounded context Notifications.
package valueobject

import "fmt"

// ── Channel ───────────────────────────────────────────────────────

// Channel es el canal por el que se envía una notificación.
// Refleja las preferencias del paciente (CommunicationChannel en Patient BC).
type Channel string

const (
	ChannelWhatsApp Channel = "WhatsApp"
	ChannelEmail    Channel = "Email"
	ChannelSMS      Channel = "SMS"
	ChannelLog      Channel = "Log" // canal stub: solo loguea, nunca falla
)

func ParseChannel(s string) (Channel, error) {
	switch Channel(s) {
	case ChannelWhatsApp, ChannelEmail, ChannelSMS, ChannelLog:
		return Channel(s), nil
	}
	return "", fmt.Errorf("canal de notificación inválido: '%s'", s)
}

func (c Channel) String() string { return string(c) }

// ── NotificationType ──────────────────────────────────────────────

// NotificationType identifica el tipo de mensaje a enviar.
// Determina qué template se usa y qué datos se necesitan.
type NotificationType string

const (
	// Flujo de citas
	TypeAppointmentBooked    NotificationType = "appointment_booked"
	TypeAppointmentConfirmed NotificationType = "appointment_confirmed"
	TypeAppointmentCancelled NotificationType = "appointment_cancelled"
	TypeAppointmentReminder  NotificationType = "appointment_reminder"
	TypeAppointmentCompleted NotificationType = "appointment_completed"
	TypeAppointmentNoShow    NotificationType = "appointment_no_show"

	// Pacientes
	TypePatientWelcome NotificationType = "patient_welcome"

	// Staff / profesionales
	TypeLicenseExpiringSoon  NotificationType = "license_expiring_soon"
	TypeAccountSuspended     NotificationType = "account_suspended"
)

func (t NotificationType) String() string { return string(t) }

// ── NotificationStatus ────────────────────────────────────────────

// NotificationStatus es el resultado del intento de envío.
type NotificationStatus string

const (
	StatusSent    NotificationStatus = "sent"
	StatusFailed  NotificationStatus = "failed"
	StatusSkipped NotificationStatus = "skipped" // canal deshabilitado o destinatario sin contacto
)
