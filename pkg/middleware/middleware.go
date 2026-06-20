// Package middleware provee los middlewares HTTP transversales del sistema.
//
// Incluye:
//   - JWT: validación de access tokens y extracción de claims al contexto
//   - RBAC: autorización por rol con soporte para múltiples roles permitidos
//   - RequestID: correlación de requests con UUID único
//   - Logger: logging estructurado de requests con duración y status
//   - Recoverer: panic recovery con logging y respuesta 500
package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ── Context keys ─────────────────────────────────────────────────

type contextKey string

const (
	ctxKeyUserClaims  contextKey = "user_claims"
	ctxKeyRequestID   contextKey = "request_id"
)

// ── UserClaims ───────────────────────────────────────────────────

// Role define los roles del sistema (ver IAM bounded context).
type Role string

const (
	RolePatient       Role = "paciente"
	RoleProfessional  Role = "profesional"
	RoleReceptionist  Role = "recepcionista"
	RoleClinicAdmin   Role = "admin_sucursal"
	RoleSuperAdmin    Role = "superadmin"
)

// UserClaims contiene el payload del JWT access token.
// Se extrae del token y se inyecta en el contexto por el middleware JWT.
type UserClaims struct {
	UserID         uuid.UUID `json:"user_id"`
	PatientID      *uuid.UUID `json:"patient_id,omitempty"` // nil si no es paciente
	Role           Role      `json:"role"`
	ClinicIDs      []uuid.UUID `json:"clinic_ids,omitempty"` // sedes autorizadas (staff)
	FamilyID       *uuid.UUID `json:"family_id,omitempty"`
	IsGuardian     bool      `json:"is_guardian"`
	jwt.RegisteredClaims
}

// HasRole reporta si el usuario tiene alguno de los roles dados.
func (c *UserClaims) HasRole(roles ...Role) bool {
	for _, r := range roles {
		if c.Role == r {
			return true
		}
	}
	return false
}

// CanAccessClinic reporta si el usuario tiene acceso a una sede específica.
// Los superadmin y pacientes no tienen restricción de sede.
func (c *UserClaims) CanAccessClinic(clinicID uuid.UUID) bool {
	if c.Role == RoleSuperAdmin || c.Role == RolePatient {
		return true
	}
	for _, id := range c.ClinicIDs {
		if id == clinicID {
			return true
		}
	}
	return false
}

// ── Context helpers ───────────────────────────────────────────────

// ClaimsFromContext extrae UserClaims del contexto HTTP.
// Retorna nil si no hay claims (request no autenticado).
func ClaimsFromContext(ctx context.Context) *UserClaims {
	v := ctx.Value(ctxKeyUserClaims)
	if v == nil {
		return nil
	}
	claims, _ := v.(*UserClaims)
	return claims
}

// RequestIDFromContext extrae el ID de correlación del request.
func RequestIDFromContext(ctx context.Context) string {
	v := ctx.Value(ctxKeyRequestID)
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// ── JWTConfig ─────────────────────────────────────────────────────

// JWTConfig contiene la configuración para el middleware JWT.
type JWTConfig struct {
	// SecretKey es la clave HMAC-SHA256 para verificar los tokens.
	SecretKey []byte
	// Issuer es el claim 'iss' esperado (ej: "odontoagenda.iam").
	Issuer string
}

// ── Middlewares ───────────────────────────────────────────────────

// RequestID inyecta un UUID de correlación en el contexto y en el header X-Request-ID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Logger registra cada request con método, ruta, status y duración.
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := newResponseWriter(w)

			next.ServeHTTP(rw, r)

			logger.InfoContext(r.Context(), "http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFromContext(r.Context()),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

// Recoverer captura panics, los loguea y devuelve HTTP 500.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "panic recovered",
						"panic", rec,
						"path", r.URL.Path,
						"request_id", RequestIDFromContext(r.Context()),
					)
					writeError(w, http.StatusInternalServerError, "INTERNAL", "error interno del servidor")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// JWT valida el Bearer token en el header Authorization y extrae los claims.
// Si el token es inválido o ausente, responde 401.
func JWT(cfg JWTConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, err := extractBearerToken(r)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
				return
			}

			claims := &UserClaims{}
			token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("método de firma inesperado: %v", t.Header["alg"])
				}
				return cfg.SecretKey, nil
			},
				jwt.WithIssuer(cfg.Issuer),
				jwt.WithExpirationRequired(),
			)
			if err != nil || !token.Valid {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token inválido o expirado")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyUserClaims, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRoles retorna 403 si el usuario autenticado no tiene alguno de los roles dados.
// Debe usarse después del middleware JWT.
func RequireRoles(roles ...Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "autenticación requerida")
				return
			}
			if !claims.HasRole(roles...) {
				writeError(w, http.StatusForbidden, "FORBIDDEN",
					fmt.Sprintf("rol '%s' no autorizado para este recurso", claims.Role),
				)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireClinicAccess valida que el usuario tenga acceso a la sede especificada
// en el path parameter {clinicId}.
// Debe usarse con chi router: chi.URLParam(r, "clinicId").
func RequireClinicAccess(getClinicID func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "autenticación requerida")
				return
			}

			rawID := getClinicID(r)
			clinicID, err := uuid.Parse(rawID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "clinicId inválido")
				return
			}

			if !claims.CanAccessClinic(clinicID) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "sin acceso a esta sede")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ── Helpers internos ──────────────────────────────────────────────

func extractBearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("header Authorization ausente")
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("formato de Authorization inválido: esperado 'Bearer <token>'")
	}
	if parts[1] == "" {
		return "", fmt.Errorf("token ausente en header Authorization")
	}
	return parts[1], nil
}

// ErrorResponse es el cuerpo JSON estándar para errores HTTP.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	ReqID   string `json:"request_id,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Code: code, Message: message})
}

// responseWriter envuelve http.ResponseWriter para capturar el status code.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}
