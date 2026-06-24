// Package payment — adaptador real de MercadoPago (Checkout Pro por clínica).
//
// Modelo: Checkout Pro por clínica.
//   - Cada clínica tiene su propio mp_access_token.
//   - Billing genera una Preference con el monto del copago.
//   - MP redirige al paciente al link de pago (init_point).
//   - Al completar el pago, MP notifica via webhook POST /billing/webhooks/mercadopago.
//   - Billing verifica la firma HMAC y confirma el Payment en el Quote.
//
// Referencias:
//   https://www.mercadopago.com.ar/developers/es/docs/checkout-pro/integrate-checkout-pro
//   https://www.mercadopago.com.ar/developers/es/docs/your-integrations/notifications/webhooks
package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const (
	mpBaseURL         = "https://api.mercadopago.com"
	mpPreferencesPath = "/checkout/preferences"
	mpPaymentsPath    = "/v1/payments"
	mpRefundsPath     = "/v1/payments/%s/refunds"
	mpHTTPTimeout     = 10 * time.Second
)

// ── MercadoPagoAdapter — adaptador real ──────────────────────────

// MercadoPagoAdapter implementa el flujo Checkout Pro de MercadoPago.
// Cada instancia está asociada a UN access token (una clínica).
// En el wire.go se crea una instancia por clínica, o una global si todas
// comparten el mismo token (MVP simplificado).
type MercadoPagoAdapter struct {
	accessToken string
	httpClient  *http.Client
	logger      *slog.Logger
}

func NewMercadoPagoAdapter(accessToken string) *MercadoPagoAdapter {
	return &MercadoPagoAdapter{
		accessToken: accessToken,
		httpClient:  &http.Client{Timeout: mpHTTPTimeout},
		logger:      slog.Default().With("component", "payment.mercadopago"),
	}
}

// ── CreatePreference ──────────────────────────────────────────────

// PreferenceRequest agrupa los datos necesarios para crear una Preference en MP.
type PreferenceRequest struct {
	QuoteID       string
	PatientName   string
	ProcedureDesc string
	AmountCents   int64  // copago del paciente en centavos ARS
	ExternalRef   string // quoteID para correlacionar el webhook
	BackURLSuccess string
	BackURLFailure string
	BackURLPending string
	WebhookURL    string // URL donde MP enviará las notificaciones
}

// PreferenceResponse contiene el resultado de crear la Preference.
type PreferenceResponse struct {
	PreferenceID string
	InitPoint    string // URL de pago para redirigir al paciente
	SandboxURL   string // URL de sandbox para testing
}

// mpPreferenceBody es el cuerpo de la request a la API de MP.
type mpPreferenceBody struct {
	Items           []mpItem        `json:"items"`
	ExternalReference string        `json:"external_reference"`
	BackURLs        mpBackURLs      `json:"back_urls"`
	AutoReturn      string          `json:"auto_return"`
	NotificationURL string          `json:"notification_url,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
}

type mpItem struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"` // MP usa float con 2 decimales
	CurrencyID  string  `json:"currency_id"`
}

type mpBackURLs struct {
	Success string `json:"success"`
	Failure string `json:"failure"`
	Pending string `json:"pending"`
}

type mpPreferenceResponse struct {
	ID         string `json:"id"`
	InitPoint  string `json:"init_point"`
	SandboxInitPoint string `json:"sandbox_init_point"`
}

