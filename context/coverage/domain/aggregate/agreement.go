// Package aggregate contiene los Aggregates del bounded context Coverage & Agreements.
//
// Aggregate Root principal: Agreement
// Entidad interna:          Plan (con sus ProcedureRules)
package aggregate

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/event"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── ProcedureRule — Value Object dentro de Plan ───────────────────

// ProcedureRule define las condiciones de cobertura para un procedimiento
// específico dentro de un Plan.
type ProcedureRule struct {
	ProcedureCode         string
	CoveragePercent       int    // % cubierto por la prepaga (0-100)
	CoPayOverride         *valueobject.CoPayOverride
	RequiresAuthorization bool
	MaxPerYear            *int   // nil = ilimitado
	MaxAmountCents        *int64 // nil = sin tope por prestación
	WaitingPeriodDays     int    // 0 = sin carencia
	AgeMin                *int   // nil = sin restricción
	AgeMax                *int   // nil = sin restricción
}

// Validate verifica las invariantes del ProcedureRule.
func (r ProcedureRule) Validate() error {
	if strings.TrimSpace(r.ProcedureCode) == "" {
		return sharederrors.NewInvalidArgument("procedure_code", "requerido")
	}
	if r.CoveragePercent < 0 || r.CoveragePercent > 100 {
		return sharederrors.NewInvalidArgument("coverage_percent", "debe estar entre 0 y 100")
	}
	if r.WaitingPeriodDays < 0 {
		return sharederrors.NewInvalidArgument("waiting_period_days", "no puede ser negativo")
	}
	if r.CoPayOverride != nil {
		if r.CoPayOverride.CoPayType == valueobject.CoPayTypePercent {
			total := r.CoveragePercent + r.CoPayOverride.CoPayValue
			if total > 100 {
				return sharederrors.NewInvalidArgument("co_pay_override",
					fmt.Sprintf("coveragePercent (%d) + coPayPercent (%d) supera 100",
						r.CoveragePercent, r.CoPayOverride.CoPayValue))
			}
		}
	}
	return nil
}

// ── Plan — Entidad dentro de Agreement ───────────────────────────

// Plan representa un nivel de cobertura dentro de un Agreement.
// Un Agreement puede tener múltiples Planes (ej: OSDE 210, OSDE 310, OSDE 410).
//
// Invariantes:
//   - No pueden existir dos ProcedureRule con el mismo ProcedureCode.
//   - Un Plan Discontinued no admite nuevas ProcedureRules.
//   - Si CoPayType = None, CoPayValue debe ser 0.
//   - CoveragePercent + CoPayPercent no puede superar 100.
type Plan struct {
	id                      uuid.UUID
	planCode                string
	planName                string
	coPayType               valueobject.CoPayType
	coPayValue              int
	requiresPreAuthorization bool
	maxAnnualVisits         *int
	coveredProcedures       []ProcedureRule
	status                  valueobject.PlanStatus
	createdAt               time.Time
	updatedAt               time.Time
}

func NewPlan(
	planCode, planName string,
	coPayType valueobject.CoPayType,
	coPayValue int,
	requiresPreAuthorization bool,
	maxAnnualVisits *int,
) (*Plan, error) {
	if strings.TrimSpace(planCode) == "" {
		return nil, sharederrors.NewInvalidArgument("plan_code", "requerido")
	}
	if strings.TrimSpace(planName) == "" {
		return nil, sharederrors.NewInvalidArgument("plan_name", "requerido")
	}
	if coPayType == valueobject.CoPayTypeNone && coPayValue != 0 {
		return nil, sharederrors.NewInvalidArgument("co_pay_value",
			"debe ser 0 cuando co_pay_type es None")
	}
	if coPayType == valueobject.CoPayTypePercent && (coPayValue < 0 || coPayValue > 100) {
		return nil, sharederrors.NewInvalidArgument("co_pay_value",
			"debe estar entre 0 y 100 para copago porcentual")
	}

	now := time.Now().UTC()
	return &Plan{
		id:                      uuid.New(),
		planCode:                strings.TrimSpace(planCode),
		planName:                strings.TrimSpace(planName),
		coPayType:               coPayType,
		coPayValue:              coPayValue,
		requiresPreAuthorization: requiresPreAuthorization,
		maxAnnualVisits:         maxAnnualVisits,
		coveredProcedures:       []ProcedureRule{},
		status:                  valueobject.PlanStatusActive,
		createdAt:               now,
		updatedAt:               now,
	}, nil
}

