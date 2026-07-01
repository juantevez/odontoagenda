package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/notifications/application/command"
	"github.com/juantevez/odontoagenda/context/notifications/domain/entity"
	"github.com/juantevez/odontoagenda/context/notifications/domain/repository"
	"github.com/juantevez/odontoagenda/context/notifications/domain/service"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
	"github.com/juantevez/odontoagenda/context/notifications/infrastructure/sender"
)

// ── mockSender ────────────────────────────────────────────────────

type mockSender struct {
	ch      valueobject.Channel
	sendErr error
	called  bool
	lastMsg service.Message
}

var _ sender.Sender = (*mockSender)(nil)

func (m *mockSender) Channel() valueobject.Channel { return m.ch }
func (m *mockSender) Send(_ context.Context, msg service.Message) error {
	m.called = true
	m.lastMsg = msg
	return m.sendErr
}

// ── mockInboxRepo ─────────────────────────────────────────────────

type mockInboxRepo struct {
	saveErr   error
	savedNote *entity.InboxNotification
}

var _ repository.InboxRepository = (*mockInboxRepo)(nil)

func (m *mockInboxRepo) Save(_ context.Context, n *entity.InboxNotification) error {
	m.savedNote = n
	return m.saveErr
}
func (m *mockInboxRepo) FindByClinic(_ context.Context, _ uuid.UUID, _ bool, _ int) ([]*entity.InboxNotification, error) {
	return nil, nil
}
func (m *mockInboxRepo) MarkRead(_ context.Context, _ uuid.UUID) error    { return nil }
func (m *mockInboxRepo) MarkAllRead(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockInboxRepo) CountUnread(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

// ── helpers ───────────────────────────────────────────────────────

func newHandler(ch valueobject.Channel, sendErr error) (*command.SendNotificationHandler, *mockSender) {
	ms := &mockSender{ch: ch, sendErr: sendErr}
	router := sender.NewRouterSender(ms)
	templates := service.NewTemplateService()
	h := command.NewSendNotificationHandler(templates, router)
	return h, ms
}

func futureSlot() (time.Time, time.Time) {
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Minute)
	return start, start.Add(30 * time.Minute)
}

// ── SendNotificationHandler.Handle ────────────────────────────────

func TestSendNotificationHandler_Exitoso_RetornaNil(t *testing.T) {
	h, ms := newHandler(valueobject.ChannelWhatsApp, nil)
	start, end := futureSlot()

	cmd := command.BuildAppointmentBookedCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", "D0150", start, end, "WhatsApp",
	)

	if err := h.Handle(context.Background(), cmd); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !ms.called {
		t.Error("Sender.Send no fue llamado")
	}
}

func TestSendNotificationHandler_TemplateNoExiste_RetornaNil(t *testing.T) {
	// Un tipo desconocido hace que TemplateService.Render falle.
	// El handler lo trata como no-fatal → retorna nil.
	h, ms := newHandler(valueobject.ChannelEmail, nil)

	cmd := command.SendNotificationCommand{
		Type:    valueobject.NotificationType("tipo_inexistente"),
		Channel: valueobject.ChannelEmail,
		Data:    service.TemplateData{PatientEmail: "x@example.com"},
	}

	if err := h.Handle(context.Background(), cmd); err != nil {
		t.Errorf("Handle() error = %v, quería nil (template no-fatal)", err)
	}
	if ms.called {
		t.Error("Sender.Send fue llamado aunque el template falló")
	}
}

func TestSendNotificationHandler_SenderFalla_PropagaError(t *testing.T) {
	sentinel := errors.New("whatsapp: provider down")
	h, _ := newHandler(valueobject.ChannelWhatsApp, sentinel)
	start, end := futureSlot()

	cmd := command.BuildAppointmentBookedCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", "D0150", start, end, "WhatsApp",
	)

	err := h.Handle(context.Background(), cmd)
	if !errors.Is(err, sentinel) {
		t.Errorf("Handle() error = %v, quería el error sentinel", err)
	}
}

