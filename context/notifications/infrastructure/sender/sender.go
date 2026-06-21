// Package sender contiene los adaptadores de salida del bounded context Notifications.
//
// Interfaz:
//   Sender — puerto de salida para despachar mensajes por un canal.
//
// Implementaciones:
//   LogSender      — stub: loguea el mensaje en stdout (default en desarrollo).
//   WhatsAppSender — stub: simula envío por WhatsApp (implementación real: Twilio/Baileys).
//   EmailSender    — stub: simula envío por email (implementación real: SendGrid/SMTP).
//   SMSSender      — stub: simula envío por SMS (implementación real: Twilio).
//   RouterSender   — enruta al sender correcto según el canal del mensaje.
package sender

import (
	"context"
	"log/slog"

	"github.com/juantevez/odontoagenda/context/notifications/domain/service"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
)

// ── Sender — puerto de salida ─────────────────────────────────────

// Sender es el puerto de salida para el envío de notificaciones.
// Cada implementación concreta maneja un canal específico.
type Sender interface {
	// Send intenta enviar el mensaje. Retorna nil si fue exitoso o si fue
	// intencionalmente omitido (ej: destinatario vacío).
	Send(ctx context.Context, msg service.Message) error

	// Channel retorna el canal que implementa este sender.
	Channel() valueobject.Channel
}

// ── LogSender — stub para desarrollo ─────────────────────────────

// LogSender es el stub por defecto. Loguea el mensaje en lugar de enviarlo.
// Se usa cuando NOTIFICATIONS_LOG_ONLY=true o cuando el canal real no está configurado.
type LogSender struct {
	channel valueobject.Channel
	logger  *slog.Logger
}

func NewLogSender(channel valueobject.Channel) *LogSender {
	return &LogSender{
		channel: channel,
		logger:  slog.Default().With("component", "sender.log", "channel", channel),
	}
}

func (s *LogSender) Channel() valueobject.Channel { return s.channel }

func (s *LogSender) Send(ctx context.Context, msg service.Message) error {
	s.logger.InfoContext(ctx, "📨 [STUB] notificación",
		"to", msg.To,
		"subject", msg.Subject,
		"body_preview", truncate(msg.Body, 120),
	)
	return nil
}

// ── WhatsAppSender — stub ─────────────────────────────────────────

// WhatsAppSender es el stub del adaptador de WhatsApp.
// Implementación real: llamada HTTP a Twilio WhatsApp API o Baileys (TypeScript).
//
// Contrato de la API real (ejemplo Twilio):
//   POST https://api.twilio.com/2010-04-01/Accounts/{SID}/Messages.json
//   Body: From=whatsapp:+14155238886&To=whatsapp:{phone}&Body={text}
type WhatsAppSender struct {
	providerURL string
	token       string
	logger      *slog.Logger
}

func NewWhatsAppSender(providerURL, token string) *WhatsAppSender {
	return &WhatsAppSender{
		providerURL: providerURL,
		token:       token,
		logger:      slog.Default().With("component", "sender.whatsapp"),
	}
}

func (s *WhatsAppSender) Channel() valueobject.Channel { return valueobject.ChannelWhatsApp }

func (s *WhatsAppSender) Send(ctx context.Context, msg service.Message) error {
	if msg.To == "" {
		s.logger.WarnContext(ctx, "destinatario vacío, omitiendo envío WhatsApp")
		return nil
	}

	// TODO: reemplazar con llamada HTTP real a Twilio / Baileys.
	// Ejemplo con Twilio:
	//   client := twilio.NewRestClient(s.accountSID, s.authToken)
	//   params := &openapi.CreateMessageParams{}
	//   params.SetTo("whatsapp:" + msg.To)
	//   params.SetFrom("whatsapp:+14155238886")
	//   params.SetBody(msg.Body)
	//   _, err := client.Api.CreateMessage(params)

	s.logger.InfoContext(ctx, "📱 [STUB WhatsApp] mensaje",
		"to", msg.To,
		"body_preview", truncate(msg.Body, 120),
	)
	return nil
}

// ── EmailSender — stub ────────────────────────────────────────────

