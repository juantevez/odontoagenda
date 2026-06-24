// Package http — webhook de MercadoPago con verificación HMAC.
//
// MP envía notificaciones a POST /api/v1/billing/webhooks/mercadopago
// con el header X-Signature: ts=<timestamp>,v1=<hmac_sha256>
//
// Verificación:
//   manifest = "id:<notification_id>;request-id:<x_request_id>;ts:<ts>"
//   expected = HMAC-SHA256(webhookSecret, manifest)
//   Rechazar si ts > now + 5 min (anti-replay).
package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	billingcmd "github.com/juantevez/odontoagenda/context/billing/application/command"
	"github.com/juantevez/odontoagenda/context/billing/infrastructure/payment"
)

// ── WebhookHandler ────────────────────────────────────────────────

type WebhookHandler struct {
	webhookSecret    string
	mpAdapter        *payment.MercadoPagoAdapter
	registerPayment  *billingcmd.RegisterPaymentHandler
	confirmPayment   *billingcmd.ConfirmPaymentHandler
	logger           *slog.Logger
}

func NewWebhookHandler(
	webhookSecret string,
	mpAdapter *payment.MercadoPagoAdapter,
	registerPayment *billingcmd.RegisterPaymentHandler,
	confirmPayment *billingcmd.ConfirmPaymentHandler,
) *WebhookHandler {
	return &WebhookHandler{
		webhookSecret:   webhookSecret,
		mpAdapter:       mpAdapter,
		registerPayment: registerPayment,
		confirmPayment:  confirmPayment,
		logger:          slog.Default().With("adapter", "billing.webhook.mercadopago"),
	}
}

// ── Payload de notificación de MP ─────────────────────────────────

type mpWebhookPayload struct {
	ID       int64  `json:"id"`
	Action   string `json:"action"` // "payment.created" | "payment.updated"
	Type     string `json:"type"`   // "payment"
	Data     struct {
		ID string `json:"id"` // ID del pago en MP
	} `json:"data"`
	DateCreated string `json:"date_created"`
	LiveMode    bool   `json:"live_mode"`
}

// ── POST /api/v1/billing/webhooks/mercadopago ─────────────────────

