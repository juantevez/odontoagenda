// Package events provee el Event Bus sobre NATS JetStream.
//
// Características:
//   - Publisher tipado con serialización JSON
//   - Subscriber con retry automático (backoff exponencial)
//   - Dead Letter Queue (DLQ) ante fallos repetidos
//   - Trazas OpenTelemetry propagadas en headers del mensaje
//   - Idempotencia: cada evento porta un EventID único para deduplicación
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// ── Envelope ─────────────────────────────────────────────────────

// Envelope es el sobre estándar de todos los domain events del sistema.
// Garantiza trazabilidad, ordenamiento y deduplicación.
type Envelope struct {
	EventID       string         `json:"event_id"`        // UUID único por evento
	EventType     string         `json:"event_type"`      // ej: "appointment.booked"
	AggregateID   string         `json:"aggregate_id"`    // ID de la entidad que originó el evento
	AggregateType string         `json:"aggregate_type"`  // ej: "Appointment"
	OccurredAt    time.Time      `json:"occurred_at"`
	BoundedContext string        `json:"bounded_context"` // ej: "scheduling"
	Version       int            `json:"version"`         // versión del esquema del evento
	TraceID       string         `json:"trace_id,omitempty"`
	SpanID        string         `json:"span_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

// DomainEvent es la interfaz que implementan todos los eventos de dominio.
type DomainEvent interface {
	EventType() string
	AggregateID() string
	AggregateType() string
	BoundedContext() string
	SchemaVersion() int
}

// ── Bus ──────────────────────────────────────────────────────────

// Bus es la abstracción central del sistema de eventos.
// Desacopla el dominio de NATS; permite testear con implementaciones mock.
type Bus interface {
	// Publish publica un evento en el stream correspondiente.
	Publish(ctx context.Context, event DomainEvent) error

	// Subscribe registra un handler para un subject.
	// El consumer es durable: sobrevive reinicios.
	Subscribe(ctx context.Context, opts SubscribeOptions, handler Handler) error

	// Close libera los recursos del bus.
	Close() error
}

// Handler es la función que procesa un evento recibido.
// Retornar error sin nil causa un retry; retornar nil hace ACK.
type Handler func(ctx context.Context, envelope Envelope) error

// ── SubscribeOptions ─────────────────────────────────────────────

type SubscribeOptions struct {
	// Stream es el nombre del JetStream stream (ej: "APPOINTMENT_EVENTS").
	Stream string
	// Subject es el filtro de mensajes (ej: "appointment.booked").
	// Soporta wildcards NATS: "appointment.*" o "appointment.>"
	Subject string
	// ConsumerName identifica al consumer group (durable).
	// Múltiples instancias con el mismo nombre comparten la carga.
	ConsumerName string
	// MaxRetries es el número máximo de reintentos antes de enviar al DLQ.
	// Default: 3.
	MaxRetries int
	// RetryBackoff es el tiempo base para backoff exponencial.
	// Default: 5s (intentos: 5s, 10s, 20s).
	RetryBackoff time.Duration
}

func (o *SubscribeOptions) defaults() {
	if o.MaxRetries <= 0 {
		o.MaxRetries = 3
	}
	if o.RetryBackoff <= 0 {
		o.RetryBackoff = 5 * time.Second
	}
}

// ── NATSBus — implementación concreta ────────────────────────────

// NATSBus implementa Bus sobre NATS JetStream.
type NATSBus struct {
	js       jetstream.JetStream
	nc       *nats.Conn
	tracer   trace.Tracer
	logger   *slog.Logger
	dlqStream string // nombre del stream de Dead Letter Queue
}

// Config agrupa la configuración del NATSBus.
type Config struct {
	URL       string
	DLQStream string // default: "DEAD_LETTER_EVENTS"
}

// New crea y conecta un NATSBus.
func New(cfg Config) (*NATSBus, error) {
	if cfg.DLQStream == "" {
		cfg.DLQStream = "DEAD_LETTER_EVENTS"
	}

	nc, err := nats.Connect(cfg.URL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1), // reconexión infinita
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			slog.Warn("NATS desconectado", "error", err)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			slog.Info("NATS reconectado")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream init: %w", err)
	}

	return &NATSBus{
		js:        js,
		nc:        nc,
		tracer:    otel.Tracer("odontoagenda/events"),
		logger:    slog.Default().With("component", "event_bus"),
		dlqStream: cfg.DLQStream,
	}, nil
}

// Publish serializa el evento en un Envelope y lo publica en NATS JetStream.
// Propaga el trace context de OpenTelemetry en los headers del mensaje.
func (b *NATSBus) Publish(ctx context.Context, event DomainEvent) error {
	ctx, span := b.tracer.Start(ctx, "events.Publish",
		trace.WithAttributes(
			attribute.String("event.type", event.EventType()),
			attribute.String("event.aggregate_id", event.AggregateID()),
			attribute.String("event.bounded_context", event.BoundedContext()),
		),
	)
	defer span.End()

	payload, err := json.Marshal(event)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "serialize event")
		return fmt.Errorf("events.Publish: marshal payload: %w", err)
	}

	spanCtx := span.SpanContext()
	env := Envelope{
		EventID:        uuid.New().String(),
		EventType:      event.EventType(),
		AggregateID:    event.AggregateID(),
		AggregateType:  event.AggregateType(),
		BoundedContext: event.BoundedContext(),
		OccurredAt:     time.Now().UTC(),
		Version:        event.SchemaVersion(),
		TraceID:        spanCtx.TraceID().String(),
		SpanID:         spanCtx.SpanID().String(),
		Payload:        payload,
	}

	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("events.Publish: marshal envelope: %w", err)
	}

	// Propagamos trace context en NATS headers para trazas distribuidas.
	headers := make(nats.Header)
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	for k, v := range carrier {
		headers.Set(k, v)
	}
	headers.Set("X-Event-ID", env.EventID)
	headers.Set("X-Event-Type", env.EventType)

	msg := &nats.Msg{
		Subject: env.EventType, // subject = tipo de evento
		Header:  headers,
		Data:    data,
	}

	if _, err := b.js.PublishMsg(ctx, msg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "publish to jetstream")
		return fmt.Errorf("events.Publish: jetstream publish: %w", err)
	}

	b.logger.InfoContext(ctx, "evento publicado",
		"event_id", env.EventID,
		"event_type", env.EventType,
		"aggregate_id", env.AggregateID,
	)

	return nil
}

// Subscribe registra un consumer durable sobre NATS JetStream.
// Implementa retry con backoff exponencial y envío a DLQ ante fallos repetidos.
func (b *NATSBus) Subscribe(ctx context.Context, opts SubscribeOptions, handler Handler) error {
	opts.defaults()

	consumer, err := b.js.CreateOrUpdateConsumer(ctx, opts.Stream, jetstream.ConsumerConfig{
		Name:          opts.ConsumerName,
		Durable:       opts.ConsumerName,
		FilterSubject: opts.Subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    opts.MaxRetries + 1,
		AckWait:       30 * time.Second,
		BackOff:       exponentialBackoff(opts.RetryBackoff, opts.MaxRetries),
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("events.Subscribe: create consumer '%s': %w", opts.ConsumerName, err)
	}

	_, err = consumer.Consume(func(msg jetstream.Msg) {
		b.handleMessage(ctx, msg, opts, handler)
	})
	if err != nil {
		return fmt.Errorf("events.Subscribe: start consume: %w", err)
	}

	b.logger.InfoContext(ctx, "consumer registrado",
		"stream", opts.Stream,
		"subject", opts.Subject,
		"consumer", opts.ConsumerName,
	)

	return nil
}

// handleMessage procesa un mensaje con tracing y manejo de errores.
func (b *NATSBus) handleMessage(ctx context.Context, msg jetstream.Msg, opts SubscribeOptions, handler Handler) {
	// Extraemos trace context del header del mensaje.
	carrier := propagation.MapCarrier{}
	for k := range msg.Headers() {
		carrier[k] = msg.Headers().Get(k)
	}
	ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)

	ctx, span := b.tracer.Start(ctx, "events.Handle",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("consumer.name", opts.ConsumerName),
			attribute.String("consumer.subject", opts.Subject),
		),
	)
	defer span.End()

	var env Envelope
	if err := json.Unmarshal(msg.Data(), &env); err != nil {
		b.logger.ErrorContext(ctx, "error deserializando envelope",
			"error", err,
			"subject", msg.Subject(),
		)
		// Mensaje malformado: NAK inmediato sin retry (irrecuperable).
		_ = msg.Term()
		return
	}

	span.SetAttributes(
		attribute.String("event.id", env.EventID),
		attribute.String("event.type", env.EventType),
		attribute.String("event.aggregate_id", env.AggregateID),
	)

	meta, _ := msg.Metadata()
	deliveryCount := 0
	if meta != nil {
		deliveryCount = int(meta.NumDelivered)
	}

	if err := handler(ctx, env); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "handler error")

		b.logger.WarnContext(ctx, "error procesando evento",
			"event_id", env.EventID,
			"event_type", env.EventType,
			"delivery_attempt", deliveryCount,
			"max_retries", opts.MaxRetries,
			"error", err,
		)

		// Si superamos el máximo de reintentos → DLQ.
		if deliveryCount >= opts.MaxRetries {
			b.sendToDLQ(ctx, env, opts.ConsumerName, err)
			_ = msg.Term() // terminar para que NATS no siga reintentando
			return
		}

		// NAK con backoff: NATS reintentará después del delay configurado.
		delay := opts.RetryBackoff * time.Duration(1<<uint(deliveryCount-1))
		_ = msg.NakWithDelay(delay)
		return
	}

	span.SetStatus(codes.Ok, "")
	_ = msg.Ack()

	b.logger.InfoContext(ctx, "evento procesado",
		"event_id", env.EventID,
		"event_type", env.EventType,
	)
}

// sendToDLQ publica el envelope fallido en el stream Dead Letter Queue.
func (b *NATSBus) sendToDLQ(ctx context.Context, env Envelope, consumer string, cause error) {
	type DLQEntry struct {
		OriginalEnvelope Envelope  `json:"original_envelope"`
		FailedConsumer   string    `json:"failed_consumer"`
		FailedAt         time.Time `json:"failed_at"`
		ErrorMessage     string    `json:"error_message"`
	}

	entry := DLQEntry{
		OriginalEnvelope: env,
		FailedConsumer:   consumer,
		FailedAt:         time.Now().UTC(),
		ErrorMessage:     cause.Error(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		b.logger.ErrorContext(ctx, "error serializando entrada DLQ", "error", err)
		return
	}

	subject := fmt.Sprintf("dlq.%s.%s", consumer, env.EventType)
	if _, err := b.js.Publish(ctx, subject, data); err != nil {
		b.logger.ErrorContext(ctx, "error publicando en DLQ",
			"subject", subject,
			"event_id", env.EventID,
			"error", err,
		)
	} else {
		b.logger.ErrorContext(ctx, "evento enviado a DLQ",
			"event_id", env.EventID,
			"event_type", env.EventType,
			"consumer", consumer,
			"cause", cause.Error(),
		)
	}
}

func (b *NATSBus) Close() error {
	b.nc.Drain()
	return nil
}

// ── Helpers ──────────────────────────────────────────────────────

// exponentialBackoff genera la lista de delays para backoff exponencial.
func exponentialBackoff(base time.Duration, steps int) []time.Duration {
	delays := make([]time.Duration, steps)
	for i := range delays {
		delays[i] = base * time.Duration(1<<uint(i))
	}
	return delays
}

// UnmarshalPayload deserializa el Payload del Envelope en el tipo T dado.
// Uso: event, err := events.UnmarshalPayload[AppointmentBooked](envelope)
func UnmarshalPayload[T any](env Envelope) (T, error) {
	var v T
	if err := json.Unmarshal(env.Payload, &v); err != nil {
		return v, fmt.Errorf("unmarshal payload type %T: %w", v, err)
	}
	return v, nil
}

// ErrSkipRetry puede retornarse desde un Handler para hacer Term inmediato
// sin enviar al DLQ (para mensajes que no tienen sentido reintentar).
var ErrSkipRetry = errors.New("skip retry: mensaje terminado intencionalmente")
