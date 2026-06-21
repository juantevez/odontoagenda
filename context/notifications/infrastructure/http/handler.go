// Package http contiene los adaptadores de entrada HTTP del bounded context Notifications.
// En el MVP expone solo /health. Rutas futuras: /api/v1/notifications (historial, reenvío manual).
package http

import (
	"github.com/go-chi/chi/v5"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

// RegisterRoutes monta las rutas del contexto Notifications.
func RegisterRoutes(r chi.Router, _ middleware.JWTConfig) {
	// En el MVP no hay endpoints propios más allá del /health
	// que se registra directamente en wire.go.
	// Rutas futuras:
	//   GET  /api/v1/notifications          → historial de notificaciones enviadas
	//   POST /api/v1/notifications/resend   → reenvío manual por staff
	_ = r
}