// HandleMercadoPago procesa las notificaciones de MercadoPago.
// No requiere JWT — la autenticación se hace via HMAC.
func (h *WebhookHandler) HandleMercadoPago(w http.ResponseWriter, r *http.Request) {
	// Leer body completo para la verificación HMAC.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // max 1 MB
	if err != nil {
		h.logger.Error("error leyendo body del webhook", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Verificar firma HMAC antes de procesar cualquier dato.
	if err := h.verifySignature(r, body); err != nil {
		h.logger.Warn("firma HMAC inválida en webhook MP",
			"error", err,
			"x_signature", r.Header.Get("X-Signature"),
			"remote_addr", r.RemoteAddr,
		)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var payload mpWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		h.logger.Error("error deserializando webhook MP", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Solo procesamos eventos de tipo "payment".
	if payload.Type != "payment" {
		w.WriteHeader(http.StatusOK) // ACK para otros tipos
		return
	}

	mpPaymentID := payload.Data.ID
	if mpPaymentID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Consultar el estado real del pago en MP (no confiar solo en el webhook).
	mpStatus, err := h.mpAdapter.GetPayment(r.Context(), mpPaymentID)
	if err != nil {
		h.logger.Error("error consultando pago en MP",
			"mp_payment_id", mpPaymentID, "error", err)
		// Retornar 200 para que MP no reintente — lo revisaremos manualmente.
		w.WriteHeader(http.StatusOK)
		return
	}

	h.logger.Info("webhook MP recibido",
		"mp_payment_id", mpPaymentID,
		"status", mpStatus.Status,
		"external_ref", mpStatus.ExternalRef,
		"amount_cents", mpStatus.AmountCents,
	)

	switch mpStatus.Status {
	case "approved":
		h.handleApproved(r, mpStatus)
	case "rejected", "cancelled":
		h.handleRejectedOrCancelled(r, mpStatus)
	default:
		// pending, in_process, etc.: no hacemos nada todavía.
		h.logger.Info("pago MP en estado intermedio, ignorando",
			"status", mpStatus.Status, "mp_payment_id", mpPaymentID)
	}

	// Siempre retornar 200 para que MP no reintente.
	w.WriteHeader(http.StatusOK)
}

// handleApproved confirma el Payment en el Quote.
func (h *WebhookHandler) handleApproved(r *http.Request, mpStatus payment.MPPaymentStatus) {
	// El externalReference es el quoteID que pusimos al crear la Preference.
	quoteID, err := uuid.Parse(mpStatus.ExternalRef)
	if err != nil {
		h.logger.Error("external_reference no es un UUID válido",
			"external_ref", mpStatus.ExternalRef)
		return
	}

	if err := h.confirmPayment.Handle(r.Context(), billingcmd.ConfirmPaymentCommand{
		QuoteID:           quoteID,
		MPPaymentID:       mpStatus.ID,
		AmountCents:       mpStatus.AmountCents,
		ExternalReference: mpStatus.ID,
	}); err != nil {
		h.logger.Error("error confirmando pago MP en Quote",
			"quote_id", quoteID,
			"mp_payment_id", mpStatus.ID,
			"error", err,
		)
	}
}

// handleRejectedOrCancelled marca el Payment como fallido.
func (h *WebhookHandler) handleRejectedOrCancelled(r *http.Request, mpStatus payment.MPPaymentStatus) {
	quoteID, err := uuid.Parse(mpStatus.ExternalRef)
	if err != nil {
		return
	}
	h.logger.Warn("pago MP rechazado o cancelado",
		"quote_id", quoteID,
		"mp_payment_id", mpStatus.ID,
		"status", mpStatus.Status,
		"detail", mpStatus.StatusDetail,
	)
	// En Fase 7 se agregará la lógica de marcar el Payment como Failed.
	// Por ahora solo logueamos; el Quote queda en Confirmed para reintento.
}

// ── Verificación HMAC ─────────────────────────────────────────────

// verifySignature verifica la firma HMAC-SHA256 del webhook de MP.
//
// Header X-Signature formato: ts=<unix_timestamp>,v1=<hex_hmac>
// Manifest: "id:<notification_id>;request-id:<x_request_id>;ts:<ts>"
// HMAC: SHA256(webhookSecret, manifest)
// Anti-replay: rechazar si ts es más de 5 minutos viejo.
func (h *WebhookHandler) verifySignature(r *http.Request, _ []byte) error {
	// En modo sin secret configurado, saltear la verificación (desarrollo).
	if h.webhookSecret == "" {
		h.logger.Warn("MP_WEBHOOK_SECRET no configurado, saltando verificación HMAC (solo desarrollo)")
		return nil
	}

	xSignature := r.Header.Get("X-Signature")
	if xSignature == "" {
		return fmt.Errorf("header X-Signature ausente")
	}

	// Parsear ts y v1 del header.
	ts, v1, err := parseXSignature(xSignature)
	if err != nil {
		return fmt.Errorf("X-Signature malformado: %w", err)
	}

	// Anti-replay: el timestamp no puede ser más de 5 minutos viejo.
	signedAt := time.Unix(ts, 0)
	if time.Since(signedAt) > 5*time.Minute {
		return fmt.Errorf("timestamp del webhook expirado: %s", signedAt)
	}

	// Obtener notification_id y request_id de la URL y headers.
	notificationID := r.URL.Query().Get("id")
	requestID := r.Header.Get("X-Request-Id")

	// Construir el manifest exacto que MP firmó.
	manifest := fmt.Sprintf("id:%s;request-id:%s;ts:%d",
		notificationID, requestID, ts)

	// Calcular el HMAC esperado.
	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write([]byte(manifest))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(v1)) {
		return fmt.Errorf("firma HMAC no coincide")
	}
	return nil
}

// parseXSignature extrae ts y v1 del header X-Signature de MP.
// Formato esperado: "ts=1234567890,v1=abc123..."
func parseXSignature(header string) (ts int64, v1 string, err error) {
	parts := strings.Split(header, ",")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "ts":
			ts, err = strconv.ParseInt(kv[1], 10, 64)
			if err != nil {
				return 0, "", fmt.Errorf("ts inválido: %w", err)
			}
		case "v1":
			v1 = kv[1]
		}
	}
	if ts == 0 || v1 == "" {
		return 0, "", fmt.Errorf("ts o v1 ausentes en X-Signature")
	}
	return ts, v1, nil
}