func TestSendNotificationHandler_SinSenderParaCanal_RetornaNil(t *testing.T) {
	// Router sin senders → canal no registrado → no es error fatal.
	router := sender.NewRouterSender() // vacío
	h := command.NewSendNotificationHandler(service.NewTemplateService(), router)
	start, end := futureSlot()

	cmd := command.BuildAppointmentBookedCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", "D0150", start, end, "Email",
	)

	if err := h.Handle(context.Background(), cmd); err != nil {
		t.Errorf("Handle() error = %v, quería nil (canal sin sender)", err)
	}
}

func TestSendNotificationHandler_MensajeUsaDestinatarioCorrecto(t *testing.T) {
	h, ms := newHandler(valueobject.ChannelEmail, nil)

	cmd := command.BuildAppointmentBookedCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", "D0150", time.Now().Add(25*time.Hour), time.Now().Add(26*time.Hour), "Email",
	)

	_ = h.Handle(context.Background(), cmd)

	if ms.lastMsg.To != "ana@example.com" {
		t.Errorf("Message.To = %q, quería email del paciente", ms.lastMsg.To)
	}
}

func TestSendNotificationHandler_WhatsApp_UsaTelefono(t *testing.T) {
	h, ms := newHandler(valueobject.ChannelWhatsApp, nil)
	start, end := futureSlot()

	cmd := command.BuildAppointmentBookedCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", "D0150", start, end, "WhatsApp",
	)

	_ = h.Handle(context.Background(), cmd)

	if ms.lastMsg.To != "+5491112345678" {
		t.Errorf("Message.To = %q, quería teléfono para WhatsApp", ms.lastMsg.To)
	}
}

// ── PreferredChannel ──────────────────────────────────────────────

func TestPreferredChannel_Email(t *testing.T) {
	if got := command.PreferredChannel("Email"); got != valueobject.ChannelEmail {
		t.Errorf("PreferredChannel(%q) = %q, quería Email", "Email", got)
	}
}

func TestPreferredChannel_SMS(t *testing.T) {
	if got := command.PreferredChannel("SMS"); got != valueobject.ChannelSMS {
		t.Errorf("PreferredChannel(%q) = %q, quería SMS", "SMS", got)
	}
}

func TestPreferredChannel_WhatsApp(t *testing.T) {
	if got := command.PreferredChannel("WhatsApp"); got != valueobject.ChannelWhatsApp {
		t.Errorf("PreferredChannel(%q) = %q, quería WhatsApp", "WhatsApp", got)
	}
}

func TestPreferredChannel_Vacio_FallbackWhatsApp(t *testing.T) {
	if got := command.PreferredChannel(""); got != valueobject.ChannelWhatsApp {
		t.Errorf("PreferredChannel(%q) = %q, quería WhatsApp (fallback)", "", got)
	}
}

func TestPreferredChannel_Desconocido_FallbackWhatsApp(t *testing.T) {
	for _, raw := range []string{"Telegram", "Push", "canal_raro"} {
		if got := command.PreferredChannel(raw); got != valueobject.ChannelWhatsApp {
			t.Errorf("PreferredChannel(%q) = %q, quería WhatsApp (fallback)", raw, got)
		}
	}
}

// ── BuildAppointmentBookedCmd ─────────────────────────────────────

func TestBuildAppointmentBookedCmd(t *testing.T) {
	start, end := futureSlot()
	cmd := command.BuildAppointmentBookedCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", "D0150", start, end, "Email",
	)

	if cmd.Type != valueobject.TypeAppointmentBooked {
		t.Errorf("Type = %q, quería TypeAppointmentBooked", cmd.Type)
	}
	if cmd.Channel != valueobject.ChannelEmail {
		t.Errorf("Channel = %q, quería Email", cmd.Channel)
	}
	if cmd.Data.PatientName != "Ana García" {
		t.Errorf("Data.PatientName = %q", cmd.Data.PatientName)
	}
	if cmd.Data.ProcedureCode != "D0150" {
		t.Errorf("Data.ProcedureCode = %q", cmd.Data.ProcedureCode)
	}
	if !cmd.Data.SlotStart.Equal(start) {
		t.Errorf("Data.SlotStart = %v, quería %v", cmd.Data.SlotStart, start)
	}
}

