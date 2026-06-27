// Package http contiene los adaptadores de entrada HTTP del bounded context Patient.
// CAMBIO respecto a la versión anterior:
//   - POST /patients restringido a recepcionista, admin_sucursal y superadmin (ítem 3).
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/application/command"
	"github.com/juantevez/odontoagenda/context/patient/application/query"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// RegisterRoutes monta todas las rutas del contexto Patient.
func RegisterRoutes(
	r chi.Router,
	jwtCfg middleware.JWTConfig,
	registerHandler *command.RegisterPatientHandler,
	addCoverageHandler *command.AddCoverageHandler,
	addAlertHandler *command.AddMedicalAlertHandler,
	mergeHandler *command.MergePatientsHandler,
	updateContactHandler *command.UpdateContactInfoHandler,
	getByIDHandler *query.GetPatientByIDHandler,
	searchHandler *query.SearchPatientsHandler,
	forBookingHandler *query.GetPatientForBookingHandler,
) {
	h := &patientHTTPHandler{
		register:      registerHandler,
		addCoverage:   addCoverageHandler,
		addAlert:      addAlertHandler,
		merge:         mergeHandler,
		updateContact: updateContactHandler,
		getByID:       getByIDHandler,
		search:        searchHandler,
		forBooking:    forBookingHandler,
		logger:        slog.Default().With("adapter", "patient.http"),
	}

	r.Group(func(r chi.Router) {
		r.Use(middleware.JWT(jwtCfg))

		// ── Lectura (todos los autenticados) ──────────────────────
		r.Get("/patients", h.Search)
		r.Get("/patients/{patientId}", h.GetByID)
		r.Get("/patients/{patientId}/for-booking", h.GetForBooking)

		// ── Alta de paciente: SOLO staff ──────────────────────────
		r.Post("/patients",
			middleware.RequireRoles(
				middleware.RoleReceptionist,
				middleware.RoleClinicAdmin,
				middleware.RoleSuperAdmin,
			)(http.HandlerFunc(h.Register)).ServeHTTP,
		)

		// ── Cobertura: solo staff ─────────────────────────────────
		r.Post("/patients/{patientId}/coverage",
			middleware.RequireRoles(
				middleware.RoleReceptionist,
				middleware.RoleClinicAdmin,
				middleware.RoleSuperAdmin,
			)(http.HandlerFunc(h.AddCoverage)).ServeHTTP,
		)

		// ── Contacto: staff + el propio paciente ──────────────────
		r.Put("/patients/{patientId}/contact", h.UpdateContact)

		// ── Alertas médicas: staff + el propio paciente ───────────
		r.Post("/patients/{patientId}/medical-alerts", h.AddMedicalAlert)

		// ── Merge: solo admin ─────────────────────────────────────
		r.Post("/patients/merge",
			middleware.RequireRoles(
				middleware.RoleClinicAdmin,
				middleware.RoleSuperAdmin,
			)(http.HandlerFunc(h.MergePatients)).ServeHTTP,
		)
	})
}

// ── Handler struct ────────────────────────────────────────────────

type patientHTTPHandler struct {
	register      *command.RegisterPatientHandler
	addCoverage   *command.AddCoverageHandler
	addAlert      *command.AddMedicalAlertHandler
	merge         *command.MergePatientsHandler
	updateContact *command.UpdateContactInfoHandler
	getByID       *query.GetPatientByIDHandler
	search        *query.SearchPatientsHandler
	forBooking    *query.GetPatientForBookingHandler
	logger        *slog.Logger
}

// ── POST /patients ────────────────────────────────────────────────

type registerPatientRequest struct {
	FullName           string `json:"full_name"`
	BirthDate          string `json:"birth_date"` // "YYYY-MM-DD"
	Gender             string `json:"gender"`
	DocType            string `json:"doc_type"`
	DocNumber          string `json:"doc_number"`
	Phone              string `json:"phone"`
	Email              string `json:"email,omitempty"`
	EmergencyName      string `json:"emergency_name,omitempty"`
	EmergencyPhone     string `json:"emergency_phone,omitempty"`
	SkipDuplicateCheck bool   `json:"skip_duplicate_check,omitempty"`
}

