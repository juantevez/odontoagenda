// Package coverage define el submódulo de Coberturas dentro del bounded context Patient.
//
// Decisión de diseño (MVP):
//
//	Coverage vive dentro de Patient Management como submódulo fuerte con
//	sus propias reglas de negocio, entidades e historial. Cuando el sistema
//	tenga 3+ convenios externos con integraciones API, se extrae a su propio
//	bounded context (Coverage & Agreements).
//
// Aggregates contenidos:
//   - PatientCoverage (Entity dentro del Aggregate Patient)
//   - CoverageHistory (log de cambios, append-only)
//   - Benefit (Value Object que describe una prestación cubierta)
package coverage

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── PatientCoverage — Entity ──────────────────────────────────────

// PatientCoverage representa la cobertura activa de un paciente.
// Es una Entity dentro del Aggregate Patient (tiene identidad propia
// para poder rastrear cambios en el historial).
//
// Invariantes:
//   - Un paciente no puede tener dos coberturas activas del mismo CoverageType.
//   - Si la cobertura tiene ValidUntil, no puede ser anterior a ValidFrom.
//   - AgreementID es requerido para todos los tipos excepto Privado.
//   - Solo el staff puede crear o modificar la cobertura (ver application layer).
type PatientCoverage struct {
	id           uuid.UUID
	patientID    sharedtypes.PatientID
	coverageType valueobject.CoverageType
	status       CoverageStatus

	// Referencia al Agreement en el futuro bounded context Coverage & Agreements.
	// En MVP es un ID externo sin FK real; la validación la hará el Coverage BC.
	agreementID      *uuid.UUID
	providerName     string // ej: "OSDE", "Swiss Medical", "Plan Propio Oro"
	planCode         string // ej: "OSDE-500", "PREPAGA-ORO"
	membershipNumber string

	validFrom  time.Time
	validUntil *time.Time // nil = sin vencimiento (Privado, PrepagaPropia sin fecha)

	// Copago: porcentaje (0-100) o monto fijo en centavos.
	// Solo uno de los dos aplica según el tipo de convenio.
	coPayPercent *int   // 0-100, nil si no aplica
	coPayFixed   *int64 // centavos ARS, nil si no aplica

	// Límites anuales por prestación (mapa ProcedureCode → límite en centavos).
	// Relevante para PrepagaExterna y ObraSocial.
	annualLimits    map[string]int64 // ProcedureCode → límite anual en centavos
	remainingLimits map[string]int64 // estado actual de consumo

	// Beneficios específicos del plan (prestaciones cubiertas).
	benefits []Benefit

	createdAt time.Time
	updatedAt time.Time
	createdBy uuid.UUID
}

// CoverageStatus es el estado del ciclo de vida de la cobertura.
type CoverageStatus string

const (
	CoverageStatusActive    CoverageStatus = "Active"
	CoverageStatusSuspended CoverageStatus = "Suspended"
	CoverageStatusExpired   CoverageStatus = "Expired"
)

// ── Benefit — Value Object ────────────────────────────────────────

// Benefit describe una prestación cubierta por el plan del paciente.
type Benefit struct {
	ProcedureCode   string // referencia a Treatment Catalog
	Description     string
	CoveragePercent int  // % cubierto por la cobertura (0-100)
	MaxPerYear      *int // máximo de prestaciones por año, nil = ilimitado
	RequiresAuth    bool // si requiere autorización previa del prestador
}

// ── CoverageHistory — Value Object (log inmutable) ────────────────

// CoverageHistoryEntry registra cada cambio de cobertura del paciente.
// Es append-only: nunca se modifica, solo se agregan entradas.
type CoverageHistoryEntry struct {
	ID             uuid.UUID
	PatientID      sharedtypes.PatientID
	PreviousType   *valueobject.CoverageType
	NewType        valueobject.CoverageType
	PreviousStatus *CoverageStatus
	NewStatus      CoverageStatus
	Reason         string
	ChangedAt      time.Time
	ChangedBy      uuid.UUID
}

