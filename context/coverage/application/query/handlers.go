// Package query contiene los Query Handlers del bounded context Coverage.
package query

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/repository"
	"github.com/juantevez/odontoagenda/context/coverage/domain/service"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── CalculateCoverage ─────────────────────────────────────────────

// CalculateCoverageQuery es el query más importante del BC.
// Billing lo llama al generar el presupuesto de una cita.
type CalculateCoverageQuery struct {
	AgreementID     uuid.UUID
	PlanID          uuid.UUID
	ProcedureCode   string
	PatientID       sharedtypes.PatientID
	PatientAge      int
	AppointmentDate time.Time
	VisitsThisYear  int
}

type CalculateCoverageHandler struct {
	agreementRepo repository.AgreementRepository
	calculator    *service.CoverageCalculator
	cache         repository.CoverageCache
}

func NewCalculateCoverageHandler(
	agreementRepo repository.AgreementRepository,
	calculator *service.CoverageCalculator,
	cache repository.CoverageCache,
) *CalculateCoverageHandler {
	return &CalculateCoverageHandler{
		agreementRepo: agreementRepo,
		calculator:    calculator,
		cache:         cache,
	}
}

func (h *CalculateCoverageHandler) Handle(ctx context.Context, q CalculateCoverageQuery) (valueobject.CoverageResult, error) {
	// 1. Intentar desde cache (Redis) — solo para convenios no-Privado.
	//    El cache no sirve para Privado (siempre es el mismo resultado estático)
	//    pero tampoco hace daño. Preferimos no cachear para no consumir Redis.
	cached, err := h.cache.GetCoverageResult(ctx, q.PlanID, q.ProcedureCode)
	if err == nil && cached != nil {
		// El resultado cacheado no incluye validaciones dinámicas (carencia, topes,
		// edad). Solo cacheamos el resultado de cobertura base; las validaciones
		// dinámicas siempre se re-evalúan.
		// En el MVP cacheamos el resultado completo (simplificación aceptable).
		return *cached, nil
	}

	// 2. Cargar el Agreement.
	agreement, err := h.agreementRepo.FindByID(ctx, q.AgreementID)
	if err != nil {
		return valueobject.CoverageResult{}, err
	}

	// 3. Calcular.
	result, err := h.calculator.Calculate(ctx, service.CalculateInput{
		Agreement:       agreement,
		PlanID:          q.PlanID,
		ProcedureCode:   q.ProcedureCode,
		PatientID:       q.PatientID,
		PatientAge:      q.PatientAge,
		AppointmentDate: q.AppointmentDate,
		VisitsThisYear:  q.VisitsThisYear,
	})
	if err != nil {
		return valueobject.CoverageResult{}, err
	}

	// 4. Guardar en cache si el resultado es positivo.
	if result.IsCovered && !agreement.ProviderType().IsPrivado() {
		_ = h.cache.SetCoverageResult(ctx, q.PlanID, q.ProcedureCode, result)
	}

	return result, nil
}

// ── VerifyAffiliation ─────────────────────────────────────────────

type VerifyAffiliationQuery struct {
	AgreementID     uuid.UUID
	PlanID          uuid.UUID
	PatientID       sharedtypes.PatientID
	AppointmentDate time.Time
}

type VerifyAffiliationHandler struct {
	verifier *service.AffiliationVerifier
}

func NewVerifyAffiliationHandler(verifier *service.AffiliationVerifier) *VerifyAffiliationHandler {
	return &VerifyAffiliationHandler{verifier: verifier}
}

func (h *VerifyAffiliationHandler) Handle(ctx context.Context, q VerifyAffiliationQuery) (service.VerifyResult, error) {
	return h.verifier.Verify(ctx, service.VerifyInput{
		AgreementID:     q.AgreementID,
		PlanID:          q.PlanID,
		PatientID:       q.PatientID,
		AppointmentDate: q.AppointmentDate,
	})
}

// ── GetAgreement ──────────────────────────────────────────────────

type GetAgreementQuery struct {
	AgreementID uuid.UUID
}

// AgreementDTO es la vista de detalle de un convenio.
type AgreementDTO struct {
	ID                     string      `json:"id"`
	AgreementCode          string      `json:"agreement_code"`
	ProviderName           string      `json:"provider_name"`
	ProviderType           string      `json:"provider_type"`
	Status                 string      `json:"status"`
	ValidFrom              string      `json:"valid_from"`
	ValidUntil             *string     `json:"valid_until,omitempty"`
	ContactEmail           string      `json:"contact_email"`
	ContactPhone           string      `json:"contact_phone"`
	CancellationNoticeDays int         `json:"cancellation_notice_days"`
	Plans                  []PlanDTO   `json:"plans"`
}