// ── BuildAppointmentConfirmedCmd ──────────────────────────────────

func TestBuildAppointmentConfirmedCmd(t *testing.T) {
	start, end := futureSlot()
	cmd := command.BuildAppointmentConfirmedCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", start, end, "SMS",
	)

	if cmd.Type != valueobject.TypeAppointmentConfirmed {
		t.Errorf("Type = %q, quería TypeAppointmentConfirmed", cmd.Type)
	}
	if cmd.Channel != valueobject.ChannelSMS {
		t.Errorf("Channel = %q, quería SMS", cmd.Channel)
	}
	if cmd.Data.ProfessionalName != "Dr. López" {
		t.Errorf("Data.ProfessionalName = %q", cmd.Data.ProfessionalName)
	}
}

// ── BuildAppointmentCancelledCmd ──────────────────────────────────

func TestBuildAppointmentCancelledCmd_SinLateCancellation(t *testing.T) {
	start, end := futureSlot()
	cmd := command.BuildAppointmentCancelledCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", start, end, "patient_request", false, "WhatsApp",
	)

	if cmd.Type != valueobject.TypeAppointmentCancelled {
		t.Errorf("Type = %q, quería TypeAppointmentCancelled", cmd.Type)
	}
	if cmd.Data.CancellationReason != "patient_request" {
		t.Errorf("Data.CancellationReason = %q", cmd.Data.CancellationReason)
	}
	if cmd.Data.IsLateCancellation {
		t.Error("Data.IsLateCancellation = true, quería false")
	}
}

func TestBuildAppointmentCancelledCmd_ConLateCancellation(t *testing.T) {
	start, end := futureSlot()
	cmd := command.BuildAppointmentCancelledCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", start, end, "late_notice", true, "Email",
	)

	if !cmd.Data.IsLateCancellation {
		t.Error("Data.IsLateCancellation = false, quería true")
	}
}

// ── BuildAppointmentCompletedCmd ──────────────────────────────────

func TestBuildAppointmentCompletedCmd(t *testing.T) {
	cmd := command.BuildAppointmentCompletedCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", "D0150", "WhatsApp",
	)

	if cmd.Type != valueobject.TypeAppointmentCompleted {
		t.Errorf("Type = %q, quería TypeAppointmentCompleted", cmd.Type)
	}
	if cmd.Data.ProcedureCode != "D0150" {
		t.Errorf("Data.ProcedureCode = %q", cmd.Data.ProcedureCode)
	}
}

// ── BuildAppointmentNoShowCmd ─────────────────────────────────────

func TestBuildAppointmentNoShowCmd_SiempreUsaChannelLog(t *testing.T) {
	start, end := futureSlot()
	apptID := uuid.New().String()
	cmd := command.BuildAppointmentNoShowCmd(
		"Ana García", "+5491112345678", "ana@example.com",
		"Dr. López", apptID, start, end,
	)

	if cmd.Type != valueobject.TypeAppointmentNoShow {
		t.Errorf("Type = %q, quería TypeAppointmentNoShow", cmd.Type)
	}
	if cmd.Channel != valueobject.ChannelLog {
		t.Errorf("Channel = %q, quería ChannelLog (no-show va al staff)", cmd.Channel)
	}
	if cmd.Data.AppointmentID != apptID {
		t.Errorf("Data.AppointmentID = %q, quería %q", cmd.Data.AppointmentID, apptID)
	}
}

// ── BuildPatientWelcomeCmd ────────────────────────────────────────

