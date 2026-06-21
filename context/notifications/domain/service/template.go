// Package service contiene los Domain Services del bounded context Notifications.
package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
)

// ── Message — resultado del renderizado de un template ───────────

// Message es el mensaje listo para enviar por un canal específico.
type Message struct {
	To      string // destino: número de teléfono, email, etc.
	Subject string // solo para email
	Body    string // cuerpo del mensaje
}

// ── TemplateData — datos de entrada para los templates ────────────

// TemplateData agrupa todos los campos posibles que un template puede necesitar.
// Cada NotificationType usa un subconjunto de estos campos.
// Se usa una struct plana para simplicidad en el MVP; en producción
// podría evolucionar a un mapa tipado por tipo de notificación.
type TemplateData struct {
	// Destinatario
	PatientName      string
	PatientPhone     string
	PatientEmail     string
	ProfessionalName string
	ProfessionalEmail string
	ProfessionalPhone string

	// Cita
	AppointmentID  string
	ProcedureCode  string
	SlotStart      time.Time
	SlotEnd        time.Time
	ClinicName     string // en MVP: no tenemos el nombre, usamos el ID
	CancellationReason string
	IsLateCancellation bool

	// Matrícula
	LicenseNumber  string
	SpecialtyCode  string
	ExpiresAt      time.Time
	DaysRemaining  int

	// Cuenta
	UserEmail string
	SuspensionReason string
}

// ── TemplateService ───────────────────────────────────────────────

// TemplateService renderiza el mensaje para un tipo de notificación y canal dados.
// En el MVP usa templates inline (strings). En producción: html/template con archivos.
type TemplateService struct{}

func NewTemplateService() *TemplateService {
	return &TemplateService{}
}

// Render produce un Message listo para enviar.
// Retorna error si el tipo de notificación no tiene template para el canal dado.
func (s *TemplateService) Render(
	notifType valueobject.NotificationType,
	channel valueobject.Channel,
	data TemplateData,
) (Message, error) {
	switch notifType {
	case valueobject.TypeAppointmentBooked:
		return s.appointmentBooked(channel, data)
	case valueobject.TypeAppointmentConfirmed:
		return s.appointmentConfirmed(channel, data)
	case valueobject.TypeAppointmentCancelled:
		return s.appointmentCancelled(channel, data)
	case valueobject.TypeAppointmentCompleted:
		return s.appointmentCompleted(channel, data)
	case valueobject.TypeAppointmentReminder:
		return s.appointmentReminder(channel, data)
	case valueobject.TypeAppointmentNoShow:
		return s.appointmentNoShow(channel, data)
	case valueobject.TypePatientWelcome:
		return s.patientWelcome(channel, data)
	case valueobject.TypeLicenseExpiringSoon:
		return s.licenseExpiringSoon(channel, data)
	case valueobject.TypeAccountSuspended:
		return s.accountSuspended(channel, data)
	default:
		return Message{}, fmt.Errorf("template no definido para tipo '%s'", notifType)
	}
}

// ── Templates por tipo ────────────────────────────────────────────

func (s *TemplateService) appointmentBooked(ch valueobject.Channel, d TemplateData) (Message, error) {
	slot := formatSlot(d.SlotStart, d.SlotEnd)
	body := fmt.Sprintf(
		"✅ Tu turno fue confirmado.\n"+
			"📅 %s\n"+
			"👨‍⚕️ %s\n"+
			"🦷 %s\n\n"+
			"Si necesitás cancelar, hacelo con al menos 24 hs de anticipación.",
		slot, d.ProfessionalName, d.ProcedureCode,
	)
	return buildMessage(ch, d.PatientPhone, d.PatientEmail, "Turno confirmado - OdontoAgenda", body), nil
}

func (s *TemplateService) appointmentConfirmed(ch valueobject.Channel, d TemplateData) (Message, error) {
	slot := formatSlot(d.SlotStart, d.SlotEnd)
	body := fmt.Sprintf(
		"✅ Tu turno fue autorizado por la cobertura.\n"+
			"📅 %s\n"+
			"👨‍⚕️ %s\n\n"+
			"¡Te esperamos!",
		slot, d.ProfessionalName,
	)
	return buildMessage(ch, d.PatientPhone, d.PatientEmail, "Turno autorizado - OdontoAgenda", body), nil
}

func (s *TemplateService) appointmentCancelled(ch valueobject.Channel, d TemplateData) (Message, error) {
	slot := formatSlot(d.SlotStart, d.SlotEnd)
	extra := ""
	if d.IsLateCancellation {
		extra = "\n⚠️ La cancelación fue tardía (menos de 24 hs). Puede aplicarse un cargo."
	}
	body := fmt.Sprintf(
		"❌ Tu turno del %s con %s fue cancelado.\n"+
			"Motivo: %s%s\n\n"+
			"Podés reservar un nuevo turno cuando quieras.",
		slot, d.ProfessionalName, d.CancellationReason, extra,
	)
	return buildMessage(ch, d.PatientPhone, d.PatientEmail, "Turno cancelado - OdontoAgenda", body), nil
}