// ── Constructor ───────────────────────────────────────────────────

// NewPatientCoverage crea una PatientCoverage validando las reglas de negocio.
func NewPatientCoverage(
	patientID sharedtypes.PatientID,
	coverageType valueobject.CoverageType,
	agreementID *uuid.UUID,
	providerName, planCode, membershipNumber string,
	validFrom time.Time,
	validUntil *time.Time,
	createdBy uuid.UUID,
) (*PatientCoverage, error) {

	// Tipos no-Privado requieren agreementID y providerName.
	if coverageType != valueobject.CoverageTypePrivate {
		if agreementID == nil {
			return nil, sharederrors.NewInvalidArgument("agreement_id",
				fmt.Sprintf("requerido para cobertura tipo '%s'", coverageType))
		}
		if strings.TrimSpace(providerName) == "" {
			return nil, sharederrors.NewInvalidArgument("provider_name",
				"requerido para cobertura no-privada")
		}
	}

	if validUntil != nil && validUntil.Before(validFrom) {
		return nil, sharederrors.NewInvalidArgument("valid_until",
			"no puede ser anterior a valid_from")
	}

	return &PatientCoverage{
		id:               uuid.New(),
		patientID:        patientID,
		coverageType:     coverageType,
		status:           CoverageStatusActive,
		agreementID:      agreementID,
		providerName:     strings.TrimSpace(providerName),
		planCode:         strings.TrimSpace(planCode),
		membershipNumber: strings.TrimSpace(membershipNumber),
		validFrom:        validFrom.UTC(),
		validUntil:       validUntil,
		annualLimits:     make(map[string]int64),
		remainingLimits:  make(map[string]int64),
		benefits:         []Benefit{},
		createdAt:        time.Now().UTC(),
		updatedAt:        time.Now().UTC(),
		createdBy:        createdBy,
	}, nil
}

// ── Comportamiento de dominio ─────────────────────────────────────

// IsActive reporta si la cobertura está vigente en el momento dado.
func (c *PatientCoverage) IsActive() bool {
	if c.status != CoverageStatusActive {
		return false
	}
	now := time.Now().UTC()
	if now.Before(c.validFrom) {
		return false
	}
	if c.validUntil != nil && now.After(*c.validUntil) {
		return false
	}
	return true
}

// Suspend suspende la cobertura activa.
func (c *PatientCoverage) Suspend(reason string, by uuid.UUID) error {
	if c.status != CoverageStatusActive {
		return sharederrors.NewPrecondition("coverage_active",
			fmt.Sprintf("no se puede suspender una cobertura en estado '%s'", c.status))
	}
	c.status = CoverageStatusSuspended
	c.updatedAt = time.Now().UTC()
	return nil
}

// Expire marca la cobertura como vencida (llamado por job scheduler o evento).
func (c *PatientCoverage) Expire() {
	c.status = CoverageStatusExpired
	c.updatedAt = time.Now().UTC()
}

// SetCoPayPercent establece el copago como porcentaje (0-100).
func (c *PatientCoverage) SetCoPayPercent(percent int) error {
	if percent < 0 || percent > 100 {
		return sharederrors.NewInvalidArgument("co_pay_percent", "debe estar entre 0 y 100")
	}
	c.coPayPercent = &percent
	c.coPayFixed = nil // solo uno aplica
	c.updatedAt = time.Now().UTC()
	return nil
}

// SetCoPayFixed establece el copago como monto fijo en centavos.
func (c *PatientCoverage) SetCoPayFixed(amountCents int64) error {
	if amountCents < 0 {
		return sharederrors.NewInvalidArgument("co_pay_fixed", "no puede ser negativo")
	}
	c.coPayFixed = &amountCents
	c.coPayPercent = nil
	c.updatedAt = time.Now().UTC()
	return nil
}