type PlanDTO struct {
	ID                      string              `json:"id"`
	PlanCode                string              `json:"plan_code"`
	PlanName                string              `json:"plan_name"`
	CoPayType               string              `json:"co_pay_type"`
	CoPayValue              int                 `json:"co_pay_value"`
	RequiresPreAuthorization bool               `json:"requires_pre_authorization"`
	MaxAnnualVisits         *int                `json:"max_annual_visits,omitempty"`
	Status                  string              `json:"status"`
	CoveredProcedures       []ProcedureRuleDTO  `json:"covered_procedures"`
}

type ProcedureRuleDTO struct {
	ProcedureCode         string `json:"procedure_code"`
	CoveragePercent       int    `json:"coverage_percent"`
	RequiresAuthorization bool   `json:"requires_authorization"`
	MaxPerYear            *int   `json:"max_per_year,omitempty"`
	WaitingPeriodDays     int    `json:"waiting_period_days"`
	AgeMin                *int   `json:"age_min,omitempty"`
	AgeMax                *int   `json:"age_max,omitempty"`
}

type GetAgreementHandler struct {
	repo repository.AgreementRepository
}

func NewGetAgreementHandler(repo repository.AgreementRepository) *GetAgreementHandler {
	return &GetAgreementHandler{repo: repo}
}

func (h *GetAgreementHandler) Handle(ctx context.Context, q GetAgreementQuery) (*AgreementDTO, error) {
	agreement, err := h.repo.FindByID(ctx, q.AgreementID)
	if err != nil {
		return nil, err
	}
	return toAgreementDTO(agreement), nil
}

// ── ListAgreements ────────────────────────────────────────────────

type ListAgreementsQuery struct {
	ProviderType string
	Page         sharedtypes.Page
}

type ListAgreementsHandler struct {
	repo repository.AgreementRepository
}

func NewListAgreementsHandler(repo repository.AgreementRepository) *ListAgreementsHandler {
	return &ListAgreementsHandler{repo: repo}
}

func (h *ListAgreementsHandler) Handle(ctx context.Context, q ListAgreementsQuery) (sharedtypes.PagedResult[*AgreementDTO], error) {
	var result sharedtypes.PagedResult[*aggregate.Agreement]
	var err error

	if q.ProviderType != "" {
		pt, parseErr := valueobject.ParseProviderType(q.ProviderType)
		if parseErr != nil {
			return sharedtypes.PagedResult[*AgreementDTO]{}, sharederrors.NewInvalidArgument("provider_type", parseErr.Error())
		}
		result, err = h.repo.FindByProviderType(ctx, pt, q.Page)
	} else {
		result, err = h.repo.FindActive(ctx, q.Page)
	}
	if err != nil {
		return sharedtypes.PagedResult[*AgreementDTO]{}, err
	}

	dtos := make([]*AgreementDTO, len(result.Items))
	for i, a := range result.Items {
		dtos[i] = toAgreementDTO(a)
	}
	return sharedtypes.NewPagedResult(dtos, result.Total, q.Page), nil
}

// ── GetAuthorization ──────────────────────────────────────────────

type GetAuthorizationQuery struct {
	AuthorizationID uuid.UUID
}

type AuthorizationDTO struct {
	ID                string  `json:"id"`
	AgreementID       string  `json:"agreement_id"`
	PlanID            string  `json:"plan_id"`
	PatientID         string  `json:"patient_id"`
	MembershipNumber  string  `json:"membership_number"`
	ProcedureCode     string  `json:"procedure_code"`
	AppointmentID     *string `json:"appointment_id,omitempty"`
	RequestedAt       string  `json:"requested_at"`
	Status            string  `json:"status"`
	AuthorizationCode *string `json:"authorization_code,omitempty"`
	ExpiresAt         *string `json:"expires_at,omitempty"`
	RejectionReason   *string `json:"rejection_reason,omitempty"`
	ResolvedAt        *string `json:"resolved_at,omitempty"`
}

type GetAuthorizationHandler struct {
	repo repository.AuthorizationRepository
}

func NewGetAuthorizationHandler(repo repository.AuthorizationRepository) *GetAuthorizationHandler {
	return &GetAuthorizationHandler{repo: repo}
}

