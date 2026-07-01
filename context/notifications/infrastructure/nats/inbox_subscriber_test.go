package nats_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	notifcmd "github.com/juantevez/odontoagenda/context/notifications/application/command"
	"github.com/juantevez/odontoagenda/context/notifications/domain/entity"
	"github.com/juantevez/odontoagenda/context/notifications/domain/repository"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
	natsinf "github.com/juantevez/odontoagenda/context/notifications/infrastructure/nats"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
)

// ── mockBus ───────────────────────────────────────────────────────

type mockBus struct {
	handlers map[string]pkgevents.Handler
	subErr   error
}

var _ pkgevents.Bus = (*mockBus)(nil)

func (m *mockBus) Subscribe(_ context.Context, opts pkgevents.SubscribeOptions, h pkgevents.Handler) error {
	if m.subErr != nil {
		return m.subErr
	}
	if m.handlers == nil {
		m.handlers = make(map[string]pkgevents.Handler)
	}
	m.handlers[opts.Subject] = h
	return nil
}
func (m *mockBus) Publish(_ context.Context, _ pkgevents.DomainEvent) error { return nil }
func (m *mockBus) Close() error                                               { return nil }

// ── mockInboxRepo ─────────────────────────────────────────────────

type mockInboxSaveRepo struct {
	saveErr   error
	savedNote *entity.InboxNotification
}

var _ repository.InboxRepository = (*mockInboxSaveRepo)(nil)

