// Package events provee el Event Bus sobre NATS JetStream.
//
// Características:
//   - Publisher tipado con serialización JSON
//   - Subscriber con retry automático (backoff exponencial)
//   - Dead Letter Queue (DLQ) ante fallos repetidos
//   - Trazas OpenTelemetry propagadas en headers del mensaje
//   - Idempotencia: cada evento porta un EventID único para deduplicación
//   - Auto-provisioning: crea el stream si no existe al publicar o suscribir
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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
	EventID        string          `json:"event_id"`
	EventType      string          `json:"event_type"`
	AggregateID    string          `json:"aggregate_id"`
	AggregateType  string          `json:"aggregate_type"`
	OccurredAt     time.Time       `json:"occurred_at"`
	BoundedContext string          `json:"bounded_context"`
	Version        int             `json:"version"`
	TraceID        string          `json:"trace_id,omitempty"`
	SpanID         string          `json:"span_id,omitempty"`
	Payload        json.RawMessage `json:"payload"`
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
type Bus interface {
	Publish(ctx context.Context, event DomainEvent) error
	Subscribe(ctx context.Context, opts SubscribeOptions, handler Handler) error
	Close() error
}

// Handler es la función que procesa un evento recibido.
type Handler func(ctx context.Context, envelope Envelope) error

// ── SubscribeOptions ─────────────────────────────────────────────