func TestBuildPatientWelcomeCmd(t *testing.T) {
	cmd := command.BuildPatientWelcomeCmd(
		"Ana García", "+5491112345678", "ana@example.com", "Email",
	)

	if cmd.Type != valueobject.TypePatientWelcome {
		t.Errorf("Type = %q, quería TypePatientWelcome", cmd.Type)
	}
	if cmd.Channel != valueobject.ChannelEmail {
		t.Errorf("Channel = %q, quería Email", cmd.Channel)
	}
	if cmd.Data.PatientName != "Ana García" {
		t.Errorf("Data.PatientName = %q", cmd.Data.PatientName)
	}
}

// ── BuildLicenseExpiringSoonCmd ───────────────────────────────────

func TestBuildLicenseExpiringSoonCmd_SiempreEmail(t *testing.T) {
	expiresAt := time.Now().UTC().Add(15 * 24 * time.Hour)
	cmd := command.BuildLicenseExpiringSoonCmd(
		"Dr. López", "+5491187654321", "lopez@example.com",
		"MAT-123", "ORTODONCIA", expiresAt, 15,
	)

	if cmd.Type != valueobject.TypeLicenseExpiringSoon {
		t.Errorf("Type = %q, quería TypeLicenseExpiringSoon", cmd.Type)
	}
	if cmd.Channel != valueobject.ChannelEmail {
		t.Errorf("Channel = %q, quería Email (profesionales siempre email)", cmd.Channel)
	}
	if cmd.Data.LicenseNumber != "MAT-123" {
		t.Errorf("Data.LicenseNumber = %q", cmd.Data.LicenseNumber)
	}
	if cmd.Data.DaysRemaining != 15 {
		t.Errorf("Data.DaysRemaining = %d, quería 15", cmd.Data.DaysRemaining)
	}
	if !cmd.Data.ExpiresAt.Equal(expiresAt) {
		t.Errorf("Data.ExpiresAt = %v, quería %v", cmd.Data.ExpiresAt, expiresAt)
	}
}

// ── BuildAccountSuspendedCmd ──────────────────────────────────────

func TestBuildAccountSuspendedCmd_SiempreEmail(t *testing.T) {
	cmd := command.BuildAccountSuspendedCmd("user@example.com", "violación de términos")

	if cmd.Type != valueobject.TypeAccountSuspended {
		t.Errorf("Type = %q, quería TypeAccountSuspended", cmd.Type)
	}
	if cmd.Channel != valueobject.ChannelEmail {
		t.Errorf("Channel = %q, quería Email", cmd.Channel)
	}
	if cmd.Data.UserEmail != "user@example.com" {
		t.Errorf("Data.UserEmail = %q", cmd.Data.UserEmail)
	}
	if cmd.Data.SuspensionReason != "violación de términos" {
		t.Errorf("Data.SuspensionReason = %q", cmd.Data.SuspensionReason)
	}
}

// ── WriteInboxHandler ─────────────────────────────────────────────

func TestWriteInboxHandler_Exitoso_RetornaNil(t *testing.T) {
	repo := &mockInboxRepo{}
	h := command.NewWriteInboxHandler(repo)
	clinicID := uuid.New()

	err := h.Handle(context.Background(), command.WriteInboxCommand{
		Type:        valueobject.TypeAppointmentBooked,
		ClinicID:    &clinicID,
		ReferenceID: uuid.New().String(),
		Title:       "Turno reservado",
		Body:        "El paciente reservó un turno para mañana.",
	})

	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if repo.savedNote == nil {
		t.Fatal("InboxRepository.Save no fue llamado")
	}
}

