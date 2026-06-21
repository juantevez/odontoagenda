// Package http contiene los adaptadores de entrada HTTP del bounded context Coverage.
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	coveragecmd "github.com/juantevez/odontoagenda/context/coverage/application/command"
	coverageqry "github.com/juantevez/odontoagenda/context/coverage/application/query"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// RegisterRoutes monta todas las rutas del contexto Coverage.
func RegisterRoutes(
	r chi.Router,
	jwtCfg middleware.JWTConfig,
	// Commands
	createAgreement      *coveragecmd.CreateAgreementHandler,
	addPlan              *coveragecmd.AddPlanHandler,
	upsertRule           *coveragecmd.UpsertProcedureRuleHandler,
	updateStatus         *coveragecmd.UpdateAgreementStatusHandler,
	requestAuthorization *coveragecmd.RequestAuthorizationHandler,
	resolveAuthorization *coveragecmd.ResolveAuthorizationHandler,
	// Queries
	getAgreement         *coverageqry.GetAgreementHandler,
	listAgreements       *coverageqry.ListAgreementsHandler,
	calculateCoverage    *coverageqry.CalculateCoverageHandler,
	verifyAffiliation    *coverageqry.VerifyAffiliationHandler,
	getAuthorization     *coverageqry.GetAuthorizationHandler,
	listPending          *coverageqry.ListPendingAuthorizationsHandler,
) {
	h := &coverageHTTPHandler{
		createAgreement:      createAgreement,
		addPlan:              addPlan,
		upsertRule:           upsertRule,
		updateStatus:         updateStatus,
		requestAuthorization: requestAuthorization,
		resolveAuthorization: resolveAuthorization,
		getAgreement:         getAgreement,
		listAgreements:       listAgreements,
		calculateCoverage:    calculateCoverage,
		verifyAffiliation:    verifyAffiliation,
		getAuthorization:     getAuthorization,
		listPending:          listPending,
		logger:               slog.Default().With("adapter", "coverage.http"),
	}

	r.Group(func(r chi.Router) {
		r.Use(middleware.JWT(jwtCfg))

		// ── Convenios ─────────────────────────────────────────────
		r.Post("/agreements", h.CreateAgreement)
		r.Get("/agreements", h.ListAgreements)
		r.Get("/agreements/{agreementId}", h.GetAgreement)
		r.Patch("/agreements/{agreementId}/status", h.UpdateAgreementStatus)

		// Planes y prestaciones (solo admin)
		r.Post("/agreements/{agreementId}/plans",
			middleware.RequireRoles(middleware.RoleClinicAdmin, middleware.RoleSuperAdmin)(
				http.HandlerFunc(h.AddPlan)).ServeHTTP,
		)
		r.Put("/agreements/{agreementId}/plans/{planId}/procedures",
			middleware.RequireRoles(middleware.RoleClinicAdmin, middleware.RoleSuperAdmin)(
				http.HandlerFunc(h.UpsertProcedureRule)).ServeHTTP,
		)

		// ── Cálculo de cobertura (consumido por Billing) ──────────
		r.Get("/coverage/calculate", h.CalculateCoverage)
		r.Get("/coverage/verify-affiliation", h.VerifyAffiliation)

		// ── Autorizaciones ────────────────────────────────────────
		r.Post("/authorizations", h.RequestAuthorization)
		r.Get("/authorizations/pending", h.ListPendingAuthorizations)
		r.Get("/authorizations/{authorizationId}", h.GetAuthorization)
		r.Patch("/authorizations/{authorizationId}/resolve",
			middleware.RequireRoles(middleware.RoleClinicAdmin, middleware.RoleSuperAdmin)(
				http.HandlerFunc(h.ResolveAuthorization)).ServeHTTP,
		)
	})
}

// ── Handler struct ────────────────────────────────────────────────