// CreatePreference crea una Preference en MP y retorna el link de pago.
func (a *MercadoPagoAdapter) CreatePreference(ctx context.Context, req PreferenceRequest) (PreferenceResponse, error) {
	// MP trabaja con pesos enteros o float con 2 decimales.
	// Convertimos centavos → pesos con 2 decimales.
	unitPrice := float64(req.AmountCents) / 100.0

	body := mpPreferenceBody{
		Items: []mpItem{
			{
				ID:         req.QuoteID,
				Title:      req.ProcedureDesc,
				Quantity:   1,
				UnitPrice:  unitPrice,
				CurrencyID: "ARS",
			},
		},
		ExternalReference: req.ExternalRef,
		BackURLs: mpBackURLs{
			Success: req.BackURLSuccess,
			Failure: req.BackURLFailure,
			Pending: req.BackURLPending,
		},
		AutoReturn:      "approved",
		NotificationURL: req.WebhookURL,
		Metadata: map[string]any{
			"quote_id":     req.QuoteID,
			"patient_name": req.PatientName,
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return PreferenceResponse{}, fmt.Errorf("MercadoPago.CreatePreference: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		mpBaseURL+mpPreferencesPath, bytes.NewReader(data))
	if err != nil {
		return PreferenceResponse{}, fmt.Errorf("MercadoPago.CreatePreference: build request: %w", err)
	}
	a.setHeaders(httpReq)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return PreferenceResponse{}, fmt.Errorf("MercadoPago.CreatePreference: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return PreferenceResponse{}, fmt.Errorf("MercadoPago.CreatePreference: status %d: %s",
			resp.StatusCode, string(body))
	}

	var mpResp mpPreferenceResponse
	if err := json.NewDecoder(resp.Body).Decode(&mpResp); err != nil {
		return PreferenceResponse{}, fmt.Errorf("MercadoPago.CreatePreference: decode: %w", err)
	}

	a.logger.InfoContext(ctx, "preference creada en MercadoPago",
		"preference_id", mpResp.ID,
		"quote_id", req.QuoteID,
		"amount", unitPrice,
	)

	return PreferenceResponse{
		PreferenceID: mpResp.ID,
		InitPoint:    mpResp.InitPoint,
		SandboxURL:   mpResp.SandboxInitPoint,
	}, nil
}

// ── GetPayment ────────────────────────────────────────────────────

// MPPaymentStatus agrupa el resultado de consultar un pago en MP.
type MPPaymentStatus struct {
	ID             string
	Status         string // approved | pending | rejected | cancelled | refunded
	StatusDetail   string
	AmountCents    int64
	ExternalRef    string
	DateApproved   *time.Time
}

type mpPaymentResponse struct {
	ID             int64   `json:"id"`
	Status         string  `json:"status"`
	StatusDetail   string  `json:"status_detail"`
	TransactionAmt float64 `json:"transaction_amount"`
	ExternalRef    string  `json:"external_reference"`
	DateApproved   *string `json:"date_approved"`
}

// GetPayment consulta el estado de un pago en MP por su ID externo.
func (a *MercadoPagoAdapter) GetPayment(ctx context.Context, mpPaymentID string) (MPPaymentStatus, error) {
	url := fmt.Sprintf("%s%s/%s", mpBaseURL, mpPaymentsPath, mpPaymentID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return MPPaymentStatus{}, fmt.Errorf("MercadoPago.GetPayment: build request: %w", err)
	}
	a.setHeaders(httpReq)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return MPPaymentStatus{}, fmt.Errorf("MercadoPago.GetPayment: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return MPPaymentStatus{}, fmt.Errorf("MercadoPago.GetPayment: status %d", resp.StatusCode)
	}

	var mpResp mpPaymentResponse
	if err := json.NewDecoder(resp.Body).Decode(&mpResp); err != nil {
		return MPPaymentStatus{}, fmt.Errorf("MercadoPago.GetPayment: decode: %w", err)
	}

	result := MPPaymentStatus{
		ID:           fmt.Sprintf("%d", mpResp.ID),
		Status:       mpResp.Status,
		StatusDetail: mpResp.StatusDetail,
		AmountCents:  int64(mpResp.TransactionAmt * 100),
		ExternalRef:  mpResp.ExternalRef,
	}

	if mpResp.DateApproved != nil {
		if t, err := time.Parse(time.RFC3339, *mpResp.DateApproved); err == nil {
			result.DateApproved = &t
		}
	}

	return result, nil
}

// ── RefundPayment ─────────────────────────────────────────────────

// RefundPayment emite una devolución total o parcial en MP.
// mpPaymentID es el ID del pago original en MP (el externalReference del Payment).
func (a *MercadoPagoAdapter) RefundPayment(ctx context.Context, mpPaymentID string, amountCents int64) error {
	url := fmt.Sprintf(mpBaseURL+mpRefundsPath, mpPaymentID)

	// Devolución total: body vacío. Parcial: incluir amount.
	var bodyData []byte
	if amountCents > 0 {
		amount := float64(amountCents) / 100.0
		body := map[string]float64{"amount": amount}
		var err error
		bodyData, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("MercadoPago.RefundPayment: marshal: %w", err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyData))
	if err != nil {
		return fmt.Errorf("MercadoPago.RefundPayment: build request: %w", err)
	}
	a.setHeaders(httpReq)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("MercadoPago.RefundPayment: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("MercadoPago.RefundPayment: status %d: %s",
			resp.StatusCode, string(body))
	}

	a.logger.InfoContext(ctx, "devolución emitida en MercadoPago",
		"mp_payment_id", mpPaymentID,
		"amount_cents", amountCents,
	)
	return nil
}

// ── IsSupported ───────────────────────────────────────────────────

func (a *MercadoPagoAdapter) IsSupported(method string) bool {
	return method == "MercadoPago"
}

// ── helper ────────────────────────────────────────────────────────

func (a *MercadoPagoAdapter) setHeaders(r *http.Request) {
	r.Header.Set("Authorization", "Bearer "+a.accessToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Idempotency-Key", r.Header.Get("X-Request-ID")) // reutilizamos el request ID
}
