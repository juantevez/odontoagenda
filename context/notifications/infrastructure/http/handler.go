// Package http contiene los adaptadores de entrada HTTP del bounded context Notifications.
package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	notifcmd "github.com/juantevez/odontoagenda/context/notifications/application/command"
	"github.com/juantevez/odontoagenda/context/notifications/domain/repository"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

// RegisterRoutes monta las rutas del bounded context Notifications.
func RegisterRoutes(r chi.Router, jwtCfg middleware.JWTConfig, inboxRepo repository.InboxRepository, writeHandler *notifcmd.WriteInboxHandler) {
	h := &notifHTTPHandler{
		inboxRepo:    inboxRepo,
		writeHandler: writeHandler,
	}

	r.Group(func(r chi.Router) {
		r.Use(middleware.JWT(jwtCfg))
		r.Get("/notifications", h.List)
		r.Post("/notifications/read-all", h.MarkAllRead)
		r.Patch("/notifications/{id}/read", h.MarkRead)
	})
}

type notifHTTPHandler struct {
	inboxRepo    repository.InboxRepository
	writeHandler *notifcmd.WriteInboxHandler
}

// ── GET /notifications ────────────────────────────────────────────

type inboxItemDTO struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	ClinicID    *string `json:"clinic_id,omitempty"`
	ReferenceID string  `json:"reference_id,omitempty"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	IsRead      bool    `json:"is_read"`
	ReadAt      *string `json:"read_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

type listResponse struct {
	Items       []inboxItemDTO `json:"items"`
	UnreadCount int            `json:"unread_count"`
}

func (h *notifHTTPHandler) List(w http.ResponseWriter, r *http.Request) {
	clinicIDStr := r.URL.Query().Get("clinic_id")
	if clinicIDStr == "" {
		writeError(w, http.StatusBadRequest, "clinic_id requerido")
		return
	}
	clinicID, err := uuid.Parse(clinicIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "clinic_id inválido")
		return
	}

	unreadOnly := r.URL.Query().Get("unread_only") == "true"
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	items, err := h.inboxRepo.FindByClinic(r.Context(), clinicID, unreadOnly, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "error al obtener notificaciones")
		return
	}

	unread, err := h.inboxRepo.CountUnread(r.Context(), clinicID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "error al contar no leídas")
		return
	}

	dtos := make([]inboxItemDTO, len(items))
	for i, n := range items {
		dto := inboxItemDTO{
			ID:          n.ID.String(),
			Type:        string(n.Type),
			ReferenceID: n.ReferenceID,
			Title:       n.Title,
			Body:        n.Body,
			IsRead:      n.IsRead(),
			CreatedAt:   n.CreatedAt.Format(time.RFC3339),
		}
		if n.ClinicID != nil {
			s := n.ClinicID.String()
			dto.ClinicID = &s
		}
		if n.ReadAt != nil {
			s := n.ReadAt.Format(time.RFC3339)
			dto.ReadAt = &s
		}
		dtos[i] = dto
	}

	writeJSON(w, http.StatusOK, listResponse{Items: dtos, UnreadCount: unread})
}

// ── PATCH /notifications/{id}/read ───────────────────────────────

func (h *notifHTTPHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := h.inboxRepo.MarkRead(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "error al marcar como leída")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── POST /notifications/read-all ─────────────────────────────────

func (h *notifHTTPHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	clinicIDStr := r.URL.Query().Get("clinic_id")
	if clinicIDStr == "" {
		writeError(w, http.StatusBadRequest, "clinic_id requerido")
		return
	}
	clinicID, err := uuid.Parse(clinicIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "clinic_id inválido")
		return
	}
	if err := h.inboxRepo.MarkAllRead(r.Context(), clinicID); err != nil {
		writeError(w, http.StatusInternalServerError, "error al marcar todas como leídas")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── helpers ───────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
