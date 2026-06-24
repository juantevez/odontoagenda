// Package http contiene los adaptadores de entrada HTTP del bounded context Billing.
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	billingcmd "github.com/juantevez/odontoagenda/context/billing/application/command"
	billingqry "github.com/juantevez/odontoagenda/context/billing/application/query"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// RegisterRoutes monta todas las rutas del contexto Billing.
func RegisterRoutes(
	r chi.Router,
	jwtCfg middleware.JWTConfig,
	// Commands
	registerPayment  *billingcmd.RegisterPaymentHandler,
	voidQuote        *billingcmd.VoidQuoteHandler,
	waiveLateFee     *billingcmd.WaiveLateFeeHandler,
	initMPPayment    *billingcmd.InitMPPaymentHandler,
	refund           *billingcmd.RefundHandler,
	// Queries
	getQuoteByID     *billingqry.GetQuoteByIDHandler,
	getQuoteByAppt   *billingqry.GetQuoteByAppointmentHandler,
	getPatientAcct   *billingqry.GetPatientAccountHandler,
	getPatientQuotes *billingqry.GetPatientQuotesHandler,
	getDailyReport   *billingqry.GetDailyReportHandler,
	// Handlers de infraestructura
	cancellationPolicyH *CancellationPolicyHTTPHandler,
	webhookH            *WebhookHandler,
	reportH             *ReportHandler,
) {
	h := &billingHTTPHandler{
		registerPayment:     registerPayment,
		voidQuote:           voidQuote,
		waiveLateFee:        waiveLateFee,
		initMPPayment:       initMPPayment,
		refund:              refund,
		getQuoteByID:        getQuoteByID,
		getQuoteByAppt:      getQuoteByAppt,
		getPatientAcct:      getPatientAcct,
		getPatientQuotes:    getPatientQuotes,
		getDailyReport:      getDailyReport,
		logger:              slog.Default().With("adapter", "billing.http"),
	}

	// ── Webhook MP: sin JWT, con HMAC ─────────────────────────────
	r.Post("/billing/webhooks/mercadopago", webhookH.HandleMercadoPago)

	r.Group(func(r chi.Router) {
		r.Use(middleware.JWT(jwtCfg))

		// Consultas de presupuesto
		r.Get("/billing/quotes/{quoteId}", h.GetQuoteByID)
		r.Get("/billing/appointments/{appointmentId}/quote", h.GetQuoteByAppointment)

		// Estado de cuenta y historial del paciente
		r.Get("/billing/patients/{patientId}/account", h.GetPatientAccount)
		r.Get("/billing/patients/{patientId}/quotes", h.GetPatientQuotes)

		// Registro de pagos
		r.Post("/billing/quotes/{quoteId}/payments", h.RegisterPayment)

		// Inicio de pago MercadoPago
		r.Post("/billing/quotes/{quoteId}/payments/mercadopago", h.InitMPPayment)

		// Administración (solo admin)
		r.Post("/billing/quotes/{quoteId}/void",
			middleware.RequireRoles(middleware.RoleClinicAdmin, middleware.RoleSuperAdmin)(
				http.HandlerFunc(h.VoidQuote)).ServeHTTP,
		)
		r.Post("/billing/quotes/{quoteId}/refund",
			middleware.RequireRoles(middleware.RoleClinicAdmin, middleware.RoleSuperAdmin)(
				http.HandlerFunc(h.Refund)).ServeHTTP,
		)
		r.Put("/billing/late-fees/{feeId}/waive",
			middleware.RequireRoles(middleware.RoleClinicAdmin, middleware.RoleSuperAdmin)(
				http.HandlerFunc(h.WaiveLateFee)).ServeHTTP,
		)

		// Reportes
		r.Get("/billing/reports/daily", reportH.GetDailyReport)
		r.Get("/billing/reports/clinic", reportH.GetClinicReport)

		// Política de cancelación por sede
		RegisterCancellationPolicyRoutes(r, cancellationPolicyH)
	})
}

// ── Handler struct ────────────────────────────────────────────────

type billingHTTPHandler struct {
	registerPayment  *billingcmd.RegisterPaymentHandler
	voidQuote        *billingcmd.VoidQuoteHandler
	waiveLateFee     *billingcmd.WaiveLateFeeHandler
	initMPPayment    *billingcmd.InitMPPaymentHandler
	refund           *billingcmd.RefundHandler
	getQuoteByID     *billingqry.GetQuoteByIDHandler
	getQuoteByAppt   *billingqry.GetQuoteByAppointmentHandler
	getPatientAcct   *billingqry.GetPatientAccountHandler
	getPatientQuotes *billingqry.GetPatientQuotesHandler
	getDailyReport   *billingqry.GetDailyReportHandler
	logger           *slog.Logger
}

// ── GET /billing/quotes/:quoteId ──────────────────────────────────

func (h *billingHTTPHandler) GetQuoteByID(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "quoteId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "quoteId inválido")
		return
	}
	dto, err := h.getQuoteByID.Handle(r.Context(), billingqry.GetQuoteByIDQuery{QuoteID: id})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// ── GET /billing/appointments/:appointmentId/quote ────────────────

func (h *billingHTTPHandler) GetQuoteByAppointment(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "appointmentId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "appointmentId inválido")
		return
	}
	dto, err := h.getQuoteByAppt.Handle(r.Context(), billingqry.GetQuoteByAppointmentQuery{AppointmentID: id})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// ── GET /billing/patients/:patientId/account ──────────────────────

