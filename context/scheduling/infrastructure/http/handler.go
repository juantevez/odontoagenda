// Package http contiene los adaptadores de entrada HTTP del bounded context Scheduling.
package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/application/command"
	"github.com/juantevez/odontoagenda/context/scheduling/application/query"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// RegisterRoutes monta todas las rutas del contexto Scheduling.
func RegisterRoutes(
	r chi.Router,
	jwtCfg middleware.JWTConfig,
	bookHandler          *command.BookAppointmentHandler,
	cancelHandler        *command.CancelAppointmentHandler,
	completeHandler      *command.CompleteAppointmentHandler,
	checkInHandler       *command.CheckInAppointmentHandler,
	noShowHandler        *command.MarkNoShowHandler,
	blockSlotHandler     *command.BlockSlotHandler,
	holdSlotHandler      *command.HoldSlotHandler,
	releaseHoldHandler   *command.ReleaseHoldHandler,
	getAvailHandler      *query.GetAvailabilityHandler,
	getAvailRangeHandler *query.GetAvailabilityRangeHandler,
	getDayHandler        *query.GetDayScheduleHandler,
	getPatientAppts      *query.GetPatientAppointmentsHandler,
) {
	h := &schedulingHTTPHandler{
		book:          bookHandler,
		cancel:        cancelHandler,
		complete:      completeHandler,
		checkIn:       checkInHandler,
		noShow:        noShowHandler,
		blockSlot:     blockSlotHandler,
		holdSlot:      holdSlotHandler,
		releaseHold:   releaseHoldHandler,
		getAvail:      getAvailHandler,
		getAvailRange: getAvailRangeHandler,
		getDay:        getDayHandler,
		getPatient:    getPatientAppts,
	}

	r.Group(func(r chi.Router) {
		r.Use(middleware.JWT(jwtCfg))

		// Disponibilidad (lectura pública para pacientes y staff)
		r.Get("/scheduling/availability", h.GetAvailability)
		r.Get("/scheduling/availability/range", h.GetAvailabilityRange)

		// Vista del día (staff)
		r.Get("/scheduling/day-schedule", h.GetDaySchedule)

		// Citas del paciente
		r.Get("/scheduling/patients/{patientId}/appointments", h.GetPatientAppointments)

		// Reserva (pacientes y staff)
		r.Post("/scheduling/appointments", h.BookAppointment)

		// Gestión de citas (staff y paciente según rol)
		r.Post("/scheduling/appointments/{appointmentId}/cancel", h.CancelAppointment)
		r.Post("/scheduling/appointments/{appointmentId}/check-in",
			middleware.RequireRoles(
				middleware.RoleReceptionist,
				middleware.RoleProfessional,
				middleware.RoleClinicAdmin,
				middleware.RoleSuperAdmin,
			)(http.HandlerFunc(h.CheckIn)).ServeHTTP,
		)
		r.Post("/scheduling/appointments/{appointmentId}/complete",
			middleware.RequireRoles(
				middleware.RoleProfessional,
				middleware.RoleClinicAdmin,
				middleware.RoleSuperAdmin,
			)(http.HandlerFunc(h.Complete)).ServeHTTP,
		)
		r.Post("/scheduling/appointments/{appointmentId}/no-show",
			middleware.RequireRoles(
				middleware.RoleReceptionist,
				middleware.RoleClinicAdmin,
				middleware.RoleSuperAdmin,
			)(http.HandlerFunc(h.MarkNoShow)).ServeHTTP,
		)

		// Bloqueos de agenda (solo staff)
		r.Post("/scheduling/block-slot",
			middleware.RequireRoles(
				middleware.RoleProfessional,
				middleware.RoleReceptionist,
				middleware.RoleClinicAdmin,
				middleware.RoleSuperAdmin,
			)(http.HandlerFunc(h.BlockSlot)).ServeHTTP,
		)

		// Holds temporales de slots (Mapa de Muelas)
		r.Post("/scheduling/hold", h.HoldSlot)
		r.Delete("/scheduling/hold/{holdId}", h.ReleaseHold)
	})
}

// ── Handler struct ────────────────────────────────────────────────

type schedulingHTTPHandler struct {
	book          *command.BookAppointmentHandler
	cancel        *command.CancelAppointmentHandler
	complete      *command.CompleteAppointmentHandler
	checkIn       *command.CheckInAppointmentHandler
	noShow        *command.MarkNoShowHandler
	blockSlot     *command.BlockSlotHandler
	holdSlot      *command.HoldSlotHandler
	releaseHold   *command.ReleaseHoldHandler
	getAvail      *query.GetAvailabilityHandler
	getAvailRange *query.GetAvailabilityRangeHandler
	getDay        *query.GetDayScheduleHandler
	getPatient    *query.GetPatientAppointmentsHandler
}

// ── GET /scheduling/availability ─────────────────────────────────