// UpsertProcedureRule agrega o actualiza una ProcedureRule dentro del Plan.
// INV: un Plan Discontinued no puede recibir nuevas reglas.
func (p *Plan) UpsertProcedureRule(rule ProcedureRule) error {
	if !p.status.IsActive() {
		return sharederrors.NewPrecondition("plan_active",
			fmt.Sprintf("el plan '%s' está discontinuado y no admite nuevas prestaciones", p.planCode))
	}
	if err := rule.Validate(); err != nil {
		return err
	}
	// Verificar INV-04 con el copago base del plan si no hay override.
	if rule.CoPayOverride == nil && p.coPayType == valueobject.CoPayTypePercent {
		if rule.CoveragePercent+p.coPayValue > 100 {
			return sharederrors.NewInvalidArgument("coverage_percent",
				fmt.Sprintf("coveragePercent (%d) + coPayPlan (%d) supera 100",
					rule.CoveragePercent, p.coPayValue))
		}
	}

	// Upsert: reemplazar si ya existe el mismo código.
	for i, existing := range p.coveredProcedures {
		if existing.ProcedureCode == rule.ProcedureCode {
			p.coveredProcedures[i] = rule
			p.updatedAt = time.Now().UTC()
			return nil
		}
	}
	p.coveredProcedures = append(p.coveredProcedures, rule)
	p.updatedAt = time.Now().UTC()
	return nil
}

// FindProcedureRule busca la regla de cobertura para un procedimiento.
func (p *Plan) FindProcedureRule(procedureCode string) (*ProcedureRule, bool) {
	for i := range p.coveredProcedures {
		if p.coveredProcedures[i].ProcedureCode == procedureCode {
			return &p.coveredProcedures[i], true
		}
	}
	return nil, false
}

// Discontinue marca el plan como discontinuado.
func (p *Plan) Discontinue() error {
	if !p.status.IsActive() {
		return sharederrors.NewPrecondition("plan_active", "el plan ya está discontinuado")
	}
	p.status = valueobject.PlanStatusDiscontinued
	p.updatedAt = time.Now().UTC()
	return nil
}

// Getters de Plan
func (p *Plan) ID() uuid.UUID                           { return p.id }
func (p *Plan) PlanCode() string                        { return p.planCode }
func (p *Plan) PlanName() string                        { return p.planName }
func (p *Plan) CoPayType() valueobject.CoPayType        { return p.coPayType }
func (p *Plan) CoPayValue() int                         { return p.coPayValue }
func (p *Plan) RequiresPreAuthorization() bool          { return p.requiresPreAuthorization }
func (p *Plan) MaxAnnualVisits() *int                   { return p.maxAnnualVisits }
func (p *Plan) CoveredProcedures() []ProcedureRule      { return p.coveredProcedures }
func (p *Plan) Status() valueobject.PlanStatus          { return p.status }
func (p *Plan) CreatedAt() time.Time                    { return p.createdAt }
func (p *Plan) UpdatedAt() time.Time                    { return p.updatedAt }

// ── Agreement — Aggregate Root ────────────────────────────────────

