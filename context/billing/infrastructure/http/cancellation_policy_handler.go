// Package http — handler para la configuración de políticas de cancelación por sede.
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/billing/domain/repository"
	"github.com/juantevez/odontoagenda/context/billing/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── CancellationPolicyHTTPHandler ────────────────────────────────

type CancellationPolicyHTTPHandler struct {
	repo   repository.ClinicCancellationPolicyRepository
	logger *slog.Logger
}

func NewCancellationPolicyHTTPHandler(repo repository.ClinicCancellationPolicyRepository) *CancellationPolicyHTTPHandler {
	return &CancellationPolicyHTTPHandler{
		repo:   repo,
		logger: slog.Default().With("adapter", "billing.cancellation_policy.http"),
	}
}

// ── GET /billing/clinics/:clinicId/cancellation-policy ────────────

// GetCancellationPolicy retorna la política vigente de una sede.
// Si la sede no tiene configuración propia, retorna la política por defecto.
func (h *CancellationPolicyHTTPHandler) GetCancellationPolicy(w http.ResponseWriter, r *http.Request) {
	clinicID, err := uuid.Parse(chi.URLParam(r, "clinicId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "clinicId inválido")
		return
	}

	policy, err := h.repo.FindByClinic(r.Context(), sharedtypes.ClinicID(clinicID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "error consultando política")
		return
	}

	writeJSON(w, http.StatusOK, toCancellationPolicyDTO(clinicID, policy))
}

// ── PUT /billing/clinics/:clinicId/cancellation-policy ────────────

type updateCancellationPolicyRequest struct {
	FreeHours               int   `json:"free_hours"`
	LateCancellationPercent int   `json:"late_cancellation_fee_percent"`
	NoShowPercent           int   `json:"no_show_fee_percent"`
	MinFeeCents             int64 `json:"min_fee_cents"`
}

// UpdateCancellationPolicy crea o actualiza la política de cancelación de una sede.
// Solo accesible para admin_sucursal y superadmin.
func (h *CancellationPolicyHTTPHandler) UpdateCancellationPolicy(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}

	clinicID, err := uuid.Parse(chi.URLParam(r, "clinicId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "clinicId inválido")
		return
	}

	var req updateCancellationPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	// Validaciones básicas.
	if req.FreeHours < 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "free_hours no puede ser negativo")
		return
	}
	if req.LateCancellationPercent < 0 || req.LateCancellationPercent > 100 {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "late_cancellation_fee_percent debe estar entre 0 y 100")
		return
	}
	if req.NoShowPercent < 0 || req.NoShowPercent > 100 {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "no_show_fee_percent debe estar entre 0 y 100")
		return
	}
	if req.MinFeeCents < 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "min_fee_cents no puede ser negativo")
		return
	}

	policy := valueobject.CancellationPolicy{
		FreeHours:               req.FreeHours,
		LateCancellationPercent: req.LateCancellationPercent,
		NoShowPercent:           req.NoShowPercent,
		MinFeeCents:             req.MinFeeCents,
	}

	if err := h.repo.Upsert(r.Context(), sharedtypes.ClinicID(clinicID), policy, claims.UserID); err != nil {
		h.logger.ErrorContext(r.Context(), "error guardando política de cancelación",
			"clinic_id", clinicID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "error guardando política")
		return
	}

	h.logger.InfoContext(r.Context(), "política de cancelación actualizada",
		"clinic_id", clinicID,
		"free_hours", req.FreeHours,
		"late_percent", req.LateCancellationPercent,
		"no_show_percent", req.NoShowPercent,
		"updated_by", claims.UserID,
	)

	writeJSON(w, http.StatusOK, toCancellationPolicyDTO(clinicID, policy))
}

// ── DTO ───────────────────────────────────────────────────────────

type cancellationPolicyDTO struct {
	ClinicID                string `json:"clinic_id"`
	FreeHours               int    `json:"free_hours"`
	LateCancellationPercent int    `json:"late_cancellation_fee_percent"`
	NoShowPercent           int    `json:"no_show_fee_percent"`
	MinFeeCents             int64  `json:"min_fee_cents"`
	// Ejemplos calculados para facilitar la comprensión del admin.
	ExampleArancelCents         int64 `json:"example_arancel_cents"`
	ExampleLateCancellationFee  int64 `json:"example_late_cancellation_fee_cents"`
	ExampleNoShowFee            int64 `json:"example_no_show_fee_cents"`
}

func toCancellationPolicyDTO(clinicID uuid.UUID, p valueobject.CancellationPolicy) cancellationPolicyDTO {
	// Ejemplo con copago de $1000 para ilustrar los montos calculados.
	exampleCoPay := int64(100000) // $1000 en centavos
	lateFee := exampleCoPay * int64(p.LateCancellationPercent) / 100
	noShowFee := exampleCoPay * int64(p.NoShowPercent) / 100
	if lateFee < p.MinFeeCents {
		lateFee = p.MinFeeCents
	}
	if noShowFee < p.MinFeeCents {
		noShowFee = p.MinFeeCents
	}

	return cancellationPolicyDTO{
		ClinicID:                clinicID.String(),
		FreeHours:               p.FreeHours,
		LateCancellationPercent: p.LateCancellationPercent,
		NoShowPercent:           p.NoShowPercent,
		MinFeeCents:             p.MinFeeCents,
		ExampleArancelCents:     exampleCoPay,
		ExampleLateCancellationFee: lateFee,
		ExampleNoShowFee:        noShowFee,
	}
}

// RegisterCancellationPolicyRoutes monta las rutas de política de cancelación.
// Se llama desde RegisterRoutes en handler.go.
func RegisterCancellationPolicyRoutes(r chi.Router, h *CancellationPolicyHTTPHandler) {
	// Lectura: recepcionista y admin pueden ver la política.
	r.Get("/billing/clinics/{clinicId}/cancellation-policy", h.GetCancellationPolicy)

	// Escritura: solo admin.
	r.Put("/billing/clinics/{clinicId}/cancellation-policy",
		middleware.RequireRoles(middleware.RoleClinicAdmin, middleware.RoleSuperAdmin)(
			http.HandlerFunc(h.UpdateCancellationPolicy)).ServeHTTP,
	)
}