type coverageHTTPHandler struct {
	createAgreement      *coveragecmd.CreateAgreementHandler
	addPlan              *coveragecmd.AddPlanHandler
	upsertRule           *coveragecmd.UpsertProcedureRuleHandler
	updateStatus         *coveragecmd.UpdateAgreementStatusHandler
	requestAuthorization *coveragecmd.RequestAuthorizationHandler
	resolveAuthorization *coveragecmd.ResolveAuthorizationHandler
	getAgreement         *coverageqry.GetAgreementHandler
	listAgreements       *coverageqry.ListAgreementsHandler
	calculateCoverage    *coverageqry.CalculateCoverageHandler
	verifyAffiliation    *coverageqry.VerifyAffiliationHandler
	getAuthorization     *coverageqry.GetAuthorizationHandler
	listPending          *coverageqry.ListPendingAuthorizationsHandler
	logger               *slog.Logger
}

// ── POST /agreements ──────────────────────────────────────────────

type createAgreementRequest struct {
	AgreementCode          string  `json:"agreement_code"`
	ProviderName           string  `json:"provider_name"`
	ProviderType           string  `json:"provider_type"`
	ValidFrom              string  `json:"valid_from"`  // YYYY-MM-DD
	ValidUntil             *string `json:"valid_until,omitempty"`
	ContactEmail           string  `json:"contact_email"`
	ContactPhone           string  `json:"contact_phone"`
	CancellationNoticeDays int     `json:"cancellation_notice_days"`
	// Primer plan (obligatorio para no-Privado)
	FirstPlanCode            string `json:"first_plan_code,omitempty"`
	FirstPlanName            string `json:"first_plan_name,omitempty"`
	FirstPlanCoPayType       string `json:"first_plan_co_pay_type,omitempty"`
	FirstPlanCoPayValue      int    `json:"first_plan_co_pay_value,omitempty"`
	FirstPlanRequiresPreAuth bool   `json:"first_plan_requires_pre_authorization,omitempty"`
	FirstPlanMaxAnnualVisits *int   `json:"first_plan_max_annual_visits,omitempty"`
}

func (h *coverageHTTPHandler) CreateAgreement(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}
	if !claims.HasRole(middleware.RoleClinicAdmin, middleware.RoleSuperAdmin) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "rol insuficiente")
		return
	}

	var req createAgreementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	validFrom, err := time.Parse("2006-01-02", req.ValidFrom)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "valid_from debe ser YYYY-MM-DD")
		return
	}

	cmd := coveragecmd.CreateAgreementCommand{
		AgreementCode:          req.AgreementCode,
		ProviderName:           req.ProviderName,
		ProviderType:           req.ProviderType,
		ValidFrom:              validFrom,
		ContactEmail:           req.ContactEmail,
		ContactPhone:           req.ContactPhone,
		CancellationNoticeDays: req.CancellationNoticeDays,
		FirstPlanCode:          req.FirstPlanCode,
		FirstPlanName:          req.FirstPlanName,
		FirstPlanCoPayType:     req.FirstPlanCoPayType,
		FirstPlanCoPayValue:    req.FirstPlanCoPayValue,
		FirstPlanRequiresPreAuth: req.FirstPlanRequiresPreAuth,
		FirstPlanMaxAnnualVisits: req.FirstPlanMaxAnnualVisits,
		CreatedBy:              &claims.UserID,
	}

	if req.ValidUntil != nil {
		t, err := time.Parse("2006-01-02", *req.ValidUntil)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "valid_until debe ser YYYY-MM-DD")
			return
		}
		cmd.ValidUntil = &t
	}

	id, err := h.createAgreement.Handle(r.Context(), cmd)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"agreement_id": id.String()})
}

// ── GET /agreements ───────────────────────────────────────────────