// EmailSender es el stub del adaptador de email.
// Implementación real: SendGrid API o SMTP estándar.
//
// Contrato SendGrid (ejemplo):
//   POST https://api.sendgrid.com/v3/mail/send
//   Authorization: Bearer {API_KEY}
//   Body: { "to": [{email}], "from": {from}, "subject": {subject}, "content": [{body}] }
type EmailSender struct {
	from       string
	apiKey     string
	logger     *slog.Logger
}

func NewEmailSender(from, apiKey string) *EmailSender {
	return &EmailSender{
		from:   from,
		apiKey: apiKey,
		logger: slog.Default().With("component", "sender.email"),
	}
}

func (s *EmailSender) Channel() valueobject.Channel { return valueobject.ChannelEmail }

func (s *EmailSender) Send(ctx context.Context, msg service.Message) error {
	if msg.To == "" {
		s.logger.WarnContext(ctx, "destinatario vacío, omitiendo envío email")
		return nil
	}

	// TODO: reemplazar con SendGrid o net/smtp real.
	// Ejemplo con sendgrid-go:
	//   from := mail.NewEmail("OdontoAgenda", s.from)
	//   to := mail.NewEmail("", msg.To)
	//   message := mail.NewSingleEmail(from, msg.Subject, to, msg.Body, "")
	//   client := sendgrid.NewSendClient(s.apiKey)
	//   _, err := client.Send(message)

	s.logger.InfoContext(ctx, "📧 [STUB Email] mensaje",
		"from", s.from,
		"to", msg.To,
		"subject", msg.Subject,
		"body_preview", truncate(msg.Body, 120),
	)
	return nil
}

// ── SMSSender — stub ──────────────────────────────────────────────

// SMSSender es el stub del adaptador de SMS.
// Implementación real: Twilio SMS API.
type SMSSender struct {
	accountSID  string
	authToken   string
	fromNumber  string
	logger      *slog.Logger
}

func NewSMSSender(accountSID, authToken, fromNumber string) *SMSSender {
	return &SMSSender{
		accountSID: accountSID,
		authToken:  authToken,
		fromNumber: fromNumber,
		logger:     slog.Default().With("component", "sender.sms"),
	}
}

func (s *SMSSender) Channel() valueobject.Channel { return valueobject.ChannelSMS }

func (s *SMSSender) Send(ctx context.Context, msg service.Message) error {
	if msg.To == "" {
		s.logger.WarnContext(ctx, "destinatario vacío, omitiendo envío SMS")
		return nil
	}

	// TODO: reemplazar con Twilio SMS real.
	// Ejemplo:
	//   client := twilio.NewRestClient(s.accountSID, s.authToken)
	//   params := &openapi.CreateMessageParams{}
	//   params.SetTo(msg.To)
	//   params.SetFrom(s.fromNumber)
	//   params.SetBody(msg.Body)
	//   _, err := client.Api.CreateMessage(params)

	s.logger.InfoContext(ctx, "💬 [STUB SMS] mensaje",
		"from", s.fromNumber,
		"to", msg.To,
		"body_preview", truncate(msg.Body, 120),
	)
	return nil
}

// ── RouterSender — enrutador por canal ───────────────────────────

// RouterSender enruta cada mensaje al Sender concreto según el canal.
// Es el adaptador que usa el application layer: recibe cualquier Message
// y lo despacha al sender correcto sin que el handler conozca las implementaciones.
type RouterSender struct {
	senders map[valueobject.Channel]Sender
	logger  *slog.Logger
}

func NewRouterSender(senders ...Sender) *RouterSender {
	m := make(map[valueobject.Channel]Sender, len(senders))
	for _, s := range senders {
		m[s.Channel()] = s
	}
	return &RouterSender{
		senders: m,
		logger:  slog.Default().With("component", "sender.router"),
	}
}

// Send despacha el mensaje al sender registrado para el canal dado.
// Si no hay sender para ese canal, loguea y retorna nil (no es error fatal).
func (r *RouterSender) Send(ctx context.Context, channel valueobject.Channel, msg service.Message) error {
	s, ok := r.senders[channel]
	if !ok {
		r.logger.WarnContext(ctx, "no hay sender registrado para el canal",
			"channel", channel,
			"to", msg.To,
		)
		return nil
	}
	return s.Send(ctx, msg)
}

// ── helpers ───────────────────────────────────────────────────────

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