func (s *TemplateService) appointmentCompleted(ch valueobject.Channel, d TemplateData) (Message, error) {
	body := fmt.Sprintf(
		"🦷 ¡Gracias por tu visita!\n"+
			"Procedimiento: %s\n"+
			"Profesional: %s\n\n"+
			"Recordá mantener tu higiene bucal y volver en tu próximo control. 😊",
		d.ProcedureCode, d.ProfessionalName,
	)
	return buildMessage(ch, d.PatientPhone, d.PatientEmail, "Resumen de tu visita - OdontoAgenda", body), nil
}

func (s *TemplateService) appointmentReminder(ch valueobject.Channel, d TemplateData) (Message, error) {
	slot := formatSlot(d.SlotStart, d.SlotEnd)
	body := fmt.Sprintf(
		"⏰ Recordatorio: tenés un turno mañana.\n"+
			"📅 %s\n"+
			"👨‍⚕️ %s\n\n"+
			"Si no podés asistir, cancelá con anticipación.",
		slot, d.ProfessionalName,
	)
	return buildMessage(ch, d.PatientPhone, d.PatientEmail, "Recordatorio de turno - OdontoAgenda", body), nil
}

func (s *TemplateService) appointmentNoShow(ch valueobject.Channel, d TemplateData) (Message, error) {
	// Este tipo va al staff interno, no al paciente.
	body := fmt.Sprintf(
		"[NO-SHOW] Paciente: %s\n"+
			"Turno: %s\n"+
			"Profesional: %s\n"+
			"ID: %s",
		d.PatientName, formatSlot(d.SlotStart, d.SlotEnd),
		d.ProfessionalName, d.AppointmentID,
	)
	return buildMessage(ch, d.PatientPhone, d.PatientEmail, "No-show registrado - OdontoAgenda", body), nil
}

func (s *TemplateService) patientWelcome(ch valueobject.Channel, d TemplateData) (Message, error) {
	body := fmt.Sprintf(
		"👋 ¡Bienvenido/a a OdontoAgenda, %s!\n\n"+
			"Tu cuenta fue creada exitosamente. "+
			"Ya podés reservar turnos con nuestros profesionales.",
		firstName(d.PatientName),
	)
	return buildMessage(ch, d.PatientPhone, d.PatientEmail, "Bienvenido/a a OdontoAgenda", body), nil
}

func (s *TemplateService) licenseExpiringSoon(ch valueobject.Channel, d TemplateData) (Message, error) {
	body := fmt.Sprintf(
		"⚠️ Atención %s: tu matrícula %s (%s) vence en %d días (%s).\n\n"+
			"Renovála antes del vencimiento para seguir atendiendo.",
		d.ProfessionalName, d.LicenseNumber, d.SpecialtyCode,
		d.DaysRemaining, d.ExpiresAt.Format("02/01/2006"),
	)
	return buildMessage(ch, d.ProfessionalPhone, d.ProfessionalEmail,
		"⚠️ Matrícula por vencer - OdontoAgenda", body), nil
}

func (s *TemplateService) accountSuspended(ch valueobject.Channel, d TemplateData) (Message, error) {
	body := fmt.Sprintf(
		"🔒 Tu cuenta en OdontoAgenda fue suspendida.\n"+
			"Motivo: %s\n\n"+
			"Contactá al administrador si creés que es un error.",
		d.SuspensionReason,
	)
	return buildMessage(ch, "", d.UserEmail, "Cuenta suspendida - OdontoAgenda", body), nil
}

// ── Helpers ───────────────────────────────────────────────────────

func buildMessage(ch valueobject.Channel, phone, email, subject, body string) Message {
	switch ch {
	case valueobject.ChannelEmail:
		return Message{To: email, Subject: subject, Body: body}
	case valueobject.ChannelWhatsApp, valueobject.ChannelSMS:
		return Message{To: phone, Body: body}
	default: // ChannelLog y cualquier fallback
		return Message{To: phone, Subject: subject, Body: body}
	}
}

func formatSlot(start, end time.Time) string {
	date := start.Format("02/01/2006")
	startTime := start.Format("15:04")
	endTime := end.Format("15:04")
	return fmt.Sprintf("%s de %s a %s hs", date, startTime, endTime)
}

func firstName(fullName string) string {
	parts := strings.Fields(fullName)
	if len(parts) > 0 {
		return parts[0]
	}
	return fullName
}