func (h *billingHTTPHandler) GetPatientAccount(w http.ResponseWriter, r *http.Request) {
	patientID, err := parseUUID(r, "patientId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patientId inválido")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	dto, err := h.getPatientAcct.Handle(r.Context(), billingqry.GetPatientAccountQuery{
		PatientID: sharedtypes.PatientID(patientID),
		Page:      sharedtypes.NewPage(limit, offset),
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// ── GET /billing/patients/:patientId/quotes ───────────────────────

func (h *billingHTTPHandler) GetPatientQuotes(w http.ResponseWriter, r *http.Request) {
	patientID, err := parseUUID(r, "patientId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patientId inválido")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	result, err := h.getPatientQuotes.Handle(r.Context(), billingqry.GetPatientQuotesQuery{
		PatientID: sharedtypes.PatientID(patientID),
		Page:      sharedtypes.NewPage(limit, offset),
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── POST /billing/quotes/:quoteId/payments ────────────────────────

type registerPaymentRequest struct {
	AmountCents   int64  `json:"amount_cents"`
	PaymentMethod string `json:"payment_method"`
	Notes         string `json:"notes,omitempty"`
}

func (h *billingHTTPHandler) RegisterPayment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}
	if !claims.HasRole(middleware.RoleReceptionist, middleware.RoleClinicAdmin, middleware.RoleSuperAdmin) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "rol insuficiente")
		return
	}
	quoteID, err := parseUUID(r, "quoteId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "quoteId inválido")
		return
	}
	var req registerPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}
	result, err := h.registerPayment.Handle(r.Context(), billingcmd.RegisterPaymentCommand{
		QuoteID:       quoteID,
		AmountCents:   req.AmountCents,
		PaymentMethod: req.PaymentMethod,
		Notes:         req.Notes,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"payment_id":   result.PaymentID.String(),
		"quote_status": result.QuoteStatus,
	})
}

// ── POST /billing/quotes/:quoteId/payments/mercadopago ────────────

type initMPPaymentRequest struct {
	AmountCents    int64  `json:"amount_cents"`
	PatientName    string `json:"patient_name"`
	ProcedureDesc  string `json:"procedure_description"`
	BackURLSuccess string `json:"back_url_success"`
	BackURLFailure string `json:"back_url_failure"`
	BackURLPending string `json:"back_url_pending"`
}

func (h *billingHTTPHandler) InitMPPayment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}
	quoteID, err := parseUUID(r, "quoteId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "quoteId inválido")
		return
	}
	var req initMPPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	// La URL del webhook se construye desde la config del servidor.
	// En producción: usar el dominio público configurado.
	webhookURL := r.URL.Scheme + "://" + r.Host + "/api/v1/billing/webhooks/mercadopago"
	if r.URL.Scheme == "" {
		webhookURL = "https://" + r.Host + "/api/v1/billing/webhooks/mercadopago"
	}

	result, err := h.initMPPayment.Handle(r.Context(), billingcmd.InitMPPaymentCommand{
		QuoteID:        quoteID,
		AmountCents:    req.AmountCents,
		PatientName:    req.PatientName,
		ProcedureDesc:  req.ProcedureDesc,
		BackURLSuccess: req.BackURLSuccess,
		BackURLFailure: req.BackURLFailure,
		BackURLPending: req.BackURLPending,
		WebhookURL:     webhookURL,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"payment_id":    result.PaymentID.String(),
		"preference_id": result.PreferenceID,
		"init_point":    result.InitPoint,
		"sandbox_url":   result.SandboxURL,
	})
}

// ── POST /billing/quotes/:quoteId/void ───────────────────────────

type voidQuoteRequest struct {
	Reason string `json:"reason"`
}

func (h *billingHTTPHandler) VoidQuote(w http.ResponseWriter, r *http.Request) {
	quoteID, err := parseUUID(r, "quoteId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "quoteId inválido")
		return
	}
	var req voidQuoteRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := h.voidQuote.Handle(r.Context(), billingcmd.VoidQuoteCommand{
		AppointmentID: quoteID,
		Reason:        req.Reason,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── POST /billing/quotes/:quoteId/refund ─────────────────────────

type refundRequest struct {
	Reason string `json:"reason"`
}

func (h *billingHTTPHandler) Refund(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}
	quoteID, err := parseUUID(r, "quoteId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "quoteId inválido")
		return
	}
	var req refundRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := h.refund.HandleWithAggregate(r.Context(), billingcmd.RefundCommand{
		QuoteID:    quoteID,
		Reason:     req.Reason,
		RefundedBy: claims.UserID,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── PUT /billing/late-fees/:feeId/waive ──────────────────────────

type waiveLateFeeRequest struct {
	QuoteID string `json:"quote_id"`
	Reason  string `json:"reason"`
}

func (h *billingHTTPHandler) WaiveLateFee(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}
	feeID, err := parseUUID(r, "feeId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "feeId inválido")
		return
	}
	var req waiveLateFeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}
	quoteID, err := uuid.Parse(req.QuoteID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "quote_id inválido")
		return
	}
	if err := h.waiveLateFee.Handle(r.Context(), billingcmd.WaiveLateFeeCommand{
		QuoteID:  quoteID,
		FeeID:    feeID,
		WaivedBy: claims.UserID,
		Reason:   req.Reason,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Helpers ───────────────────────────────────────────────────────

func parseUUID(r *http.Request, param string) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, param))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

func writeErrorFromDomain(w http.ResponseWriter, err error) {
	if de, ok := sharederrors.As(err); ok {
		writeError(w, de.HTTPStatus(), string(de.Code), de.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL", "error interno del servidor")
}

