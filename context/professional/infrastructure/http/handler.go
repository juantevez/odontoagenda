// Package http contiene los adaptadores de entrada HTTP del bounded context Professional.
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	profcmd "github.com/juantevez/odontoagenda/context/professional/application/command"
	profqry "github.com/juantevez/odontoagenda/context/professional/application/query"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// RegisterRoutes monta todas las rutas del contexto Professional.
func RegisterRoutes(
	r chi.Router,
	jwtCfg middleware.JWTConfig,
	registerHandler *profcmd.RegisterProfessionalHandler,
	addLicenseHandler *profcmd.AddLicenseHandler,
	assignClinicHandler *profcmd.AssignToClinicHandler,
	updateScheduleHandler *profcmd.UpdateClinicScheduleHandler,
	addExceptionHandler *profcmd.AddExceptionHandler,
	setDurationHandler *profcmd.SetProcedureDurationHandler,
	suspendHandler *profcmd.SuspendProfessionalHandler,
	getByIDHandler *profqry.GetProfessionalByIDHandler,
	findByClinicHandler *profqry.FindByClinicHandler,
	availableAtHandler *profqry.FindAvailableAtHandler,
	forSchedulingHandler *profqry.GetProfessionalForSchedulingHandler,
) {
	h := &professionalHTTPHandler{
		getByID:       getByIDHandler,
		findByClinic:  findByClinicHandler,
		availableAt:   availableAtHandler,
		forScheduling: forSchedulingHandler,
		logger:        slog.Default().With("adapter", "professional.http"),
	}

	_ = registerHandler
	_ = addLicenseHandler
	_ = assignClinicHandler
	_ = updateScheduleHandler
	_ = addExceptionHandler
	_ = setDurationHandler
	_ = suspendHandler

	r.Group(func(r chi.Router) {
		r.Use(middleware.JWT(jwtCfg))

		r.Get("/professionals", h.FindByClinic)
		r.Get("/professionals/{professionalId}", h.GetByID)
		r.Get("/professionals/{professionalId}/for-scheduling", h.GetForScheduling)
	})
}

type professionalHTTPHandler struct {
	getByID       *profqry.GetProfessionalByIDHandler
	findByClinic  *profqry.FindByClinicHandler
	availableAt   *profqry.FindAvailableAtHandler
	forScheduling *profqry.GetProfessionalForSchedulingHandler
	logger        *slog.Logger
}

// GET /professionals?clinic_id=...&specialty=...
func (h *professionalHTTPHandler) FindByClinic(w http.ResponseWriter, r *http.Request) {
	clinicIDStr := r.URL.Query().Get("clinic_id")
	specialty := r.URL.Query().Get("specialty")

	var clinicID sharedtypes.ClinicID
	if clinicIDStr != "" {
		id, err := uuid.Parse(clinicIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "clinic_id inválido")
			return
		}
		clinicID = id
	}

	q := profqry.FindByClinicQuery{ClinicID: clinicID}
	if specialty != "" {
		q.Specialty = &specialty
	}

	result, err := h.findByClinic.Handle(r.Context(), q)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// GET /professionals/{professionalId}
func (h *professionalHTTPHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "professionalId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "professionalId inválido")
		return
	}

	result, err := h.getByID.Handle(r.Context(), profqry.GetProfessionalByIDQuery{ProfessionalID: id})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// GET /professionals/{professionalId}/for-scheduling
func (h *professionalHTTPHandler) GetForScheduling(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "professionalId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "professionalId inválido")
		return
	}

	result, err := h.forScheduling.Handle(r.Context(), profqry.GetProfessionalForSchedulingQuery{ProfessionalID: id})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── helpers ───────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

func writeErrorFromDomain(w http.ResponseWriter, err error) {
	switch {
	case sharederrors.IsCode(err, sharederrors.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case sharederrors.IsCode(err, sharederrors.ErrInvalidArgument):
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
	case sharederrors.IsCode(err, sharederrors.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "ALREADY_EXISTS", err.Error())
	case sharederrors.IsCode(err, sharederrors.ErrPrecondition):
		writeError(w, http.StatusUnprocessableEntity, "PRECONDITION_FAILED", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", "error interno del servidor")
	}
}