func TestWriteInboxHandler_PersisteCamposCorrectamente(t *testing.T) {
	repo := &mockInboxRepo{}
	h := command.NewWriteInboxHandler(repo)
	clinicID := uuid.New()
	refID := uuid.New().String()

	_ = h.Handle(context.Background(), command.WriteInboxCommand{
		Type:        valueobject.TypeLicenseExpiringSoon,
		ClinicID:    &clinicID,
		ReferenceID: refID,
		Title:       "Matrícula por vencer",
		Body:        "La matrícula vence en 15 días.",
	})

	n := repo.savedNote
	if n == nil {
		t.Fatal("savedNote es nil")
	}
	if n.Type != valueobject.TypeLicenseExpiringSoon {
		t.Errorf("Type = %q, quería TypeLicenseExpiringSoon", n.Type)
	}
	if n.ClinicID == nil || *n.ClinicID != clinicID {
		t.Errorf("ClinicID = %v, quería %v", n.ClinicID, clinicID)
	}
	if n.ReferenceID != refID {
		t.Errorf("ReferenceID = %q, quería %q", n.ReferenceID, refID)
	}
	if n.Title != "Matrícula por vencer" {
		t.Errorf("Title = %q", n.Title)
	}
	if n.Body != "La matrícula vence en 15 días." {
		t.Errorf("Body = %q", n.Body)
	}
}

func TestWriteInboxHandler_ClinicIDNil_VisibleEnTodasLasSedes(t *testing.T) {
	repo := &mockInboxRepo{}
	h := command.NewWriteInboxHandler(repo)

	_ = h.Handle(context.Background(), command.WriteInboxCommand{
		Type:     valueobject.TypeAppointmentNoShow,
		ClinicID: nil, // visible globalmente
		Title:    "No-show registrado",
		Body:     "Paciente no asistió.",
	})

	if repo.savedNote == nil {
		t.Fatal("savedNote es nil")
	}
	if repo.savedNote.ClinicID != nil {
		t.Errorf("ClinicID = %v, quería nil", repo.savedNote.ClinicID)
	}
}

func TestWriteInboxHandler_NuevoID_CadaVez(t *testing.T) {
	repo := &mockInboxRepo{}
	h := command.NewWriteInboxHandler(repo)

	ids := make(map[uuid.UUID]bool)
	for i := 0; i < 3; i++ {
		_ = h.Handle(context.Background(), command.WriteInboxCommand{
			Type:  valueobject.TypeAppointmentBooked,
			Title: "Turno", Body: "Body",
		})
		if repo.savedNote != nil {
			ids[repo.savedNote.ID] = true
		}
	}
	if len(ids) != 3 {
		t.Errorf("IDs únicos = %d, quería 3 (cada notificación debe tener ID distinto)", len(ids))
	}
}

func TestWriteInboxHandler_RepoFalla_PropagaError(t *testing.T) {
	sentinel := errors.New("db: constraint violation")
	repo := &mockInboxRepo{saveErr: sentinel}
	h := command.NewWriteInboxHandler(repo)

	err := h.Handle(context.Background(), command.WriteInboxCommand{
		Type: valueobject.TypeAppointmentBooked, Title: "T", Body: "B",
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("Handle() error = %v, quería el error sentinel", err)
	}
}

func TestWriteInboxHandler_NuevoIDDistintoDelCero(t *testing.T) {
	repo := &mockInboxRepo{}
	h := command.NewWriteInboxHandler(repo)

	_ = h.Handle(context.Background(), command.WriteInboxCommand{
		Type: valueobject.TypePatientWelcome, Title: "T", Body: "B",
	})

	if repo.savedNote.ID == uuid.Nil {
		t.Error("InboxNotification.ID es uuid.Nil — debe ser generado")
	}
}

func TestWriteInboxHandler_CreatedAt_EsCercanoAHora(t *testing.T) {
	repo := &mockInboxRepo{}
	h := command.NewWriteInboxHandler(repo)
	before := time.Now().UTC().Add(-time.Second)

	_ = h.Handle(context.Background(), command.WriteInboxCommand{
		Type: valueobject.TypePatientWelcome, Title: "T", Body: "B",
	})

	if repo.savedNote.CreatedAt.Before(before) {
		t.Errorf("CreatedAt = %v está en el pasado", repo.savedNote.CreatedAt)
	}
}