func (m *mockInboxSaveRepo) Save(_ context.Context, n *entity.InboxNotification) error {
	m.savedNote = n
	return m.saveErr
}
func (m *mockInboxSaveRepo) FindByClinic(_ context.Context, _ uuid.UUID, _ bool, _ int) ([]*entity.InboxNotification, error) {
	return nil, nil
}
func (m *mockInboxSaveRepo) MarkRead(_ context.Context, _ uuid.UUID) error    { return nil }
func (m *mockInboxSaveRepo) MarkAllRead(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockInboxSaveRepo) CountUnread(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

// ── helpers ───────────────────────────────────────────────────────

func newInboxDeps() (*mockBus, *mockInboxSaveRepo, *natsinf.InboxEventSubscriber) {
	bus := &mockBus{}
	repo := &mockInboxSaveRepo{}
	handler := notifcmd.NewWriteInboxHandler(repo)
	sub := natsinf.NewInboxEventSubscriber(bus, handler)
	return bus, repo, sub
}

func registerInbox(t *testing.T, sub *natsinf.InboxEventSubscriber, bus *mockBus) map[string]pkgevents.Handler {
	t.Helper()
	if err := sub.RegisterAll(context.Background()); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	return bus.handlers
}

func inboxEnvelope(subject string, payload any) pkgevents.Envelope {
	data, _ := json.Marshal(payload)
	return pkgevents.Envelope{
		EventID:   uuid.New().String(),
		EventType: subject,
		Payload:   json.RawMessage(data),
	}
}

func badEnvelope() pkgevents.Envelope {
	return pkgevents.Envelope{Payload: json.RawMessage(`{{{invalid`)}
}

var fixedSlot = time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

// ── RegisterAll ───────────────────────────────────────────────────

func TestInboxRegisterAll_Registra5Handlers(t *testing.T) {
	bus, _, sub := newInboxDeps()
	if err := sub.RegisterAll(context.Background()); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	expected := []string{
		"appointment.booked",
		"appointment.cancelled",
		"appointment.no_show",
		"appointment.checked_in",
		"professional.license.expiring_soon",
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

func TestInboxRegisterAll_PropagaErrorDelBus(t *testing.T) {
	bus := &mockBus{subErr: errors.New("nats: conn refused")}
	repo := &mockInboxSaveRepo{}
	sub := natsinf.NewInboxEventSubscriber(bus, notifcmd.NewWriteInboxHandler(repo))

	if err := sub.RegisterAll(context.Background()); err == nil {
		t.Error("RegisterAll debería propagar el error del bus")
	}
}

// ── handleBooked ──────────────────────────────────────────────────

func TestInboxHandleBooked_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	err := h["appointment.booked"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestInboxHandleBooked_Exitoso_PersisteCamposCorrectos(t *testing.T) {
	bus, repo, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	clinicID := uuid.New()
	apptID := uuid.New().String()
	payload := map[string]any{
		"appointment_id":    apptID,
		"clinic_id":         clinicID.String(),
		"slot_start":        fixedSlot,
		"patient_name":      "Ana García",
		"professional_name": "Dr. López",
	}
	err := h["appointment.booked"](context.Background(), inboxEnvelope("appointment.booked", payload))
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}

	n := repo.savedNote
	if n == nil {
		t.Fatal("InboxRepository.Save no fue llamado")
	}
	if n.Type != valueobject.TypeAppointmentBooked {
		t.Errorf("Type = %q, quería TypeAppointmentBooked", n.Type)
	}
	if n.ReferenceID != apptID {
		t.Errorf("ReferenceID = %q, quería %q", n.ReferenceID, apptID)
	}
	if n.ClinicID == nil || *n.ClinicID != clinicID {
		t.Errorf("ClinicID = %v, quería %v", n.ClinicID, clinicID)
	}
	if n.Title != "Nuevo turno reservado" {
		t.Errorf("Title = %q", n.Title)
	}
	if !strings.Contains(n.Body, "Ana García") || !strings.Contains(n.Body, "Dr. López") {
		t.Errorf("Body = %q, debe contener paciente y profesional", n.Body)
	}
	if !strings.Contains(n.Body, "15/03 10:00") {
		t.Errorf("Body = %q, debe contener el slot '15/03 10:00'", n.Body)
	}
}

func TestInboxHandleBooked_NombreVacio_UsaFallback(t *testing.T) {
	bus, repo, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	payload := map[string]any{
		"appointment_id":    uuid.New().String(),
		"slot_start":        fixedSlot,
		"patient_name":      "",
		"professional_name": "",
	}
	h["appointment.booked"](context.Background(), inboxEnvelope("appointment.booked", payload))

	n := repo.savedNote
	if !strings.Contains(n.Body, "Paciente") {
		t.Errorf("Body = %q, debe usar fallback 'Paciente'", n.Body)
	}
	if !strings.Contains(n.Body, "Profesional") {
		t.Errorf("Body = %q, debe usar fallback 'Profesional'", n.Body)
	}
}

func TestInboxHandleBooked_ClinicIDVacio_NilEnNotificacion(t *testing.T) {
	bus, repo, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	payload := map[string]any{
		"appointment_id": uuid.New().String(),
		"clinic_id":      "",
		"slot_start":     fixedSlot,
	}
	h["appointment.booked"](context.Background(), inboxEnvelope("appointment.booked", payload))

	if repo.savedNote != nil && repo.savedNote.ClinicID != nil {
		t.Errorf("ClinicID = %v, quería nil para clinic_id vacío", repo.savedNote.ClinicID)
	}
}

// ── handleCancelled ───────────────────────────────────────────────

func TestInboxHandleCancelled_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	err := h["appointment.cancelled"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestInboxHandleCancelled_ConMotivoExplicito(t *testing.T) {
	bus, repo, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	payload := map[string]any{
		"appointment_id":    uuid.New().String(),
		"clinic_id":         uuid.New().String(),
		"slot_start":        fixedSlot,
		"patient_name":      "Ana García",
		"professional_name": "Dr. López",
		"reason":            "viaje de trabajo",
	}
	h["appointment.cancelled"](context.Background(), inboxEnvelope("appointment.cancelled", payload))

	n := repo.savedNote
	if n.Type != valueobject.TypeAppointmentCancelled {
		t.Errorf("Type = %q", n.Type)
	}
	if !strings.Contains(n.Body, "viaje de trabajo") {
		t.Errorf("Body = %q, debe contener el motivo", n.Body)
	}
}

func TestInboxHandleCancelled_SinMotivo_UsaFallback(t *testing.T) {
	bus, repo, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	payload := map[string]any{
		"appointment_id": uuid.New().String(),
		"slot_start":     fixedSlot,
		"reason":         "",
	}
	h["appointment.cancelled"](context.Background(), inboxEnvelope("appointment.cancelled", payload))

	if !strings.Contains(repo.savedNote.Body, "sin motivo indicado") {
		t.Errorf("Body = %q, debe usar fallback de motivo", repo.savedNote.Body)
	}
}

// ── handleNoShow ──────────────────────────────────────────────────

func TestInboxHandleNoShow_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	err := h["appointment.no_show"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestInboxHandleNoShow_Exitoso_PersisteCamposCorrectos(t *testing.T) {
	bus, repo, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	apptID := uuid.New().String()
	payload := map[string]any{
		"appointment_id":    apptID,
		"clinic_id":         uuid.New().String(),
		"slot_start":        fixedSlot,
		"patient_name":      "Ana García",
		"professional_name": "Dr. López",
	}
	h["appointment.no_show"](context.Background(), inboxEnvelope("appointment.no_show", payload))

	n := repo.savedNote
	if n.Type != valueobject.TypeAppointmentNoShow {
		t.Errorf("Type = %q, quería TypeAppointmentNoShow", n.Type)
	}
	if n.Title != "Paciente no se presentó" {
		t.Errorf("Title = %q", n.Title)
	}
	if !strings.Contains(n.Body, "Ana García") || !strings.Contains(n.Body, "Dr. López") {
		t.Errorf("Body = %q, debe contener paciente y profesional", n.Body)
	}
	if n.ReferenceID != apptID {
		t.Errorf("ReferenceID = %q, quería %q", n.ReferenceID, apptID)
	}
}

// ── handleCheckedIn ───────────────────────────────────────────────

func TestInboxHandleCheckedIn_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	err := h["appointment.checked_in"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestInboxHandleCheckedIn_Exitoso_PersisteCamposCorrectos(t *testing.T) {
	bus, repo, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	apptID := uuid.New().String()
	slotStart := time.Date(2026, 3, 15, 14, 30, 0, 0, time.UTC)
	payload := map[string]any{
		"appointment_id":    apptID,
		"clinic_id":         uuid.New().String(),
		"slot_start":        slotStart,
		"patient_name":      "Ana García",
		"professional_name": "Dr. López",
	}
	h["appointment.checked_in"](context.Background(), inboxEnvelope("appointment.checked_in", payload))

	n := repo.savedNote
	if n.Type != valueobject.TypePatientCheckedIn {
		t.Errorf("Type = %q, quería TypePatientCheckedIn", n.Type)
	}
	if n.Title != "Paciente en sala" {
		t.Errorf("Title = %q", n.Title)
	}
	// Body debe incluir la hora del slot en formato HH:MM
	if !strings.Contains(n.Body, "14:30") {
		t.Errorf("Body = %q, debe incluir la hora '14:30'", n.Body)
	}
	if n.ReferenceID != apptID {
		t.Errorf("ReferenceID = %q, quería %q", n.ReferenceID, apptID)
	}
}

// ── handleLicenseExpiring ─────────────────────────────────────────

func TestInboxHandleLicenseExpiring_PayloadInvalido_RetornaSkipRetry(t *testing.T) {
	bus, _, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	err := h["professional.license.expiring_soon"](context.Background(), badEnvelope())
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestInboxHandleLicenseExpiring_ClinicIDSiempreNil(t *testing.T) {
	bus, repo, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	expiresAt := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"license_id":        uuid.New().String(),
		"professional_name": "Dr. López",
		"specialty_code":    "ORTODONCIA",
		"days_remaining":    15,
		"expires_at":        expiresAt,
	}
	h["professional.license.expiring_soon"](context.Background(),
		inboxEnvelope("professional.license.expiring_soon", payload))

	n := repo.savedNote
	if n.ClinicID != nil {
		t.Errorf("ClinicID = %v, quería nil (visible en todas las sedes)", n.ClinicID)
	}
}

func TestInboxHandleLicenseExpiring_Exitoso_PersisteCamposCorrectos(t *testing.T) {
	bus, repo, sub := newInboxDeps()
	h := registerInbox(t, sub, bus)

	licenseID := uuid.New().String()
	expiresAt := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"license_id":        licenseID,
		"professional_name": "Dr. López",
		"specialty_code":    "ORTODONCIA",
		"days_remaining":    15,
		"expires_at":        expiresAt,
	}
	h["professional.license.expiring_soon"](context.Background(),
		inboxEnvelope("professional.license.expiring_soon", payload))

	n := repo.savedNote
	if n.Type != valueobject.TypeLicenseExpiringSoon {
		t.Errorf("Type = %q, quería TypeLicenseExpiringSoon", n.Type)
	}
	if n.Title != "Matrícula por vencer" {
		t.Errorf("Title = %q", n.Title)
	}
	if n.ReferenceID != licenseID {
		t.Errorf("ReferenceID = %q, quería %q", n.ReferenceID, licenseID)
	}
	for _, want := range []string{"Dr. López", "ORTODONCIA", "15", "30/06/2026"} {
		if !strings.Contains(n.Body, want) {
			t.Errorf("Body = %q, debe contener %q", n.Body, want)
		}
	}
}