func (h *schedulingHTTPHandler) GetAvailability(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	profID, err := uuid.Parse(q.Get("professional_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "professional_id inválido")
		return
	}
	clinicID, err := uuid.Parse(q.Get("clinic_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "clinic_id inválido")
		return
	}
	date, err := time.Parse("2006-01-02", q.Get("date"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "date debe ser YYYY-MM-DD")
		return
	}
	procedureCode := q.Get("procedure_code")

	result, err := h.getAvail.Handle(r.Context(), query.GetAvailabilityQuery{
		ProfessionalID: sharedtypes.ProfessionalID(profID),
		ClinicID:       sharedtypes.ClinicID(clinicID),
		Date:           date,
		ProcedureCode:  procedureCode,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── GET /scheduling/availability/range ───────────────────────────

func (h *schedulingHTTPHandler) GetAvailabilityRange(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	profID, _ := uuid.Parse(q.Get("professional_id"))
	clinicID, _ := uuid.Parse(q.Get("clinic_id"))
	from, err := time.Parse("2006-01-02", q.Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "from debe ser YYYY-MM-DD")
		return
	}
	to, err := time.Parse("2006-01-02", q.Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "to debe ser YYYY-MM-DD")
		return
	}

	result, err := h.getAvailRange.Handle(r.Context(), query.GetAvailabilityRangeQuery{
		ProfessionalID: sharedtypes.ProfessionalID(profID),
		ClinicID:       sharedtypes.ClinicID(clinicID),
		ProcedureCode:  q.Get("procedure_code"),
		From:           from,
		To:             to,
		MaxResults:     20,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── GET /scheduling/day-schedule ─────────────────────────────────

func (h *schedulingHTTPHandler) GetDaySchedule(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	profID, _ := uuid.Parse(q.Get("professional_id"))
	clinicID, _ := uuid.Parse(q.Get("clinic_id"))
	date, err := time.Parse("2006-01-02", q.Get("date"))
	if err != nil {
		date = time.Now().UTC()
	}

	result, err := h.getDay.Handle(r.Context(), query.GetDayScheduleQuery{
		ProfessionalID: sharedtypes.ProfessionalID(profID),
		ClinicID:       sharedtypes.ClinicID(clinicID),
		Date:           date,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── GET /scheduling/patients/{patientId}/appointments ─────────────

func (h *schedulingHTTPHandler) GetPatientAppointments(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "patientId")
	patientID, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patientId inválido")
		return
	}

	result, err := h.getPatient.Handle(r.Context(), query.GetPatientAppointmentsQuery{
		PatientID:  sharedtypes.PatientID(patientID),
		OnlyActive: r.URL.Query().Get("only_active") == "true",
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── POST /scheduling/appointments ────────────────────────────────

type bookAppointmentRequest struct {
	PatientID             string  `json:"patient_id"`
	BookedByID            string  `json:"booked_by_id,omitempty"` // si difiere del patientID
	ProfessionalID        string  `json:"professional_id"`
	ClinicID              string  `json:"clinic_id"`
	ProcedureCode         string  `json:"procedure_code"`
	SlotStart             string  `json:"slot_start"` // RFC3339
	SlotEnd               string  `json:"slot_end"`   // RFC3339
	CoverageType          string  `json:"coverage_type"`
	AgreementID           *string `json:"agreement_id,omitempty"`
	RequiresAuthorization bool    `json:"requires_authorization"`
}

func (h *schedulingHTTPHandler) BookAppointment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}

	var req bookAppointmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	patientID, err := uuid.Parse(req.PatientID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patient_id inválido")
		return
	}
	bookedByID := patientID
	if req.BookedByID != "" {
		if id, err := uuid.Parse(req.BookedByID); err == nil {
			bookedByID = id
		}
	}
	profID, _ := uuid.Parse(req.ProfessionalID)
	clinicID, _ := uuid.Parse(req.ClinicID)
	slotStart, err := time.Parse(time.RFC3339, req.SlotStart)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "slot_start debe ser RFC3339")
		return
	}
	slotEnd, err := time.Parse(time.RFC3339, req.SlotEnd)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "slot_end debe ser RFC3339")
		return
	}

	cmd := command.BookAppointmentCommand{
		PatientID:             sharedtypes.PatientID(patientID),
		BookedByID:            sharedtypes.PatientID(bookedByID),
		ProfessionalID:        sharedtypes.ProfessionalID(profID),
		ClinicID:              sharedtypes.ClinicID(clinicID),
		ProcedureCode:         req.ProcedureCode,
		SlotStart:             slotStart,
		SlotEnd:               slotEnd,
		CoverageType:          req.CoverageType,
		RequiresAuthorization: req.RequiresAuthorization,
		CreatedBy:             claims.UserID,
	}
	if req.AgreementID != nil {
		if id, err := uuid.Parse(*req.AgreementID); err == nil {
			cmd.AgreementID = &id
		}
	}

	result, err := h.book.Handle(r.Context(), cmd)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"appointment_id": result.AppointmentID.String(),
		"status":         result.Status,
	})
}

// ── POST /scheduling/appointments/{appointmentId}/cancel ──────────

type cancelRequest struct {
	Reason string `json:"reason"`
	Note   string `json:"note,omitempty"`
}

func (h *schedulingHTTPHandler) CancelAppointment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}
	apptID, err := parseAppointmentID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "appointmentId inválido")
		return
	}

	var req cancelRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := h.cancel.Handle(r.Context(), command.CancelAppointmentCommand{
		AppointmentID: sharedtypes.AppointmentID(apptID),
		Reason:        req.Reason,
		Note:          req.Note,
		CancelledBy:   claims.UserID,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *schedulingHTTPHandler) CheckIn(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	apptID, err := parseAppointmentID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "appointmentId inválido")
		return
	}
	if err := h.checkIn.Handle(r.Context(), command.CheckInAppointmentCommand{
		AppointmentID: sharedtypes.AppointmentID(apptID),
		CheckedInBy:   claims.UserID,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type completeRequest struct {
	ClinicalNotes string `json:"clinical_notes,omitempty"`
}

func (h *schedulingHTTPHandler) Complete(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	apptID, err := parseAppointmentID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "appointmentId inválido")
		return
	}
	var req completeRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := h.complete.Handle(r.Context(), command.CompleteAppointmentCommand{
		AppointmentID: sharedtypes.AppointmentID(apptID),
		ClinicalNotes: req.ClinicalNotes,
		CompletedBy:   claims.UserID,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *schedulingHTTPHandler) MarkNoShow(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	apptID, err := parseAppointmentID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "appointmentId inválido")
		return
	}
	if err := h.noShow.Handle(r.Context(), command.MarkNoShowCommand{
		AppointmentID: sharedtypes.AppointmentID(apptID),
		MarkedBy:      claims.UserID,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type blockSlotRequest struct {
	ProfessionalID string `json:"professional_id"`
	ClinicID       string `json:"clinic_id"`
	SlotStart      string `json:"slot_start"` // RFC3339
	SlotEnd        string `json:"slot_end"`
	Reason         string `json:"reason"`
	Note           string `json:"note,omitempty"`
}

func (h *schedulingHTTPHandler) BlockSlot(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}
	var req blockSlotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}
	profID, _ := uuid.Parse(req.ProfessionalID)
	clinicID, _ := uuid.Parse(req.ClinicID)
	slotStart, _ := time.Parse(time.RFC3339, req.SlotStart)
	slotEnd, _ := time.Parse(time.RFC3339, req.SlotEnd)

	if err := h.blockSlot.Handle(r.Context(), command.BlockSlotCommand{
		ProfessionalID: sharedtypes.ProfessionalID(profID),
		ClinicID:       sharedtypes.ClinicID(clinicID),
		SlotStart:      slotStart,
		SlotEnd:        slotEnd,
		Reason:         req.Reason,
		Note:           req.Note,
		BlockedBy:      claims.UserID,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── POST /scheduling/hold ─────────────────────────────────────────

type holdSlotRequest struct {
	ProfessionalID string `json:"professional_id"`
	ClinicID       string `json:"clinic_id"`
	SlotStart      string `json:"slot_start"` // RFC3339
	SlotEnd        string `json:"slot_end"`
}

func (h *schedulingHTTPHandler) HoldSlot(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}
	var req holdSlotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}
	profID, err := uuid.Parse(req.ProfessionalID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "professional_id inválido")
		return
	}
	clinicID, err := uuid.Parse(req.ClinicID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "clinic_id inválido")
		return
	}
	slotStart, err := time.Parse(time.RFC3339, req.SlotStart)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "slot_start debe ser RFC3339")
		return
	}
	slotEnd, err := time.Parse(time.RFC3339, req.SlotEnd)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "slot_end debe ser RFC3339")
		return
	}

	result, err := h.holdSlot.Handle(r.Context(), command.HoldSlotCommand{
		ProfessionalID: sharedtypes.ProfessionalID(profID),
		ClinicID:       sharedtypes.ClinicID(clinicID),
		SlotStart:      slotStart,
		SlotEnd:        slotEnd,
		HeldBy:         claims.UserID,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"hold_id":    result.HoldID.String(),
		"expires_at": result.ExpiresAt.Format(time.RFC3339),
	})
}

// ── DELETE /scheduling/hold/{holdId} ─────────────────────────────

func (h *schedulingHTTPHandler) ReleaseHold(w http.ResponseWriter, r *http.Request) {
	holdID, err := uuid.Parse(chi.URLParam(r, "holdId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "holdId inválido")
		return
	}
	if err := h.releaseHold.Handle(r.Context(), command.ReleaseHoldCommand{HoldID: holdID}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Helpers ───────────────────────────────────────────────────────

func parseAppointmentID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "appointmentId"))
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