func (h *patientHTTPHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerPatientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	birthDate, err := time.Parse("2006-01-02", req.BirthDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "birth_date debe ser YYYY-MM-DD")
		return
	}

	claims := middleware.ClaimsFromContext(r.Context())
	var createdBy *uuid.UUID
	if claims != nil {
		id := claims.UserID
		createdBy = &id
	}

	result, err := h.register.Handle(r.Context(), command.RegisterPatientCommand{
		FullName:           req.FullName,
		BirthDate:          birthDate,
		Gender:             req.Gender,
		DocType:            req.DocType,
		DocNumber:          req.DocNumber,
		Phone:              req.Phone,
		Email:              req.Email,
		EmergencyName:      req.EmergencyName,
		EmergencyPhone:     req.EmergencyPhone,
		SkipDuplicateCheck: req.SkipDuplicateCheck,
		CreatedBy:          createdBy,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}

	if len(result.DuplicateCandidates) > 0 {
		type duplicateWarning struct {
			Code       string `json:"code"`
			Message    string `json:"message"`
			Candidates []struct {
				PatientID string   `json:"patient_id"`
				FullName  string   `json:"full_name"`
				Score     float64  `json:"score"`
				MatchedOn []string `json:"matched_on"`
			} `json:"candidates"`
		}
		resp := duplicateWarning{
			Code:    "DUPLICATE_WARNING",
			Message: "Se encontraron pacientes similares. Confirme con skip_duplicate_check=true para continuar.",
		}
		for _, c := range result.DuplicateCandidates {
			resp.Candidates = append(resp.Candidates, struct {
				PatientID string   `json:"patient_id"`
				FullName  string   `json:"full_name"`
				Score     float64  `json:"score"`
				MatchedOn []string `json:"matched_on"`
			}{
				PatientID: c.Patient.ID().String(),
				FullName:  c.Patient.FullName().String(),
				Score:     c.Score,
				MatchedOn: c.MatchedOn,
			})
		}
		writeJSON(w, http.StatusConflict, resp)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"patient_id": result.PatientID.String()})
}

// ── GET /patients ─────────────────────────────────────────────────

func (h *patientHTTPHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	result, err := h.search.Handle(r.Context(), query.SearchPatientsQuery{
		Query: q,
		Page:  sharedtypes.NewPage(limit, offset),
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── GET /patients/{patientId} ─────────────────────────────────────

func (h *patientHTTPHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	patientID, err := parsePatientID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patientId inválido")
		return
	}
	dto, err := h.getByID.Handle(r.Context(), query.GetPatientByIDQuery{PatientID: patientID})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// ── GET /patients/{patientId}/for-booking ─────────────────────────

func (h *patientHTTPHandler) GetForBooking(w http.ResponseWriter, r *http.Request) {
	patientID, err := parsePatientID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patientId inválido")
		return
	}
	dto, err := h.forBooking.Handle(r.Context(), query.GetPatientForBookingQuery{PatientID: patientID})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// ── POST /patients/{patientId}/coverage ───────────────────────────

type addCoverageRequest struct {
	CoverageType     string  `json:"coverage_type"`
	AgreementID      *string `json:"agreement_id,omitempty"`
	ProviderName     string  `json:"provider_name"`
	PlanCode         string  `json:"plan_code"`
	MembershipNumber string  `json:"membership_number"`
	ValidFrom        string  `json:"valid_from"`
	ValidUntil       *string `json:"valid_until,omitempty"`
	CoPayPercent     *int    `json:"co_pay_percent,omitempty"`
	CoPayFixedCents  *int64  `json:"co_pay_fixed_cents,omitempty"`
}

func (h *patientHTTPHandler) AddCoverage(w http.ResponseWriter, r *http.Request) {
	patientID, err := parsePatientID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patientId inválido")
		return
	}
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}

	var req addCoverageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	validFrom, err := time.Parse("2006-01-02", req.ValidFrom)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "valid_from debe ser YYYY-MM-DD")
		return
	}

	cmd := command.AddCoverageCommand{
		PatientID:        patientID,
		CoverageType:     req.CoverageType,
		ProviderName:     req.ProviderName,
		PlanCode:         req.PlanCode,
		MembershipNumber: req.MembershipNumber,
		ValidFrom:        validFrom,
		CoPayPercent:     req.CoPayPercent,
		CoPayFixed:       req.CoPayFixedCents,
		AddedBy:          claims.UserID,
	}
	if req.AgreementID != nil {
		if id, err := uuid.Parse(*req.AgreementID); err == nil {
			cmd.AgreementID = &id
		}
	}
	if req.ValidUntil != nil {
		if t, err := time.Parse("2006-01-02", *req.ValidUntil); err == nil {
			cmd.ValidUntil = &t
		}
	}

	coverageID, err := h.addCoverage.Handle(r.Context(), cmd)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"coverage_id": coverageID.String()})
}