// Agreement representa el convenio entre OdontoAgenda y una entidad financiadora.
//
// Invariantes:
//   - Un Agreement Active debe tener al menos 1 Plan Active.
//   - No pueden existir dos Planes con el mismo PlanCode dentro del mismo Agreement.
//   - validUntil, si está presente, siempre debe ser > validFrom.
type Agreement struct {
	id                     uuid.UUID
	agreementCode          string
	providerName           string
	providerType           valueobject.ProviderType
	status                 valueobject.AgreementStatus
	validFrom              time.Time
	validUntil             *time.Time
	contactEmail           sharedvo.Email
	contactPhone           sharedvo.PhoneNumber
	cancellationNoticeDays int
	plans                  []Plan
	createdAt              time.Time
	updatedAt              time.Time
	createdBy              *uuid.UUID
	version                int64
	pendingEvents          []event.DomainEvent
}

// NewAgreement crea un Agreement con un primer Plan inicial.
func NewAgreement(
	agreementCode, providerName string,
	providerType valueobject.ProviderType,
	validFrom time.Time,
	validUntil *time.Time,
	contactEmail sharedvo.Email,
	contactPhone sharedvo.PhoneNumber,
	cancellationNoticeDays int,
	firstPlan *Plan,
	createdBy *uuid.UUID,
) (*Agreement, error) {
	if strings.TrimSpace(agreementCode) == "" {
		return nil, sharederrors.NewInvalidArgument("agreement_code", "requerido")
	}
	if strings.TrimSpace(providerName) == "" {
		return nil, sharederrors.NewInvalidArgument("provider_name", "requerido")
	}
	if validUntil != nil && !validUntil.After(validFrom) {
		return nil, sharederrors.NewInvalidArgument("valid_until",
			"debe ser posterior a valid_from")
	}
	if cancellationNoticeDays < 0 {
		return nil, sharederrors.NewInvalidArgument("cancellation_notice_days",
			"no puede ser negativo")
	}

	// ProviderTypePrivado: no requiere plan ni email de contacto.
	plans := []Plan{}
	if firstPlan != nil {
		plans = append(plans, *firstPlan)
	} else if !providerType.IsPrivado() {
		return nil, sharederrors.NewInvalidArgument("first_plan",
			"un convenio no-Privado requiere al menos un plan inicial")
	}

	now := time.Now().UTC()
	a := &Agreement{
		id:                     uuid.New(),
		agreementCode:          strings.TrimSpace(agreementCode),
		providerName:           strings.TrimSpace(providerName),
		providerType:           providerType,
		status:                 valueobject.AgreementStatusActive,
		validFrom:              validFrom,
		validUntil:             validUntil,
		contactEmail:           contactEmail,
		contactPhone:           contactPhone,
		cancellationNoticeDays: cancellationNoticeDays,
		plans:                  plans,
		createdAt:              now,
		updatedAt:              now,
		createdBy:              createdBy,
		version:                1,
	}

	a.pendingEvents = append(a.pendingEvents, event.AgreementCreated{
		AgreementID:   a.id,
		AgreementCode: a.agreementCode,
		ProviderName:  a.providerName,
		ProviderType:  string(a.providerType),
		OccurredAt:    now,
	})

	return a, nil
}

// AddPlan agrega un nuevo Plan al Agreement.
// INV: no pueden existir dos Planes con el mismo PlanCode.
// INV: Agreement debe estar Active para agregar planes.
func (a *Agreement) AddPlan(plan Plan) error {
	if !a.status.IsActive() {
		return sharederrors.NewPrecondition("agreement_active",
			"solo se pueden agregar planes a un convenio activo")
	}
	if a.providerType.IsPrivado() {
		return sharederrors.NewPrecondition("agreement_not_privado",
			"los convenios de tipo Privado no tienen planes")
	}
	for _, existing := range a.plans {
		if existing.planCode == plan.planCode {
			return sharederrors.NewAlreadyExists("Plan", "plan_code", plan.planCode)
		}
	}

	a.plans = append(a.plans, plan)
	a.updatedAt = time.Now().UTC()

	a.pendingEvents = append(a.pendingEvents, event.AgreementPlanAdded{
		AgreementID: a.id,
		PlanID:      plan.id,
		PlanCode:    plan.planCode,
		CoPayType:   string(plan.coPayType),
		CoPayValue:  plan.coPayValue,
		OccurredAt:  time.Now().UTC(),
	})
	return nil
}

