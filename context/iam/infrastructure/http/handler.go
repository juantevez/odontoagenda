// Package http contiene los adaptadores de entrada HTTP del bounded context IAM.
// Traduce requests HTTP a Commands/Queries y respuestas de dominio a JSON.
// No contiene lógica de negocio.
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/juantevez/odontoagenda/context/iam/application/command"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── Router ────────────────────────────────────────────────────────

// RegisterRoutes monta todas las rutas del contexto IAM en el router dado.
// Recibe los handlers ya construidos con sus dependencias inyectadas.
func RegisterRoutes(
	r chi.Router,
	jwtCfg middleware.JWTConfig,
	registerHandler *command.RegisterUserHandler,
	loginHandler *command.LoginHandler,
	refreshHandler *command.RefreshTokensHandler,
	logoutHandler *command.LogoutHandler,
) {
	h := &iamHTTPHandler{
		register: registerHandler,
		login:    loginHandler,
		refresh:  refreshHandler,
		logout:   logoutHandler,
		logger:   slog.Default().With("adapter", "iam.http"),
	}

	// Rutas públicas (sin JWT).
	r.Post("/auth/register", h.Register)
	r.Post("/auth/login", h.Login)
	r.Post("/auth/refresh", h.RefreshTokens)

	// Rutas protegidas.
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWT(jwtCfg))
		r.Post("/auth/logout", h.Logout)
		r.Get("/auth/me", h.Me)
	})
}

// ── Handler struct ────────────────────────────────────────────────

type iamHTTPHandler struct {
	register *command.RegisterUserHandler
	login    *command.LoginHandler
	refresh  *command.RefreshTokensHandler
	logout   *command.LogoutHandler
	logger   *slog.Logger
}

// ── POST /auth/register ───────────────────────────────────────────

type registerRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	Role       string `json:"role"`
	FamilyName string `json:"family_name,omitempty"`
}

type registerResponse struct {
	UserID   string  `json:"user_id"`
	FamilyID *string `json:"family_id,omitempty"`
}

func (h *iamHTTPHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo de request inválido")
		return
	}

	result, err := h.register.Handle(r.Context(), command.RegisterUserCommand{
		Email:         req.Email,
		PlainPassword: req.Password,
		Role:          req.Role,
		FamilyName:    req.FamilyName,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}

	resp := registerResponse{UserID: result.UserID.String()}
	if result.FamilyID != nil {
		s := result.FamilyID.String()
		resp.FamilyID = &s
	}

	writeJSON(w, http.StatusCreated, resp)
}

// ── POST /auth/login ──────────────────────────────────────────────

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	DeviceID string `json:"device_id"`
}

type loginResponse struct {
	AccessToken        string `json:"access_token"`
	RefreshToken       string `json:"refresh_token"`
	AccessTokenExpiry  int64  `json:"access_token_expiry"`
	RefreshTokenExpiry int64  `json:"refresh_token_expiry"`
	TokenType          string `json:"token_type"`
}

func (h *iamHTTPHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo de request inválido")
		return
	}

	if req.DeviceID == "" {
		req.DeviceID = r.Header.Get("User-Agent")
	}

	result, err := h.login.Handle(r.Context(), command.LoginCommand{
		Email:         req.Email,
		PlainPassword: req.Password,
		DeviceID:      req.DeviceID,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:        result.AccessToken,
		RefreshToken:       result.RefreshToken,
		AccessTokenExpiry:  result.AccessTokenExpiry,
		RefreshTokenExpiry: result.RefreshTokenExpiry,
		TokenType:          "Bearer",
	})
}

// ── POST /auth/refresh ────────────────────────────────────────────

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
	DeviceID     string `json:"device_id"`
}

func (h *iamHTTPHandler) RefreshTokens(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo de request inválido")
		return
	}

	userID, err := sharedtypes.ParseID(req.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "user_id inválido")
		return
	}

	result, err := h.refresh.Handle(r.Context(), command.RefreshTokensCommand{
		RefreshToken: req.RefreshToken,
		DeviceID:     req.DeviceID,
		UserID:       userID,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:        result.AccessToken,
		RefreshToken:       result.RefreshToken,
		AccessTokenExpiry:  result.AccessTokenExpiry,
		RefreshTokenExpiry: result.RefreshTokenExpiry,
		TokenType:          "Bearer",
	})
}

// ── POST /auth/logout ─────────────────────────────────────────────

type logoutRequest struct {
	RefreshToken string `json:"refresh_token,omitempty"`
	GlobalLogout bool   `json:"global_logout,omitempty"`
}

func (h *iamHTTPHandler) Logout(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}

	var req logoutRequest
	// Ignoramos error de decode: el body es opcional en logout.
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := h.logout.Handle(r.Context(), command.LogoutCommand{
		UserID:       claims.UserID,
		RefreshToken: req.RefreshToken,
		GlobalLogout: req.GlobalLogout,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── GET /auth/me ──────────────────────────────────────────────────

type meResponse struct {
	UserID     string   `json:"user_id"`
	Role       string   `json:"role"`
	PatientID  *string  `json:"patient_id,omitempty"`
	FamilyID   *string  `json:"family_id,omitempty"`
	IsGuardian bool     `json:"is_guardian"`
	ClinicIDs  []string `json:"clinic_ids,omitempty"`
}

func (h *iamHTTPHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}

	resp := meResponse{
		UserID:     claims.UserID.String(),
		Role:       string(claims.Role),
		IsGuardian: claims.IsGuardian,
	}

	if claims.PatientID != nil {
		s := claims.PatientID.String()
		resp.PatientID = &s
	}
	if claims.FamilyID != nil {
		s := claims.FamilyID.String()
		resp.FamilyID = &s
	}
	for _, id := range claims.ClinicIDs {
		resp.ClinicIDs = append(resp.ClinicIDs, id.String())
	}

	writeJSON(w, http.StatusOK, resp)
}

// ── Helpers ───────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

// writeErrorFromDomain traduce un error de dominio a la respuesta HTTP apropiada.
func writeErrorFromDomain(w http.ResponseWriter, err error) {
	if de, ok := sharederrors.As(err); ok {
		writeError(w, de.HTTPStatus(), string(de.Code), de.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL", "error interno del servidor")
}
