// Package valueobject define los Value Objects del bounded context Coverage & Agreements.
package valueobject

import "fmt"

// ── ProviderType ──────────────────────────────────────────────────

// ProviderType clasifica al financiador del convenio.
type ProviderType string

const (
	ProviderTypeObraSocial      ProviderType = "ObraSocial"
	ProviderTypePrepagaExterna  ProviderType = "PrepagaExterna"
	ProviderTypePrepagaPropia   ProviderType = "PrepagaPropia"
	ProviderTypeCorporativo     ProviderType = "Corporativo"
	ProviderTypeConvenioEspecial ProviderType = "ConvenioEspecial"
	// Privado: pago de bolsillo 100%, sin reglas de cobertura ni autorización.
	// Incluye casos de emergencia extraordinaria de pago único.
	ProviderTypePrivado         ProviderType = "Privado"
)

func ParseProviderType(s string) (ProviderType, error) {
	switch ProviderType(s) {
	case ProviderTypeObraSocial, ProviderTypePrepagaExterna, ProviderTypePrepagaPropia,
		ProviderTypeCorporativo, ProviderTypeConvenioEspecial, ProviderTypePrivado:
		return ProviderType(s), nil
	}
	return "", fmt.Errorf("tipo de proveedor inválido: '%s'", s)
}

func (p ProviderType) String() string { return string(p) }

// IsPrivado reporta si el convenio es de pago directo (sin reglas de cobertura).
// CoverageCalculator usa esto para el fast-path: retorna 0% cobertura sin evaluar reglas.
func (p ProviderType) IsPrivado() bool { return p == ProviderTypePrivado }

// ── AgreementStatus ───────────────────────────────────────────────

type AgreementStatus string

const (
	AgreementStatusActive    AgreementStatus = "Active"
	AgreementStatusSuspended AgreementStatus = "Suspended"
	AgreementStatusExpired   AgreementStatus = "Expired"
)

func ParseAgreementStatus(s string) (AgreementStatus, error) {
	switch AgreementStatus(s) {
	case AgreementStatusActive, AgreementStatusSuspended, AgreementStatusExpired:
		return AgreementStatus(s), nil
	}
	return "", fmt.Errorf("estado de convenio inválido: '%s'", s)
}

func (s AgreementStatus) IsActive() bool  { return s == AgreementStatusActive }
func (s AgreementStatus) String() string  { return string(s) }

// ── PlanStatus ────────────────────────────────────────────────────

type PlanStatus string

const (
	PlanStatusActive       PlanStatus = "Active"
	PlanStatusDiscontinued PlanStatus = "Discontinued"
)

func (s PlanStatus) IsActive() bool { return s == PlanStatusActive }
func (s PlanStatus) String() string { return string(s) }

// ── CoPayType ─────────────────────────────────────────────────────

// CoPayType define cómo se calcula el copago del paciente.
type CoPayType string

const (
	CoPayTypePercent     CoPayType = "Percent"     // porcentaje del arancel (0-100)
	CoPayTypeFixedAmount CoPayType = "FixedAmount" // monto fijo en centavos ARS
	CoPayTypeNone        CoPayType = "None"        // sin copago (cobertura total)
)

func ParseCoPayType(s string) (CoPayType, error) {
	switch CoPayType(s) {
	case CoPayTypePercent, CoPayTypeFixedAmount, CoPayTypeNone:
		return CoPayType(s), nil
	}
	return "", fmt.Errorf("tipo de copago inválido: '%s'", s)
}

func (c CoPayType) String() string { return string(c) }

// ── AuthorizationStatus ───────────────────────────────────────────

type AuthorizationStatus string

const (
	AuthorizationStatusPending  AuthorizationStatus = "Pending"
	AuthorizationStatusApproved AuthorizationStatus = "Approved"
	AuthorizationStatusRejected AuthorizationStatus = "Rejected"
	AuthorizationStatusExpired  AuthorizationStatus = "Expired"
)

func ParseAuthorizationStatus(s string) (AuthorizationStatus, error) {
	switch AuthorizationStatus(s) {
	case AuthorizationStatusPending, AuthorizationStatusApproved,
		AuthorizationStatusRejected, AuthorizationStatusExpired:
		return AuthorizationStatus(s), nil
	}
	return "", fmt.Errorf("estado de autorización inválido: '%s'", s)
}

func (s AuthorizationStatus) String() string   { return string(s) }
func (s AuthorizationStatus) IsPending() bool  { return s == AuthorizationStatusPending }
func (s AuthorizationStatus) IsApproved() bool { return s == AuthorizationStatusApproved }
func (s AuthorizationStatus) IsTerminal() bool {
	return s == AuthorizationStatusApproved ||
		s == AuthorizationStatusRejected ||
		s == AuthorizationStatusExpired
}

// ── AffiliationStatus ─────────────────────────────────────────────

type AffiliationStatus string

const (
	AffiliationStatusActive    AffiliationStatus = "Active"
	AffiliationStatusSuspended AffiliationStatus = "Suspended"
	AffiliationStatusCancelled AffiliationStatus = "Cancelled"
)

func (s AffiliationStatus) IsActive() bool { return s == AffiliationStatusActive }
func (s AffiliationStatus) String() string { return string(s) }

// ── CoPayOverride ─────────────────────────────────────────────────

// CoPayOverride permite sobreescribir el copago base del plan
// para un procedimiento específico dentro de ProcedureRule.
type CoPayOverride struct {
	CoPayType  CoPayType
	CoPayValue int // porcentaje (0-100) o centavos según CoPayType
}

// ── CoverageResult ────────────────────────────────────────────────

// CoverageResult es el resultado del cálculo de cobertura.
// Es el DTO que CoverageCalculator retorna y que Billing consume.
type CoverageResult struct {
	IsCovered             bool
	CoveragePercent       int    // % que cubre la prepaga
	CoPayType             CoPayType
	CoPayValue            int    // porcentaje o centavos según CoPayType
	RequiresAuthorization bool
	// RejectionReason se completa cuando IsCovered = false.
	RejectionReason string
}

// NotCovered construye un CoverageResult de rechazo.
func NotCovered(reason string) CoverageResult {
	return CoverageResult{IsCovered: false, RejectionReason: reason}
}

// PrivadoResult es el fast-path para ProviderType = Privado.
// Pago 100% de bolsillo, sin cobertura, sin autorización.
func PrivadoResult() CoverageResult {
	return CoverageResult{
		IsCovered:             true,
		CoveragePercent:       0,
		CoPayType:             CoPayTypePercent,
		CoPayValue:            100,
		RequiresAuthorization: false,
	}
}
