package sender

import (
	"context"
	"errors"
	"testing"

	"github.com/juantevez/odontoagenda/context/notifications/domain/service"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
)

// ── mockSender (para RouterSender) ───────────────────────────────

type stubSender struct {
	ch      valueobject.Channel
	sendErr error
	called  bool
	lastMsg service.Message
}

func (s *stubSender) Channel() valueobject.Channel { return s.ch }
func (s *stubSender) Send(_ context.Context, msg service.Message) error {
	s.called = true
	s.lastMsg = msg
	return s.sendErr
}

// ── LogSender ─────────────────────────────────────────────────────

func TestLogSender_Channel_RetornaElCanalConfigurado(t *testing.T) {
	casos := []valueobject.Channel{
		valueobject.ChannelLog,
		valueobject.ChannelWhatsApp,
		valueobject.ChannelEmail,
	}
	for _, ch := range casos {
		s := NewLogSender(ch)
		if got := s.Channel(); got != ch {
			t.Errorf("NewLogSender(%q).Channel() = %q, quería %q", ch, got, ch)
		}
	}
}

func TestLogSender_Send_SiempreRetornaNil(t *testing.T) {
	s := NewLogSender(valueobject.ChannelLog)
	msg := service.Message{To: "+5491112345678", Body: "hola"}

	if err := s.Send(context.Background(), msg); err != nil {
		t.Errorf("Send() error = %v, quería nil", err)
	}
}

func TestLogSender_Send_DestinatarioVacio_RetornaNil(t *testing.T) {
	s := NewLogSender(valueobject.ChannelLog)
	if err := s.Send(context.Background(), service.Message{}); err != nil {
		t.Errorf("Send() con To vacío error = %v, quería nil", err)
	}
}

// ── WhatsAppSender ────────────────────────────────────────────────

func TestWhatsAppSender_NoNil(t *testing.T) {
	if NewWhatsAppSender("url", "token") == nil {
		t.Error("NewWhatsAppSender retornó nil")
	}
}

func TestWhatsAppSender_Channel_RetornaWhatsApp(t *testing.T) {
	s := NewWhatsAppSender("url", "token")
	if s.Channel() != valueobject.ChannelWhatsApp {
		t.Errorf("Channel() = %q, quería ChannelWhatsApp", s.Channel())
	}
}

func TestWhatsAppSender_Send_DestinatarioVacio_RetornaNil(t *testing.T) {
	s := NewWhatsAppSender("url", "token")
	if err := s.Send(context.Background(), service.Message{To: ""}); err != nil {
		t.Errorf("Send(To=\"\") error = %v, quería nil", err)
	}
}

func TestWhatsAppSender_Send_ConDestinatario_RetornaNil(t *testing.T) {
	s := NewWhatsAppSender("url", "token")
	if err := s.Send(context.Background(), service.Message{To: "+5491112345678", Body: "test"}); err != nil {
		t.Errorf("Send() error = %v, quería nil (stub siempre ok)", err)
	}
}

// ── EmailSender ───────────────────────────────────────────────────

func TestEmailSender_NoNil(t *testing.T) {
	if NewEmailSender("from@example.com", "api-key") == nil {
		t.Error("NewEmailSender retornó nil")
	}
}

func TestEmailSender_Channel_RetornaEmail(t *testing.T) {
	s := NewEmailSender("from@example.com", "api-key")
	if s.Channel() != valueobject.ChannelEmail {
		t.Errorf("Channel() = %q, quería ChannelEmail", s.Channel())
	}
}

func TestEmailSender_Send_DestinatarioVacio_RetornaNil(t *testing.T) {
	s := NewEmailSender("from@example.com", "api-key")
	if err := s.Send(context.Background(), service.Message{To: ""}); err != nil {
		t.Errorf("Send(To=\"\") error = %v, quería nil", err)
	}
}

func TestEmailSender_Send_ConDestinatario_RetornaNil(t *testing.T) {
	s := NewEmailSender("from@example.com", "api-key")
	msg := service.Message{To: "user@example.com", Subject: "Turno", Body: "body"}
	if err := s.Send(context.Background(), msg); err != nil {
		t.Errorf("Send() error = %v, quería nil (stub siempre ok)", err)
	}
}

// ── SMSSender ─────────────────────────────────────────────────────

func TestSMSSender_NoNil(t *testing.T) {
	if NewSMSSender("sid", "token", "+1555000000") == nil {
		t.Error("NewSMSSender retornó nil")
	}
}

func TestSMSSender_Channel_RetornaSMS(t *testing.T) {
	s := NewSMSSender("sid", "token", "+1555000000")
	if s.Channel() != valueobject.ChannelSMS {
		t.Errorf("Channel() = %q, quería ChannelSMS", s.Channel())
	}
}

func TestSMSSender_Send_DestinatarioVacio_RetornaNil(t *testing.T) {
	s := NewSMSSender("sid", "token", "+1555000000")
	if err := s.Send(context.Background(), service.Message{To: ""}); err != nil {
		t.Errorf("Send(To=\"\") error = %v, quería nil", err)
	}
}

