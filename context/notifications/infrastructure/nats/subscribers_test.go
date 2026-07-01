package nats_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	notifcmd "github.com/juantevez/odontoagenda/context/notifications/application/command"
	natsinf "github.com/juantevez/odontoagenda/context/notifications/infrastructure/nats"
	"github.com/juantevez/odontoagenda/context/notifications/domain/service"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
	"github.com/juantevez/odontoagenda/context/notifications/infrastructure/sender"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
)

// ── captureSender ─────────────────────────────────────────────────

type captureSender struct {
	ch      valueobject.Channel
	called  bool
	lastMsg service.Message
	sendErr error
}

var _ sender.Sender = (*captureSender)(nil)

func (c *captureSender) Channel() valueobject.Channel { return c.ch }
func (c *captureSender) Send(_ context.Context, msg service.Message) error {
	c.called = true
	c.lastMsg = msg
	return c.sendErr
}

// ── helpers ───────────────────────────────────────────────────────

// fullHandler crea un SendNotificationHandler con captureSenders para los 4 canales.
func fullHandler() (*notifcmd.SendNotificationHandler, map[valueobject.Channel]*captureSender) {
	caps := map[valueobject.Channel]*captureSender{
		valueobject.ChannelWhatsApp: {ch: valueobject.ChannelWhatsApp},
		valueobject.ChannelEmail:    {ch: valueobject.ChannelEmail},
		valueobject.ChannelSMS:      {ch: valueobject.ChannelSMS},
		valueobject.ChannelLog:      {ch: valueobject.ChannelLog},
	}
	senders := []sender.Sender{
		caps[valueobject.ChannelWhatsApp],
		caps[valueobject.ChannelEmail],
		caps[valueobject.ChannelSMS],
		caps[valueobject.ChannelLog],
	}
	router := sender.NewRouterSender(senders...)
	h := notifcmd.NewSendNotificationHandler(service.NewTemplateService(), router)
	return h, caps
}

func newNotifDeps() (*mockBus, map[valueobject.Channel]*captureSender, *natsinf.NotificationEventSubscriber) {
	bus := &mockBus{}
	h, caps := fullHandler()
	sub := natsinf.NewNotificationEventSubscriber(bus, h)
	return bus, caps, sub
}

func registerNotif(t *testing.T, sub *natsinf.NotificationEventSubscriber, bus *mockBus) map[string]pkgevents.Handler {
	t.Helper()
	if err := sub.RegisterAll(context.Background()); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	return bus.handlers
}

// ── RegisterAll ───────────────────────────────────────────────────

func TestNotifRegisterAll_Registra8Handlers(t *testing.T) {
	bus, _, sub := newNotifDeps()
	if err := sub.RegisterAll(context.Background()); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	expected := []string{
		"appointment.booked",
		"appointment.confirmed",
		"appointment.cancelled",
		"appointment.completed",
		"appointment.no_show",
		"patient.registered",
		"professional.license.expiring_soon",
		"user.suspended",
	}
	for _, subj := range expected {
		if _, ok := bus.handlers[subj]; !ok {
			t.Errorf("handler no registrado para %q", subj)
		}
	}
	if got := len(bus.handlers); got != len(expected) {
		t.Errorf("handlers registrados = %d, quería %d", got, len(expected))
	}
}

func TestNotifRegisterAll_PropagaErrorDelBus(t *testing.T) {
	bus := &mockBus{subErr: errors.New("nats: conn refused")}
	h, _ := fullHandler()
	sub := natsinf.NewNotificationEventSubscriber(bus, h)

	if err := sub.RegisterAll(context.Background()); err == nil {
		t.Error("RegisterAll debería propagar el error del bus")
	}
}

// ── handleAppointmentBooked ───────────────────────────────────────