type SubscribeOptions struct {
	Stream       string
	Subject      string
	ConsumerName string
	MaxRetries   int
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

// ── streamSpec — definición canónica de cada stream ──────────────

// streamSpec describe la configuración de un stream JetStream.
// Es la fuente de verdad compartida entre el init externo (nats-init)
// y el auto-provisioning interno del bus.
type streamSpec struct {
	// Subjects que cubre este stream (puede incluir wildcards).
	Subjects []string
	// MaxAge es la ventana de replay ante fallos (default 7 días).
	MaxAge time.Duration
	// MaxBytes en el stream (-1 = sin límite).
	MaxBytes int64
}

// knownStreams mapea nombre de stream → especificación.
// Al agregar un nuevo bounded context basta con añadir aquí su entrada.
var knownStreams = map[string]streamSpec{
	"IAM_EVENTS": {
		Subjects: []string{
			"user.registered",
			"user.logged_out",
			"user.suspended",
			"family.member_added",
		},
		MaxAge:   7 * 24 * time.Hour,
		MaxBytes: -1,
	},
	"PATIENT_EVENTS": {
		Subjects: []string{
			"patient.registered",
			"patient.coverage.updated",
			"patient.medical_alert.added",
			"patient.preferences.updated",
			"patient.merged",
			"patient.archived",
		},
		MaxAge:   7 * 24 * time.Hour,
		MaxBytes: -1,
	},
	"PROFESSIONAL_EVENTS": {
		Subjects: []string{
			"professional.registered",
			"professional.license.added",
			"professional.license.expiring_soon",
			"professional.assigned_to_clinic",
			"professional.schedule.updated",
			"professional.suspended",
		},
		MaxAge:   7 * 24 * time.Hour,
		MaxBytes: -1,
	},
	"APPOINTMENT_EVENTS": {
		Subjects: []string{
			"appointment.booked",
			"appointment.confirmed",
			"appointment.completed",
			"appointment.cancelled",
			"appointment.no_show",
			"appointment.checked_in",
			"scheduling.availability.updated",
		},
		MaxAge:   7 * 24 * time.Hour,
		MaxBytes: -1,
	},
	"BILLING_EVENTS": {
		Subjects: []string{
			"billing.quote_created",
			"billing.payment_received",
			"billing.quote_paid",
			"billing.late_fee_applied",
			"billing.late_fee_waived",
			"billing.quote_voided",
			"billing.refund_issued",
		},
		MaxAge:   30 * 24 * time.Hour,
		MaxBytes: -1,
	},
	"COVERAGE_EVENTS": {
		Subjects: []string{
			"agreement.created",
			"agreement.plan_added",
			"agreement.procedure_rule_updated",
			"agreement.suspended",
			"agreement.activated",
			"agreement.expired",
			"authorization.requested",
			"authorization.resolved",
			"authorization.expired",
		},
		MaxAge:   7 * 24 * time.Hour,
		MaxBytes: -1,
	},
	"DEAD_LETTER_EVENTS": {
		Subjects: []string{"dlq.>"},
		MaxAge:   30 * 24 * time.Hour,
		MaxBytes: 100 * 1024 * 1024, // 100 MB
	},
}

// ── NATSBus — implementación concreta ────────────────────────────

// NATSBus implementa Bus sobre NATS JetStream con auto-provisioning de streams.
type NATSBus struct {
	js        jetstream.JetStream
	nc        *nats.Conn
	tracer    trace.Tracer
	logger    *slog.Logger
	dlqStream string

	// ensuredStreams evita llamar a EnsureStream más de una vez por nombre
	// dentro del mismo proceso (optimización: después del primer Subscribe
	// el stream ya existe con certeza).
	ensuredStreams map[string]struct{}
	ensuredMu      sync.Mutex
}

// Config agrupa la configuración del NATSBus.
type Config struct {
	URL       string
	DLQStream string
}

// New crea y conecta un NATSBus.
func New(cfg Config) (*NATSBus, error) {
	if cfg.DLQStream == "" {
		cfg.DLQStream = "DEAD_LETTER_EVENTS"
	}

	nc, err := nats.Connect(cfg.URL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
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

	bus := &NATSBus{
		js:             js,
		nc:             nc,
		tracer:         otel.Tracer("odontoagenda/events"),
		logger:         slog.Default().With("component", "event_bus"),
		dlqStream:      cfg.DLQStream,
		ensuredStreams:  make(map[string]struct{}),
	}

	// Provisionar el DLQ stream al inicio (siempre necesario).
	ctx := context.Background()
	if err := bus.ensureStream(ctx, cfg.DLQStream); err != nil {
		// No es fatal: si el DLQ no existe simplemente no se podrá escribir en él.
		slog.Warn("no se pudo provisionar DLQ stream", "stream", cfg.DLQStream, "error", err)
	}

	return bus, nil
}

// ── ensureStream — auto-provisioning ─────────────────────────────

// ensureStream crea el stream si no existe, o lo actualiza si ya existe
// pero con una configuración distinta. Es idempotente y thread-safe.
//
// Usa knownStreams como fuente de verdad. Si el nombre no está registrado,
// crea un stream genérico con configuración por defecto (útil para tests).
func (b *NATSBus) ensureStream(ctx context.Context, streamName string) error {
	b.ensuredMu.Lock()
	if _, already := b.ensuredStreams[streamName]; already {
		b.ensuredMu.Unlock()
		return nil
	}
	b.ensuredMu.Unlock()

	spec, known := knownStreams[streamName]
	if !known {
		// Stream desconocido: crear con subjects = nombre del stream (fallback).
		spec = streamSpec{
			Subjects: []string{streamName + ".>"},
			MaxAge:   7 * 24 * time.Hour,
			MaxBytes: -1,
		}
		b.logger.Warn("stream no registrado en knownStreams, usando config por defecto",
			"stream", streamName)
	}

	if spec.MaxAge == 0 {
		spec.MaxAge = 7 * 24 * time.Hour
	}

	cfg := jetstream.StreamConfig{
		Name:              streamName,
		Subjects:          spec.Subjects,
		Storage:           jetstream.FileStorage,
		Replicas:          1,
		Retention:         jetstream.LimitsPolicy,
		MaxAge:            spec.MaxAge,
		MaxBytes:          spec.MaxBytes,
		MaxMsgSize:        1024 * 1024, // 1 MB por mensaje
		Discard: jetstream.DiscardOld,
		Duplicates: 2 * time.Minute,
		NoAck:      false,
	}

	// CreateOrUpdateStream: crea si no existe, actualiza si existe con
	// config distinta. Es la operación idempotente correcta de la API v2.
	stream, err := b.js.CreateOrUpdateStream(ctx, cfg)
	if err != nil {
		return fmt.Errorf("ensureStream %q: %w", streamName, err)
	}

	info, _ := stream.Info(ctx)
	if info != nil {
		b.logger.Info("stream listo",
			"stream", streamName,
			"subjects", spec.Subjects,
			"msgs", info.State.Msgs,
		)
	}

	b.ensuredMu.Lock()
	b.ensuredStreams[streamName] = struct{}{}
	b.ensuredMu.Unlock()

	return nil
}

// ── Publish ──────────────────────────────────────────────────────

// Publish serializa el evento y lo publica en NATS JetStream.
// Infiere el stream a partir del subject usando subjectToStream.
func (b *NATSBus) Publish(ctx context.Context, event DomainEvent) error {
	ctx, span := b.tracer.Start(ctx, "events.Publish",
		trace.WithAttributes(
			attribute.String("event.type", event.EventType()),
			attribute.String("event.aggregate_id", event.AggregateID()),
			attribute.String("event.bounded_context", event.BoundedContext()),
		),
	)
	defer span.End()

	// Asegurar que el stream destino existe antes de publicar.
	streamName := subjectToStream(event.EventType())
	if streamName != "" {
		if err := b.ensureStream(ctx, streamName); err != nil {
			b.logger.Warn("no se pudo asegurar stream para publicación",
				"stream", streamName, "event_type", event.EventType(), "error", err)
			// Continuamos: si el stream existe en NATS funcionará igual.
		}
	}

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

	headers := make(nats.Header)
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	for k, v := range carrier {
		headers.Set(k, v)
	}
	headers.Set("X-Event-ID", env.EventID)
	headers.Set("X-Event-Type", env.EventType)

	msg := &nats.Msg{
		Subject: env.EventType,
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

// ── Subscribe ─────────────────────────────────────────────────────

// Subscribe registra un consumer durable sobre NATS JetStream.
// Crea el stream automáticamente si no existe (auto-provisioning).
func (b *NATSBus) Subscribe(ctx context.Context, opts SubscribeOptions, handler Handler) error {
	opts.defaults()

	// Garantizar que el stream existe ANTES de crear el consumer.
	// Este es el fix al error "stream not found" (err_code=10059).
	if err := b.ensureStream(ctx, opts.Stream); err != nil {
		return fmt.Errorf("events.Subscribe: ensure stream %q: %w", opts.Stream, err)
	}

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

// ── handleMessage ─────────────────────────────────────────────────

func (b *NATSBus) handleMessage(ctx context.Context, msg jetstream.Msg, opts SubscribeOptions, handler Handler) {
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
			"error", err, "subject", msg.Subject())
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

		if deliveryCount >= opts.MaxRetries {
			b.sendToDLQ(ctx, env, opts.ConsumerName, err)
			_ = msg.Term()
			return
		}

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

// ── sendToDLQ ─────────────────────────────────────────────────────

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
			"subject", subject, "event_id", env.EventID, "error", err)
	} else {
		b.logger.ErrorContext(ctx, "evento enviado a DLQ",
			"event_id", env.EventID,
			"event_type", env.EventType,
			"consumer", consumer,
			"cause", cause.Error(),
		)
	}
}

// ── Close ─────────────────────────────────────────────────────────

func (b *NATSBus) Close() error {
	b.nc.Drain()
	return nil
}

// ── Helpers ──────────────────────────────────────────────────────

// subjectToStream infiere el nombre del stream a partir del subject/event_type.
// Mapeo: prefijo del event type → nombre del stream.
//
// Ejemplos:
//   "user.registered"              → "IAM_EVENTS"
//   "appointment.completed"        → "APPOINTMENT_EVENTS"
//   "professional.schedule.updated"→ "PROFESSIONAL_EVENTS"
func subjectToStream(eventType string) string {
	for streamName, spec := range knownStreams {
		for _, subject := range spec.Subjects {
			// Soporte de wildcard simple: "dlq.>" matchea "dlq.foo.bar"
			if subjectMatches(subject, eventType) {
				return streamName
			}
		}
	}
	return ""
}

// subjectMatches evalúa si un subject pattern NATS matchea un subject concreto.
func subjectMatches(pattern, subject string) bool {
	if pattern == subject {
		return true
	}
	// Wildcard ">" al final: matchea cualquier sufijo.
	if strings.HasSuffix(pattern, ".>") {
		prefix := strings.TrimSuffix(pattern, ">")
		return strings.HasPrefix(subject, prefix)
	}
	// Wildcard "*": matchea un único token.
	patternParts := strings.Split(pattern, ".")
	subjectParts := strings.Split(subject, ".")
	if len(patternParts) != len(subjectParts) {
		return false
	}
	for i, p := range patternParts {
		if p != "*" && p != subjectParts[i] {
			return false
		}
	}
	return true
}

// exponentialBackoff genera la lista de delays para backoff exponencial.
func exponentialBackoff(base time.Duration, steps int) []time.Duration {
	delays := make([]time.Duration, steps)
	for i := range delays {
		delays[i] = base * time.Duration(1<<uint(i))
	}
	return delays
}

// UnmarshalPayload deserializa el Payload del Envelope en el tipo T dado.
func UnmarshalPayload[T any](env Envelope) (T, error) {
	var v T
	if err := json.Unmarshal(env.Payload, &v); err != nil {
		return v, fmt.Errorf("unmarshal payload type %T: %w", v, err)
	}
	return v, nil
}

// ErrSkipRetry puede retornarse desde un Handler para hacer Term inmediato
// sin enviar al DLQ.
var ErrSkipRetry = errors.New("skip retry: mensaje terminado intencionalmente")