// UpsertProcedureRule delega en el Plan correspondiente.
func (a *Agreement) UpsertProcedureRule(planID uuid.UUID, rule ProcedureRule) error {
	for i := range a.plans {
		if a.plans[i].id == planID {
			if err := a.plans[i].UpsertProcedureRule(rule); err != nil {
				return err
			}
			a.updatedAt = time.Now().UTC()
			a.pendingEvents = append(a.pendingEvents, event.AgreementProcedureRuleUpdated{
				AgreementID:           a.id,
				PlanID:                planID,
				ProcedureCode:         rule.ProcedureCode,
				CoveragePercent:       rule.CoveragePercent,
				RequiresAuthorization: rule.RequiresAuthorization,
				OccurredAt:            time.Now().UTC(),
			})
			return nil
		}
	}
	return sharederrors.NewNotFound("Plan", planID.String())
}

// Suspend suspende el convenio.
func (a *Agreement) Suspend(reason string, by uuid.UUID) error {
	if a.status == valueobject.AgreementStatusSuspended {
		return sharederrors.NewPrecondition("already_suspended", "el convenio ya está suspendido")
	}
	if a.status == valueobject.AgreementStatusExpired {
		return sharederrors.NewPrecondition("agreement_expired", "no se puede suspender un convenio expirado")
	}
	a.status = valueobject.AgreementStatusSuspended
	a.updatedAt = time.Now().UTC()

	a.pendingEvents = append(a.pendingEvents, event.AgreementSuspended{
		AgreementID: a.id,
		Reason:      reason,
		SuspendedBy: by,
		OccurredAt:  time.Now().UTC(),
	})
	return nil
}

// Activate reactiva un convenio suspendido.
func (a *Agreement) Activate(by uuid.UUID) error {
	if a.status == valueobject.AgreementStatusActive {
		return sharederrors.NewPrecondition("already_active", "el convenio ya está activo")
	}
	if a.status == valueobject.AgreementStatusExpired {
		return sharederrors.NewPrecondition("agreement_expired", "no se puede activar un convenio expirado")
	}
	// INV-01: debe tener al menos un plan activo para volver a Active.
	if !a.providerType.IsPrivado() && !a.hasActivePlan() {
		return sharederrors.NewPrecondition("requires_active_plan",
			"el convenio debe tener al menos un plan activo para ser activado")
	}
	a.status = valueobject.AgreementStatusActive
	a.updatedAt = time.Now().UTC()

	a.pendingEvents = append(a.pendingEvents, event.AgreementActivated{
		AgreementID: a.id,
		ActivatedBy: by,
		OccurredAt:  time.Now().UTC(),
	})
	return nil
}

// Expire marca el convenio como expirado (llamado por job scheduler).
func (a *Agreement) Expire() {
	a.status = valueobject.AgreementStatusExpired
	a.updatedAt = time.Now().UTC()
	a.pendingEvents = append(a.pendingEvents, event.AgreementExpired{
		AgreementID: a.id,
		ExpiredAt:   time.Now().UTC(),
	})
}

// FindPlan retorna el Plan con el ID dado.
func (a *Agreement) FindPlan(planID uuid.UUID) (*Plan, bool) {
	for i := range a.plans {
		if a.plans[i].id == planID {
			return &a.plans[i], true
		}
	}
	return nil, false
}

// IsValidAt reporta si el Agreement está vigente en una fecha dada.
func (a *Agreement) IsValidAt(date time.Time) bool {
	if !a.status.IsActive() {
		return false
	}
	if date.Before(a.validFrom) {
		return false
	}
	if a.validUntil != nil && date.After(*a.validUntil) {
		return false
	}
	return true
}

