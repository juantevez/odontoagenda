// Package service contiene los Domain Services del bounded context Coverage.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/repository"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── CoverageCalculator — Domain Service ──────────────────────────

// CoverageCalculator evalúa las reglas de un Plan para determinar
// si un procedimiento está cubierto y bajo qué condiciones.
//
// Es Domain Service porque coordina Agreement, Plan, ProcedureRule y
// PatientAffiliation — información que cruza múltiples entidades.
//
// Algoritmo (en orden):
//  1. Fast-path Privado: sin reglas, 100% de bolsillo.
//  2. Verificar vigencia del Agreement en la fecha de la prestación.
//  3. Verificar que el Plan esté Active.
//  4. Buscar ProcedureRule para el procedureCode.
//  5. Verificar afiliación activa del paciente al plan.
//  6. Verificar carencia (waitingPeriodDays).
//  7. Verificar restricciones de edad (ageMin, ageMax).
//  8. Verificar tope anual de visitas (maxPerYear).
//  9. Calcular copago efectivo (override del procedimiento o base del plan).
// 10. Determinar si requiere autorización (Plan.requiresPreAuth OR Rule.requiresAuthorization).
type CoverageCalculator struct {
	affiliationRepo repository.PatientAffiliationRepository
}

func NewCoverageCalculator(affiliationRepo repository.PatientAffiliationRepository) *CoverageCalculator {
	return &CoverageCalculator{affiliationRepo: affiliationRepo}
}

// CalculateInput agrupa los parámetros de entrada del cálculo.
type CalculateInput struct {
	Agreement       *aggregate.Agreement
	PlanID          uuid.UUID
	ProcedureCode   string
	PatientID       sharedtypes.PatientID
	PatientAge      int
	AppointmentDate time.Time
	// VisitsThisYear: cuántas veces ya se realizó este procedimiento en el año actual.
	// El caller (Billing) es responsable de proveer este dato.
	VisitsThisYear int
}

// Calculate ejecuta el algoritmo de evaluación de cobertura.
func (c *CoverageCalculator) Calculate(ctx context.Context, input CalculateInput) (valueobject.CoverageResult, error) {

	// ── Paso 1: Fast-path para convenios Privado ──────────────────
	if input.Agreement.ProviderType().IsPrivado() {
		return valueobject.PrivadoResult(), nil
	}

	// ── Paso 2: Vigencia del Agreement ───────────────────────────
	if !input.Agreement.IsValidAt(input.AppointmentDate) {
		return valueobject.NotCovered(
			fmt.Sprintf("el convenio '%s' no está vigente en la fecha %s",
				input.Agreement.AgreementCode(),
				input.AppointmentDate.Format("2006-01-02"),
			)), nil
	}

	// ── Paso 3: Plan activo ───────────────────────────────────────
	plan, ok := input.Agreement.FindPlan(input.PlanID)
	if !ok {
		return valueobject.NotCovered("plan no encontrado en el convenio"), nil
	}
	if !plan.Status().IsActive() {
		return valueobject.NotCovered(
			fmt.Sprintf("el plan '%s' está discontinuado", plan.PlanCode())), nil
	}

	// ── Paso 4: Buscar ProcedureRule ──────────────────────────────
	rule, found := plan.FindProcedureRule(input.ProcedureCode)
	if !found {
		return valueobject.NotCovered(
			fmt.Sprintf("el procedimiento '%s' no está cubierto por el plan '%s'",
				input.ProcedureCode, plan.PlanCode())), nil
	}

	// ── Paso 5: Afiliación activa ─────────────────────────────────
	affiliation, err := c.affiliationRepo.FindActive(ctx, input.PatientID, input.PlanID)
	if err != nil || affiliation == nil {
		return valueobject.NotCovered("el paciente no tiene afiliación activa a este plan"), nil
	}

	// ── Paso 6: Carencia ──────────────────────────────────────────
	if rule.WaitingPeriodDays > 0 {
		waitingEnd := affiliation.AffiliatedSince.AddDate(0, 0, rule.WaitingPeriodDays)
		if input.AppointmentDate.Before(waitingEnd) {
			return valueobject.NotCovered(
				fmt.Sprintf("el procedimiento '%s' está en período de carencia hasta %s",
					input.ProcedureCode, waitingEnd.Format("02/01/2006"))), nil
		}
	}

	// ── Paso 7: Restricciones de edad ─────────────────────────────
	if rule.AgeMin != nil && input.PatientAge < *rule.AgeMin {
		return valueobject.NotCovered(
			fmt.Sprintf("el procedimiento requiere edad mínima de %d años", *rule.AgeMin)), nil
	}
	if rule.AgeMax != nil && input.PatientAge > *rule.AgeMax {
		return valueobject.NotCovered(
			fmt.Sprintf("el procedimiento requiere edad máxima de %d años", *rule.AgeMax)), nil
	}

	// ── Paso 8: Tope anual de visitas ─────────────────────────────
	if rule.MaxPerYear != nil && input.VisitsThisYear >= *rule.MaxPerYear {
		return valueobject.NotCovered(
			fmt.Sprintf("se alcanzó el tope anual de %d prestaciones para '%s'",
				*rule.MaxPerYear, input.ProcedureCode)), nil
	}

	// ── Paso 9: Copago efectivo ───────────────────────────────────
	coPayType, coPayValue := effectiveCoPay(plan, rule)

	// ── Paso 10: ¿Requiere autorización? ─────────────────────────
	requiresAuth := plan.RequiresPreAuthorization() || rule.RequiresAuthorization

	return valueobject.CoverageResult{
		IsCovered:             true,
		CoveragePercent:       rule.CoveragePercent,
		CoPayType:             coPayType,
		CoPayValue:            coPayValue,
		RequiresAuthorization: requiresAuth,
	}, nil
}