// ── POST /patients/{patientId}/medical-alerts ─────────────────────

type addAlertRequest struct {
	AlertType   string `json:"alert_type"`
	Severity    string `json:"severity,omitempty"`
	Description string `json:"description"`
}

func (h *patientHTTPHandler) AddMedicalAlert(w http.ResponseWriter, r *http.Request) {
	patientID, err := parsePatientID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patientId inválido")
		return
	}
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}

	var req addAlertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	isPatient := claims.Role == middleware.RolePatient
	alertID, err := h.addAlert.Handle(r.Context(), command.AddMedicalAlertCommand{
		PatientID:      patientID,
		AlertType:      req.AlertType,
		Severity:       req.Severity,
		Description:    req.Description,
		IsSelfReported: isPatient,
		RequestedBy:    claims.UserID,
		RequestedRole:  string(claims.Role),
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"alert_id": alertID.String()})
}

// ── POST /patients/merge ──────────────────────────────────────────

type mergePatientsRequest struct {
	TargetPatientID string `json:"target_patient_id"`
	SourcePatientID string `json:"source_patient_id"`
}

func (h *patientHTTPHandler) MergePatients(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}

	var req mergePatientsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	targetID, err := uuid.Parse(req.TargetPatientID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "target_patient_id inválido")
		return
	}
	sourceID, err := uuid.Parse(req.SourcePatientID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "source_patient_id inválido")
		return
	}

	if err := h.merge.Handle(r.Context(), command.MergePatientsCommand{
		TargetPatientID: sharedtypes.PatientID(targetID),
		SourcePatientID: sharedtypes.PatientID(sourceID),
		MergedBy:        claims.UserID,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Helpers ───────────────────────────────────────────────────────

func parsePatientID(r *http.Request) (sharedtypes.PatientID, error) {
	return uuid.Parse(chi.URLParam(r, "patientId"))
}

// ── PUT /patients/{patientId}/contact ────────────────────────────

type updateContactRequest struct {
	Phone          string `json:"phone"`
	Email          string `json:"email,omitempty"`
	WhatsApp       string `json:"whatsapp,omitempty"`
	EmergencyName  string `json:"emergency_name,omitempty"`
	EmergencyPhone string `json:"emergency_phone,omitempty"`
}

func (h *patientHTTPHandler) UpdateContact(w http.ResponseWriter, r *http.Request) {
	patientID, err := uuid.Parse(chi.URLParam(r, "patientId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patientId inválido")
		return
	}

	var req updateContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	if err := h.updateContact.Handle(r.Context(), command.UpdateContactInfoCommand{
		PatientID:      sharedtypes.PatientID(patientID),
		Phone:          req.Phone,
		Email:          req.Email,
		WhatsApp:       req.WhatsApp,
		EmergencyName:  req.EmergencyName,
		EmergencyPhone: req.EmergencyPhone,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