// SetAnnualLimit establece el límite anual para un procedimiento.
func (c *PatientCoverage) SetAnnualLimit(procedureCode string, limitCents int64) {
	c.annualLimits[procedureCode] = limitCents
	if _, exists := c.remainingLimits[procedureCode]; !exists {
		c.remainingLimits[procedureCode] = limitCents
	}
	c.updatedAt = time.Now().UTC()
}

// ConsumeLimit descuenta del límite anual de un procedimiento.
// Retorna ErrPrecondition si excede el límite disponible.
func (c *PatientCoverage) ConsumeLimit(procedureCode string, amountCents int64) error {
	limit, hasLimit := c.annualLimits[procedureCode]
	if !hasLimit {
		return nil // sin límite definido: ilimitado
	}

	remaining, ok := c.remainingLimits[procedureCode]
	if !ok {
		remaining = limit
	}

	if amountCents > remaining {
		return sharederrors.NewPrecondition("annual_limit_exceeded",
			fmt.Sprintf("límite anual excedido para '%s': disponible %d centavos, solicitado %d",
				procedureCode, remaining, amountCents))
	}

	c.remainingLimits[procedureCode] = remaining - amountCents
	c.updatedAt = time.Now().UTC()
	return nil
}

// AddBenefit agrega una prestación cubierta al plan.
func (c *PatientCoverage) AddBenefit(b Benefit) error {
	if b.CoveragePercent < 0 || b.CoveragePercent > 100 {
		return sharederrors.NewInvalidArgument("coverage_percent", "debe estar entre 0 y 100")
	}
	// Reemplazar si ya existe el mismo procedureCode.
	for i, existing := range c.benefits {
		if existing.ProcedureCode == b.ProcedureCode {
			c.benefits[i] = b
			c.updatedAt = time.Now().UTC()
			return nil
		}
	}
	c.benefits = append(c.benefits, b)
	c.updatedAt = time.Now().UTC()
	return nil
}

// CoverageForProcedure retorna el Benefit para un procedimiento, si existe.
func (c *PatientCoverage) CoverageForProcedure(procedureCode string) (*Benefit, bool) {
	for i := range c.benefits {
		if c.benefits[i].ProcedureCode == procedureCode {
			return &c.benefits[i], true
		}
	}
	return nil, false
}

// ── Getters ───────────────────────────────────────────────────────

func (c *PatientCoverage) ID() uuid.UUID                          { return c.id }
func (c *PatientCoverage) PatientID() sharedtypes.PatientID       { return c.patientID }
func (c *PatientCoverage) CoverageType() valueobject.CoverageType { return c.coverageType }
func (c *PatientCoverage) Status() CoverageStatus                 { return c.status }
func (c *PatientCoverage) AgreementID() *uuid.UUID                { return c.agreementID }
func (c *PatientCoverage) ProviderName() string                   { return c.providerName }
func (c *PatientCoverage) PlanCode() string                       { return c.planCode }
func (c *PatientCoverage) MembershipNumber() string               { return c.membershipNumber }
func (c *PatientCoverage) ValidFrom() time.Time                   { return c.validFrom }
func (c *PatientCoverage) ValidUntil() *time.Time                 { return c.validUntil }
func (c *PatientCoverage) CoPayPercent() *int                     { return c.coPayPercent }
func (c *PatientCoverage) CoPayFixed() *int64                     { return c.coPayFixed }
func (c *PatientCoverage) AnnualLimits() map[string]int64         { return c.annualLimits }
func (c *PatientCoverage) RemainingLimits() map[string]int64      { return c.remainingLimits }
func (c *PatientCoverage) Benefits() []Benefit                    { return c.benefits }
func (c *PatientCoverage) CreatedAt() time.Time                   { return c.createdAt }
func (c *PatientCoverage) UpdatedAt() time.Time                   { return c.updatedAt }
func (c *PatientCoverage) CreatedBy() uuid.UUID                   { return c.createdBy }