func (h *GetAuthorizationHandler) Handle(ctx context.Context, q GetAuthorizationQuery) (*AuthorizationDTO, error) {
	ar, err := h.repo.FindByID(ctx, q.AuthorizationID)
	if err != nil {
		return nil, err
	}
	return toAuthorizationDTO(ar), nil
}

// ── ListPendingAuthorizations ─────────────────────────────────────

type ListPendingAuthorizationsQuery struct {
	AgreementID uuid.UUID
}

type ListPendingAuthorizationsHandler struct {
	repo repository.AuthorizationRepository
}

func NewListPendingAuthorizationsHandler(repo repository.AuthorizationRepository) *ListPendingAuthorizationsHandler {
	return &ListPendingAuthorizationsHandler{repo: repo}
}

func (h *ListPendingAuthorizationsHandler) Handle(ctx context.Context, q ListPendingAuthorizationsQuery) ([]*AuthorizationDTO, error) {
	ars, err := h.repo.FindPendingByAgreement(ctx, q.AgreementID)
	if err != nil {
		return nil, err
	}
	dtos := make([]*AuthorizationDTO, len(ars))
	for i, ar := range ars {
		dtos[i] = toAuthorizationDTO(ar)
	}
	return dtos, nil
}

// ── DTO mappers ───────────────────────────────────────────────────

func toAgreementDTO(a *aggregate.Agreement) *AgreementDTO {
	dto := &AgreementDTO{
		ID:                     a.ID().String(),
		AgreementCode:          a.AgreementCode(),
		ProviderName:           a.ProviderName(),
		ProviderType:           a.ProviderType().String(),
		Status:                 a.Status().String(),
		ValidFrom:              a.ValidFrom().Format("2006-01-02"),
		ContactEmail:           a.ContactEmail().String(),
		ContactPhone:           a.ContactPhone().String(),
		CancellationNoticeDays: a.CancellationNoticeDays(),
	}
	if a.ValidUntil() != nil {
		s := a.ValidUntil().Format("2006-01-02")
		dto.ValidUntil = &s
	}
	for _, p := range a.Plans() {
		dto.Plans = append(dto.Plans, toPlanDTO(p))
	}
	return dto
}

func toPlanDTO(p aggregate.Plan) PlanDTO {
	dto := PlanDTO{
		ID:                      p.ID().String(),
		PlanCode:                p.PlanCode(),
		PlanName:                p.PlanName(),
		CoPayType:               p.CoPayType().String(),
		CoPayValue:              p.CoPayValue(),
		RequiresPreAuthorization: p.RequiresPreAuthorization(),
		MaxAnnualVisits:         p.MaxAnnualVisits(),
		Status:                  p.Status().String(),
	}
	for _, r := range p.CoveredProcedures() {
		dto.CoveredProcedures = append(dto.CoveredProcedures, ProcedureRuleDTO{
			ProcedureCode:         r.ProcedureCode,
			CoveragePercent:       r.CoveragePercent,
			RequiresAuthorization: r.RequiresAuthorization,
			MaxPerYear:            r.MaxPerYear,
			WaitingPeriodDays:     r.WaitingPeriodDays,
			AgeMin:                r.AgeMin,
			AgeMax:                r.AgeMax,
		})
	}
	return dto
}

func toAuthorizationDTO(ar *aggregate.AuthorizationRequest) *AuthorizationDTO {
	dto := &AuthorizationDTO{
		ID:               ar.ID().String(),
		AgreementID:      ar.AgreementID().String(),
		PlanID:           ar.PlanID().String(),
		PatientID:        ar.PatientID().String(),
		MembershipNumber: ar.MembershipNumber(),
		ProcedureCode:    ar.ProcedureCode(),
		RequestedAt:      ar.RequestedAt().Format(time.RFC3339),
		Status:           ar.Status().String(),
		AuthorizationCode: ar.AuthorizationCode(),
		RejectionReason:  ar.RejectionReason(),
	}
	if ar.AppointmentID() != nil {
		s := ar.AppointmentID().String()
		dto.AppointmentID = &s
	}
	if ar.ExpiresAt() != nil {
		s := ar.ExpiresAt().Format(time.RFC3339)
		dto.ExpiresAt = &s
	}
	if ar.ResolvedAt() != nil {
		s := ar.ResolvedAt().Format(time.RFC3339)
		dto.ResolvedAt = &s
	}
	return dto
}