func (h *coverageHTTPHandler) ListAgreements(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	result, err := h.listAgreements.Handle(r.Context(), coverageqry.ListAgreementsQuery{
		ProviderType: r.URL.Query().Get("provider_type"),
		Page:         sharedtypes.NewPage(limit, offset),
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── GET /agreements/:id ───────────────────────────────────────────

func (h *coverageHTTPHandler) GetAgreement(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "agreementId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "agreementId inválido")
		return
	}
	dto, err := h.getAgreement.Handle(r.Context(), coverageqry.GetAgreementQuery{AgreementID: id})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// ── PATCH /agreements/:id/status ──────────────────────────────────

type updateStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func (h *coverageHTTPHandler) UpdateAgreementStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil || !claims.HasRole(middleware.RoleSuperAdmin) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "solo superadmin puede cambiar el estado de un convenio")
		return
	}

	id, err := parseUUID(r, "agreementId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "agreementId inválido")
		return
	}

	var req updateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	if err := h.updateStatus.Handle(r.Context(), coveragecmd.UpdateAgreementStatusCommand{
		AgreementID: id,
		NewStatus:   req.Status,
		Reason:      req.Reason,
		UpdatedBy:   claims.UserID,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── POST /agreements/:id/plans ────────────────────────────────────

type addPlanRequest struct {
	PlanCode        string `json:"plan_code"`
	PlanName        string `json:"plan_name"`
	CoPayType       string `json:"co_pay_type"`
	CoPayValue      int    `json:"co_pay_value"`
	RequiresPreAuth bool   `json:"requires_pre_authorization"`
	MaxAnnualVisits *int   `json:"max_annual_visits,omitempty"`
}

func (h *coverageHTTPHandler) AddPlan(w http.ResponseWriter, r *http.Request) {
	agreementID, err := parseUUID(r, "agreementId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "agreementId inválido")
		return
	}

	var req addPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	planID, err := h.addPlan.Handle(r.Context(), coveragecmd.AddPlanCommand{
		AgreementID:     agreementID,
		PlanCode:        req.PlanCode,
		PlanName:        req.PlanName,
		CoPayType:       req.CoPayType,
		CoPayValue:      req.CoPayValue,
		RequiresPreAuth: req.RequiresPreAuth,
		MaxAnnualVisits: req.MaxAnnualVisits,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"plan_id": planID.String()})
}

// ── PUT /agreements/:id/plans/:planId/procedures ──────────────────

type upsertProcedureRuleRequest struct {
	ProcedureCode         string `json:"procedure_code"`
	CoveragePercent       int    `json:"coverage_percent"`
	RequiresAuthorization bool   `json:"requires_authorization"`
	MaxPerYear            *int   `json:"max_per_year,omitempty"`
	WaitingPeriodDays     int    `json:"waiting_period_days"`
	AgeMin                *int   `json:"age_min,omitempty"`
	AgeMax                *int   `json:"age_max,omitempty"`
}

func (h *coverageHTTPHandler) UpsertProcedureRule(w http.ResponseWriter, r *http.Request) {
	agreementID, err := parseUUID(r, "agreementId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "agreementId inválido")
		return
	}
	planID, err := parseUUID(r, "planId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "planId inválido")
		return
	}

	var req upsertProcedureRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	if err := h.upsertRule.Handle(r.Context(), coveragecmd.UpsertProcedureRuleCommand{
		AgreementID:           agreementID,
		PlanID:                planID,
		ProcedureCode:         req.ProcedureCode,
		CoveragePercent:       req.CoveragePercent,
		RequiresAuthorization: req.RequiresAuthorization,
		MaxPerYear:            req.MaxPerYear,
		WaitingPeriodDays:     req.WaitingPeriodDays,
		AgeMin:                req.AgeMin,
		AgeMax:                req.AgeMax,
	}); err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── GET /coverage/calculate ───────────────────────────────────────

func (h *coverageHTTPHandler) CalculateCoverage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	agreementID, err := uuid.Parse(q.Get("agreement_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "agreement_id inválido")
		return
	}
	planID, err := uuid.Parse(q.Get("plan_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "plan_id inválido")
		return
	}
	patientID, err := uuid.Parse(q.Get("patient_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patient_id inválido")
		return
	}

	appointmentDate := time.Now().UTC()
	if d := q.Get("appointment_date"); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			appointmentDate = t
		}
	}

	patientAge, _ := strconv.Atoi(q.Get("patient_age"))
	visitsThisYear, _ := strconv.Atoi(q.Get("visits_this_year"))

	result, err := h.calculateCoverage.Handle(r.Context(), coverageqry.CalculateCoverageQuery{
		AgreementID:     agreementID,
		PlanID:          planID,
		ProcedureCode:   q.Get("procedure_code"),
		PatientID:       sharedtypes.PatientID(patientID),
		PatientAge:      patientAge,
		AppointmentDate: appointmentDate,
		VisitsThisYear:  visitsThisYear,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── GET /coverage/verify-affiliation ─────────────────────────────

func (h *coverageHTTPHandler) VerifyAffiliation(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	agreementID, err := uuid.Parse(q.Get("agreement_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "agreement_id inválido")
		return
	}
	planID, err := uuid.Parse(q.Get("plan_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "plan_id inválido")
		return
	}
	patientID, err := uuid.Parse(q.Get("patient_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patient_id inválido")
		return
	}

	appointmentDate := time.Now().UTC()
	if d := q.Get("appointment_date"); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			appointmentDate = t
		}
	}

	result, err := h.verifyAffiliation.Handle(r.Context(), coverageqry.VerifyAffiliationQuery{
		AgreementID:     agreementID,
		PlanID:          planID,
		PatientID:       sharedtypes.PatientID(patientID),
		AppointmentDate: appointmentDate,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── POST /authorizations ──────────────────────────────────────────

type requestAuthorizationRequest struct {
	AgreementID      string  `json:"agreement_id"`
	PlanID           string  `json:"plan_id"`
	PatientID        string  `json:"patient_id"`
	MembershipNumber string  `json:"membership_number"`
	ProcedureCode    string  `json:"procedure_code"`
	AppointmentID    *string `json:"appointment_id,omitempty"`
}

func (h *coverageHTTPHandler) RequestAuthorization(w http.ResponseWriter, r *http.Request) {
	var req requestAuthorizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	agreementID, err := uuid.Parse(req.AgreementID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "agreement_id inválido")
		return
	}
	planID, err := uuid.Parse(req.PlanID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "plan_id inválido")
		return
	}
	patientID, err := uuid.Parse(req.PatientID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "patient_id inválido")
		return
	}

	cmd := coveragecmd.RequestAuthorizationCommand{
		AgreementID:      agreementID,
		PlanID:           planID,
		PatientID:        sharedtypes.PatientID(patientID),
		MembershipNumber: req.MembershipNumber,
		ProcedureCode:    req.ProcedureCode,
	}
	if req.AppointmentID != nil {
		if apptID, err := uuid.Parse(*req.AppointmentID); err == nil {
			cmd.AppointmentID = &apptID
		}
	}

	id, err := h.requestAuthorization.Handle(r.Context(), cmd)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"authorization_id": id.String()})
}

// ── GET /authorizations/:id ───────────────────────────────────────

func (h *coverageHTTPHandler) GetAuthorization(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "authorizationId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "authorizationId inválido")
		return
	}
	dto, err := h.getAuthorization.Handle(r.Context(), coverageqry.GetAuthorizationQuery{AuthorizationID: id})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// ── GET /authorizations/pending ───────────────────────────────────

func (h *coverageHTTPHandler) ListPendingAuthorizations(w http.ResponseWriter, r *http.Request) {
	agreementID, err := uuid.Parse(r.URL.Query().Get("agreement_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "agreement_id inválido")
		return
	}
	dtos, err := h.listPending.Handle(r.Context(), coverageqry.ListPendingAuthorizationsQuery{AgreementID: agreementID})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtos)
}

// ── PATCH /authorizations/:id/resolve ────────────────────────────

type resolveAuthorizationRequest struct {
	Status            string `json:"status"`
	AuthorizationCode string `json:"authorization_code,omitempty"`
	RejectionReason   string `json:"rejection_reason,omitempty"`
}

func (h *coverageHTTPHandler) ResolveAuthorization(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autenticado")
		return
	}
	id, err := parseUUID(r, "authorizationId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "authorizationId inválido")
		return
	}

	var req resolveAuthorizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cuerpo inválido")
		return
	}

	if err := h.resolveAuthorization.Handle(r.Context(), coveragecmd.ResolveAuthorizationCommand{
		AuthorizationID:   id,
		Status:            req.Status,
		AuthorizationCode: req.AuthorizationCode,
		RejectionReason:   req.RejectionReason,
		ResolvedBy:        claims.UserID,
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