func TestSMSSender_Send_ConDestinatario_RetornaNil(t *testing.T) {
	s := NewSMSSender("sid", "token", "+1555000000")
	if err := s.Send(context.Background(), service.Message{To: "+5491112345678", Body: "turno"}); err != nil {
		t.Errorf("Send() error = %v, quería nil (stub siempre ok)", err)
	}
}

// ── RouterSender ──────────────────────────────────────────────────

func TestRouterSender_SinSenders_CanalDesconocido_RetornaNil(t *testing.T) {
	r := NewRouterSender()
	err := r.Send(context.Background(), valueobject.ChannelWhatsApp, service.Message{To: "+1"})
	if err != nil {
		t.Errorf("Send() canal sin sender error = %v, quería nil (no es error fatal)", err)
	}
}

func TestRouterSender_CanalRegistrado_DelegaAlSender(t *testing.T) {
	stub := &stubSender{ch: valueobject.ChannelEmail}
	r := NewRouterSender(stub)

	msg := service.Message{To: "user@example.com", Subject: "test", Body: "body"}
	if err := r.Send(context.Background(), valueobject.ChannelEmail, msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !stub.called {
		t.Error("Sender.Send no fue llamado")
	}
	if stub.lastMsg.To != msg.To {
		t.Errorf("lastMsg.To = %q, quería %q", stub.lastMsg.To, msg.To)
	}
}

func TestRouterSender_ErrorDelSender_Propagado(t *testing.T) {
	sentinel := errors.New("email: proveedor caído")
	stub := &stubSender{ch: valueobject.ChannelEmail, sendErr: sentinel}
	r := NewRouterSender(stub)

	err := r.Send(context.Background(), valueobject.ChannelEmail, service.Message{To: "x@x.com"})
	if !errors.Is(err, sentinel) {
		t.Errorf("Send() error = %v, quería error sentinel", err)
	}
}

func TestRouterSender_EnrutaAlCanalCorrecto(t *testing.T) {
	wa := &stubSender{ch: valueobject.ChannelWhatsApp}
	em := &stubSender{ch: valueobject.ChannelEmail}
	sm := &stubSender{ch: valueobject.ChannelSMS}
	r := NewRouterSender(wa, em, sm)

	msg := service.Message{To: "dest", Body: "test"}

	r.Send(context.Background(), valueobject.ChannelEmail, msg)

	if em.called && wa.called || em.called && sm.called {
		t.Error("senders incorrectos fueron llamados")
	}
	if !em.called {
		t.Error("sender Email no fue llamado")
	}
}

func TestRouterSender_UltimoSenderGanaParaMismoCanal(t *testing.T) {
	first := &stubSender{ch: valueobject.ChannelWhatsApp}
	second := &stubSender{ch: valueobject.ChannelWhatsApp}
	r := NewRouterSender(first, second)

	r.Send(context.Background(), valueobject.ChannelWhatsApp, service.Message{To: "+1"})

	if first.called {
		t.Error("primer sender fue llamado — debería ganar el último registrado")
	}
	if !second.called {
		t.Error("segundo sender no fue llamado")
	}
}

func TestRouterSender_MensajeIntactoHastaSender(t *testing.T) {
	stub := &stubSender{ch: valueobject.ChannelSMS}
	r := NewRouterSender(stub)

	msg := service.Message{To: "+5491112345678", Subject: "asunto", Body: "cuerpo largo"}
	r.Send(context.Background(), valueobject.ChannelSMS, msg)

	if stub.lastMsg != msg {
		t.Errorf("mensaje modificado en tránsito: got %+v, quería %+v", stub.lastMsg, msg)
	}
}

// ── truncate ──────────────────────────────────────────────────────

func TestTruncate_StringCorto_SinCambios(t *testing.T) {
	s := "hola mundo"
	if got := truncate(s, 20); got != s {
		t.Errorf("truncate(%q, 20) = %q, quería sin cambios", s, got)
	}
}

func TestTruncate_StringExactoAlMax_SinCambios(t *testing.T) {
	s := "abcde"
	if got := truncate(s, 5); got != s {
		t.Errorf("truncate(%q, 5) = %q, quería sin cambios", s, got)
	}
}

func TestTruncate_StringLargo_TruncaConElipsis(t *testing.T) {
	s := "0123456789"
	got := truncate(s, 5)
	if got != "01234…" {
		t.Errorf("truncate(%q, 5) = %q, quería %q", s, got, "01234…")
	}
}

func TestTruncate_Unicode_TruncaPorRuneNoBytes(t *testing.T) {
	// Cada emoji ocupa 4 bytes pero 1 rune.
	s := "🦷🦷🦷🦷🦷"
	got := truncate(s, 3)
	if got != "🦷🦷🦷…" {
		t.Errorf("truncate(%q, 3) = %q, quería 3 emojis + ellipsis", s, got)
	}
}

func TestTruncate_StringVacio_RetornaVacio(t *testing.T) {
	if got := truncate("", 10); got != "" {
		t.Errorf("truncate(\"\", 10) = %q, quería string vacío", got)
	}
}

func TestTruncate_MaxCero_TodoTruncado(t *testing.T) {
	got := truncate("algo", 0)
	if got != "…" {
		t.Errorf("truncate(\"algo\", 0) = %q, quería solo elipsis", got)
	}
}