func TestNotifHandleBooked_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	err := h["appointment.booked"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestNotifHandleBooked_Exitoso_EnviaPorCanalPreferido(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	payload := map[string]any{
		"appointment_id":    uuid.New().String(),
		"patient_id":        uuid.New().String(),
		"professional_id":   uuid.New().String(),
		"patient_name":      "Ana García",
		"patient_phone":     "+5491112345678",
		"patient_email":     "ana@example.com",
		"professional_name": "Dr. López",
		"procedure_code":    "D0150",
		"slot_start":        fixedSlot,
		"slot_end":          fixedSlot.Add(30 * time.Minute),
		"preferred_channel": "WhatsApp",
	}
	err := h["appointment.booked"](context.Background(), inboxEnvelope("appointment.booked", payload))
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if !caps[valueobject.ChannelWhatsApp].called {
		t.Error("sender WhatsApp no fue llamado")
	}
	if caps[valueobject.ChannelWhatsApp].lastMsg.To != "+5491112345678" {
		t.Errorf("To = %q, quería teléfono del paciente", caps[valueobject.ChannelWhatsApp].lastMsg.To)
	}
}

func TestNotifHandleBooked_NombreProfVacio_UsaFallbackConID(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	profID := uuid.New().String()
	payload := map[string]any{
		"appointment_id":    uuid.New().String(),
		"patient_id":        uuid.New().String(),
		"professional_id":   profID,
		"patient_name":      "Ana García",
		"patient_phone":     "+5491112345678",
		"professional_name": "", // vacío → fallback "Profesional <ID>"
		"procedure_code":    "D0150",
		"slot_start":        fixedSlot,
		"slot_end":          fixedSlot.Add(30 * time.Minute),
		"preferred_channel": "WhatsApp",
	}
	h["appointment.booked"](context.Background(), inboxEnvelope("appointment.booked", payload))

	// El template appointmentBooked incluye ProfessionalName en el body.
	body := caps[valueobject.ChannelWhatsApp].lastMsg.Body
	if !strings.Contains(body, "Profesional "+profID) {
		t.Errorf("Body = %q, debe contener fallback 'Profesional %s'", body, profID)
	}
}

func TestNotifHandleNoShow_NombrePacVacio_UsaFallbackConID(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	patientID := uuid.New().String()
	payload := map[string]any{
		"appointment_id":    uuid.New().String(),
		"patient_id":        patientID,
		"professional_id":   uuid.New().String(),
		"patient_name":      "", // vacío → fallback "Paciente <ID>"
		"patient_phone":     "+5491112345678",
		"professional_name": "Dr. López",
		"slot_start":        fixedSlot,
		"slot_end":          fixedSlot.Add(30 * time.Minute),
	}
	h["appointment.no_show"](context.Background(), inboxEnvelope("appointment.no_show", payload))

	// El template appointmentNoShow sí incluye PatientName en el body.
	body := caps[valueobject.ChannelLog].lastMsg.Body
	if !strings.Contains(body, "Paciente "+patientID) {
		t.Errorf("Body = %q, debe contener fallback 'Paciente %s'", body, patientID)
	}
}

// ── handleAppointmentConfirmed ────────────────────────────────────

func TestNotifHandleConfirmed_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	err := h["appointment.confirmed"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestNotifHandleConfirmed_Exitoso_SenderLlamado(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	payload := map[string]any{
		"appointment_id":    uuid.New().String(),
		"patient_id":        uuid.New().String(),
		"professional_id":   uuid.New().String(),
		"patient_name":      "Ana García",
		"patient_phone":     "+5491112345678",
		"patient_email":     "ana@example.com",
		"professional_name": "Dr. López",
		"slot_start":        fixedSlot,
		"slot_end":          fixedSlot.Add(30 * time.Minute),
		"preferred_channel": "SMS",
	}
	err := h["appointment.confirmed"](context.Background(), inboxEnvelope("appointment.confirmed", payload))
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if !caps[valueobject.ChannelSMS].called {
		t.Error("sender SMS no fue llamado")
	}
}

// ── handleAppointmentCancelled ────────────────────────────────────

func TestNotifHandleCancelled_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	err := h["appointment.cancelled"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestNotifHandleCancelled_Exitoso_PropagaLateCancellation(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	payload := map[string]any{
		"appointment_id":       uuid.New().String(),
		"patient_id":           uuid.New().String(),
		"professional_id":      uuid.New().String(),
		"patient_name":         "Ana García",
		"patient_phone":        "+5491112345678",
		"patient_email":        "ana@example.com",
		"professional_name":    "Dr. López",
		"slot_start":           fixedSlot,
		"slot_end":             fixedSlot.Add(30 * time.Minute),
		"reason":               "viaje de trabajo",
		"is_late_cancellation": true,
		"preferred_channel":    "WhatsApp",
	}
	h["appointment.cancelled"](context.Background(), inboxEnvelope("appointment.cancelled", payload))

	body := caps[valueobject.ChannelWhatsApp].lastMsg.Body
	if !strings.Contains(body, "tardía") {
		t.Errorf("Body = %q, debe mencionar cancelación tardía cuando is_late_cancellation=true", body)
	}
}

// ── handleAppointmentCompleted ────────────────────────────────────

func TestNotifHandleCompleted_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	err := h["appointment.completed"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestNotifHandleCompleted_Exitoso_SenderLlamado(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	payload := map[string]any{
		"appointment_id":    uuid.New().String(),
		"patient_id":        uuid.New().String(),
		"professional_id":   uuid.New().String(),
		"patient_name":      "Ana García",
		"patient_phone":     "+5491112345678",
		"patient_email":     "ana@example.com",
		"professional_name": "Dr. López",
		"procedure_code":    "D0150",
		"slot_start":        fixedSlot,
		"slot_end":          fixedSlot.Add(30 * time.Minute),
		"preferred_channel": "Email",
	}
	err := h["appointment.completed"](context.Background(), inboxEnvelope("appointment.completed", payload))
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if !caps[valueobject.ChannelEmail].called {
		t.Error("sender Email no fue llamado")
	}
}

// ── handleAppointmentNoShow ───────────────────────────────────────

func TestNotifHandleNoShow_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	err := h["appointment.no_show"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestNotifHandleNoShow_SiempreUsaChannelLog(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	payload := map[string]any{
		"appointment_id":    uuid.New().String(),
		"patient_id":        uuid.New().String(),
		"professional_id":   uuid.New().String(),
		"patient_name":      "Ana García",
		"patient_phone":     "+5491112345678",
		"professional_name": "Dr. López",
		"slot_start":        fixedSlot,
		"slot_end":          fixedSlot.Add(30 * time.Minute),
	}
	err := h["appointment.no_show"](context.Background(), inboxEnvelope("appointment.no_show", payload))
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	// BuildAppointmentNoShowCmd siempre usa ChannelLog
	if !caps[valueobject.ChannelLog].called {
		t.Error("sender Log no fue llamado — no-show siempre debe ir a ChannelLog")
	}
}

// ── handlePatientRegistered ───────────────────────────────────────

func TestNotifHandlePatientRegistered_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	err := h["patient.registered"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestNotifHandlePatientRegistered_Exitoso_EnviaBienvenida(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	payload := map[string]any{
		"patient_id":        uuid.New().String(),
		"full_name":         "Ana García",
		"phone":             "+5491112345678",
		"email":             "ana@example.com",
		"preferred_channel": "Email",
	}
	err := h["patient.registered"](context.Background(), inboxEnvelope("patient.registered", payload))
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if !caps[valueobject.ChannelEmail].called {
		t.Error("sender Email no fue llamado")
	}
	if !strings.Contains(caps[valueobject.ChannelEmail].lastMsg.Body, "Bienvenid") {
		t.Errorf("Body = %q, debe contener mensaje de bienvenida", caps[valueobject.ChannelEmail].lastMsg.Body)
	}
}

// ── handleLicenseExpiringSoon ─────────────────────────────────────

func TestNotifHandleLicenseExpiringSoon_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	err := h["professional.license.expiring_soon"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestNotifHandleLicenseExpiringSoon_SiempreUsaEmail(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	payload := map[string]any{
		"professional_id":    uuid.New().String(),
		"license_id":         uuid.New().String(),
		"professional_name":  "Dr. López",
		"professional_email": "lopez@example.com",
		"professional_phone": "+5491187654321",
		"license_number":     "MAT-456",
		"specialty_code":     "ORTODONCIA",
		"days_remaining":     15,
		"expires_at":         time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
	}
	err := h["professional.license.expiring_soon"](context.Background(),
		inboxEnvelope("professional.license.expiring_soon", payload))
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	// BuildLicenseExpiringSoonCmd siempre usa ChannelEmail
	if !caps[valueobject.ChannelEmail].called {
		t.Error("sender Email no fue llamado — license expiring siempre debe ir a Email")
	}
	if caps[valueobject.ChannelEmail].lastMsg.To != "lopez@example.com" {
		t.Errorf("To = %q, quería email del profesional", caps[valueobject.ChannelEmail].lastMsg.To)
	}
}

// ── handleUserSuspended ───────────────────────────────────────────

func TestNotifHandleUserSuspended_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	err := h["user.suspended"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestNotifHandleUserSuspended_EmailVacio_OmiteSinError(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	payload := map[string]any{
		"user_id":    uuid.New().String(),
		"reason":     "violación de términos",
		"user_email": "", // sin email → no se puede notificar
	}
	err := h["user.suspended"](context.Background(), inboxEnvelope("user.suspended", payload))
	if err != nil {
		t.Errorf("handler error = %v, quería nil (email vacío = silencioso)", err)
	}
	for ch, s := range caps {
		if s.called {
			t.Errorf("sender %q fue llamado aunque no hay email — no debería enviarse", ch)
		}
	}
}

func TestNotifHandleUserSuspended_ConEmail_EnviaPorEmail(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	payload := map[string]any{
		"user_id":    uuid.New().String(),
		"reason":     "violación de términos",
		"user_email": "user@example.com",
	}
	err := h["user.suspended"](context.Background(), inboxEnvelope("user.suspended", payload))
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	// BuildAccountSuspendedCmd siempre usa ChannelEmail
	if !caps[valueobject.ChannelEmail].called {
		t.Error("sender Email no fue llamado — account suspended siempre debe ir a Email")
	}
	if caps[valueobject.ChannelEmail].lastMsg.To != "user@example.com" {
		t.Errorf("To = %q, quería user@example.com", caps[valueobject.ChannelEmail].lastMsg.To)
	}
}

func TestNotifHandleUserSuspended_BodyContieneMotivoSuspension(t *testing.T) {
	bus, caps, sub := newNotifDeps()
	h := registerNotif(t, sub, bus)

	payload := map[string]any{
		"user_id":    uuid.New().String(),
		"reason":     "pagos pendientes",
		"user_email": "user@example.com",
	}
	h["user.suspended"](context.Background(), inboxEnvelope("user.suspended", payload))

	if !strings.Contains(caps[valueobject.ChannelEmail].lastMsg.Body, "pagos pendientes") {
		t.Errorf("Body = %q, debe contener el motivo de suspensión", caps[valueobject.ChannelEmail].lastMsg.Body)
	}
}