// ── effectiveCoPay calcula el copago efectivo ─────────────────────
// Prioridad: override del procedimiento > copago base del plan.
func effectiveCoPay(plan *aggregate.Plan, rule *aggregate.ProcedureRule) (valueobject.CoPayType, int) {
	if rule.CoPayOverride != nil {
		return rule.CoPayOverride.CoPayType, rule.CoPayOverride.CoPayValue
	}
	return plan.CoPayType(), plan.CoPayValue()
}

// ── AffiliationVerifier — Domain Service ─────────────────────────

// AffiliationVerifier verifica la vigencia de la afiliación de un paciente
// a un plan, sin calcular la cobertura completa.
// Usado por Scheduling al confirmar un turno.
type AffiliationVerifier struct {
	agreementRepo   repository.AgreementRepository
	affiliationRepo repository.PatientAffiliationRepository
}

func NewAffiliationVerifier(
	agreementRepo repository.AgreementRepository,
	affiliationRepo repository.PatientAffiliationRepository,
) *AffiliationVerifier {
	return &AffiliationVerifier{
		agreementRepo:   agreementRepo,
		affiliationRepo: affiliationRepo,
	}
}

// VerifyInput agrupa los parámetros de verificación.
type VerifyInput struct {
	AgreementID     uuid.UUID
	PlanID          uuid.UUID
	PatientID       sharedtypes.PatientID
	AppointmentDate time.Time
}

// VerifyResult es el resultado de la verificación de afiliación.
type VerifyResult struct {
	Status valueobject.AffiliationStatus
	Reason string
}

// Verify evalúa si la afiliación del paciente al plan está activa y vigente.
func (v *AffiliationVerifier) Verify(ctx context.Context, input VerifyInput) (VerifyResult, error) {
	// Verificar que el Agreement está vigente.
	agreement, err := v.agreementRepo.FindByID(ctx, input.AgreementID)
	if err != nil {
		return VerifyResult{Status: valueobject.AffiliationStatusCancelled, Reason: "convenio no encontrado"}, nil
	}
	if !agreement.IsValidAt(input.AppointmentDate) {
		return VerifyResult{
			Status: valueobject.AffiliationStatusSuspended,
			Reason: "el convenio no está vigente en la fecha del turno",
		}, nil
	}

	// Verificar que el Plan existe y está activo.
	plan, ok := agreement.FindPlan(input.PlanID)
	if !ok || !plan.Status().IsActive() {
		return VerifyResult{
			Status: valueobject.AffiliationStatusCancelled,
			Reason: "el plan no está disponible",
		}, nil
	}

	// Verificar afiliación del paciente.
	affiliation, err := v.affiliationRepo.FindActive(ctx, input.PatientID, input.PlanID)
	if err != nil || affiliation == nil {
		return VerifyResult{
			Status: valueobject.AffiliationStatusCancelled,
			Reason: "el paciente no tiene afiliación activa a este plan",
		}, nil
	}

	return VerifyResult{Status: valueobject.AffiliationStatusActive}, nil
}