// BumpVersion incrementa la versión tras una persistencia exitosa.
func (a *Agreement) BumpVersion() { a.version++ }

// PendingEvents retorna y limpia los eventos pendientes.
func (a *Agreement) PendingEvents() []event.DomainEvent {
	evts := a.pendingEvents
	a.pendingEvents = nil
	return evts
}

// Getters
func (a *Agreement) ID() uuid.UUID                           { return a.id }
func (a *Agreement) AgreementCode() string                   { return a.agreementCode }
func (a *Agreement) ProviderName() string                    { return a.providerName }
func (a *Agreement) ProviderType() valueobject.ProviderType  { return a.providerType }
func (a *Agreement) Status() valueobject.AgreementStatus     { return a.status }
func (a *Agreement) ValidFrom() time.Time                    { return a.validFrom }
func (a *Agreement) ValidUntil() *time.Time                  { return a.validUntil }
func (a *Agreement) ContactEmail() sharedvo.Email            { return a.contactEmail }
func (a *Agreement) ContactPhone() sharedvo.PhoneNumber      { return a.contactPhone }
func (a *Agreement) CancellationNoticeDays() int             { return a.cancellationNoticeDays }
func (a *Agreement) Plans() []Plan                           { return a.plans }
func (a *Agreement) CreatedAt() time.Time                    { return a.createdAt }
func (a *Agreement) UpdatedAt() time.Time                    { return a.updatedAt }
func (a *Agreement) CreatedBy() *uuid.UUID                   { return a.createdBy }
func (a *Agreement) Version() int64                          { return a.version }

// Reconstitute reconstruye un Agreement desde persistencia sin disparar eventos.
func Reconstitute(
	id uuid.UUID,
	agreementCode, providerName string,
	providerType valueobject.ProviderType,
	status valueobject.AgreementStatus,
	validFrom time.Time,
	validUntil *time.Time,
	contactEmail sharedvo.Email,
	contactPhone sharedvo.PhoneNumber,
	cancellationNoticeDays int,
	plans []Plan,
	createdAt, updatedAt time.Time,
	createdBy *uuid.UUID,
	version int64,
) *Agreement {
	return &Agreement{
		id:                     id,
		agreementCode:          agreementCode,
		providerName:           providerName,
		providerType:           providerType,
		status:                 status,
		validFrom:              validFrom,
		validUntil:             validUntil,
		contactEmail:           contactEmail,
		contactPhone:           contactPhone,
		cancellationNoticeDays: cancellationNoticeDays,
		plans:                  plans,
		createdAt:              createdAt,
		updatedAt:              updatedAt,
		createdBy:              createdBy,
		version:                version,
		pendingEvents:          []event.DomainEvent{},
	}
}

// ReconstitutePlan reconstruye un Plan desde persistencia.
func ReconstitutePlan(
	id uuid.UUID,
	planCode, planName string,
	coPayType valueobject.CoPayType,
	coPayValue int,
	requiresPreAuth bool,
	maxAnnualVisits *int,
	coveredProcedures []ProcedureRule,
	status valueobject.PlanStatus,
	createdAt, updatedAt time.Time,
) Plan {
	return Plan{
		id:                      id,
		planCode:                planCode,
		planName:                planName,
		coPayType:               coPayType,
		coPayValue:              coPayValue,
		requiresPreAuthorization: requiresPreAuth,
		maxAnnualVisits:         maxAnnualVisits,
		coveredProcedures:       coveredProcedures,
		status:                  status,
		createdAt:               createdAt,
		updatedAt:               updatedAt,
	}
}

// ── helpers internos ──────────────────────────────────────────────

func (a *Agreement) hasActivePlan() bool {
	for _, p := range a.plans {
		if p.status.IsActive() {
			return true
		}
	}
	return false
}
