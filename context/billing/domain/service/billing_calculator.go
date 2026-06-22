// Package service contiene los Domain Services del bounded context Billing.
package service

import (
	"context"
	"log/slog"

	"github.com/juantevez/odontoagenda/context/billing/domain/repository"
	"github.com/juantevez/odontoagenda/context/billing/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── CoverageInput — datos de cobertura recibidos de Coverage BC ──

// CoverageInput es el DTO que llega desde el CoverageClient HTTP.
// Refleja el CoverageResult de Coverage BC sin importar sus tipos.
type CoverageInput struct {
	IsCovered             bool
	CoveragePercent       int
	CoPayType             string
	CoPayValue            int
	RequiresAuthorization bool
}

// ── BillingCalculator — Domain Service ───────────────────────────

// BillingCalculator calcula los montos de un Quote a partir del arancel
// y el resultado de cobertura de Coverage BC.
//
// Algoritmo:
//  1. Fast-path si no hay cobertura: paciente paga el 100% del arancel.
//  2. Calcular monto cubierto por la prepaga: floor(arancel * coveragePercent / 100).
//  3. Calcular copago del paciente según CoPayType.
//  4. Ajuste por redondeo (INV-01: la suma debe cerrar exactamente).
//  5. Verificar que copago >= 0 (INV-02).
type BillingCalculator struct{}

func NewBillingCalculator() *BillingCalculator {
	return &BillingCalculator{}
}

// Calculate aplica el algoritmo y retorna los montos calculados.
func (c *BillingCalculator) Calculate(
	arancelCents int64,
	coverage CoverageInput,
) valueobject.QuoteAmounts {

	// ── Paso 1: fast-path sin cobertura ──────────────────────────
	if !coverage.IsCovered || coverage.CoveragePercent == 0 {
		return valueobject.QuoteAmounts{
			CoverageAmountCents: 0,
			CoPayAmountCents:    arancelCents,
		}
	}

	// ── Paso 2: monto cubierto por la prepaga ─────────────────────
	coverageAmountCents := arancelCents * int64(coverage.CoveragePercent) / 100

	// ── Paso 3: copago del paciente según CoPayType ───────────────
	var coPayAmountCents int64
	switch coverage.CoPayType {
	case string(valueobject.CoPayTypePercent):
		coPayAmountCents = arancelCents * int64(coverage.CoPayValue) / 100
	case string(valueobject.CoPayTypeFixedAmount):
		coPayAmountCents = int64(coverage.CoPayValue)
	case string(valueobject.CoPayTypeNone):
		coPayAmountCents = 0
	default:
		coPayAmountCents = arancelCents - coverageAmountCents
	}

	// ── Paso 4: ajuste por redondeo (INV-01) ─────────────────────
	// El paciente absorbe cualquier diferencia de centavo.
	coPayAmountCents = arancelCents - coverageAmountCents

	// ── Paso 5: garantizar INV-02 (copago >= 0) ───────────────────
	if coPayAmountCents < 0 {
		coPayAmountCents = 0
	}

	return valueobject.QuoteAmounts{
		CoverageAmountCents: coverageAmountCents,
		CoPayAmountCents:    coPayAmountCents,
	}
}

// ── CancellationPolicyService — Domain Service ────────────────────

// CancellationPolicyService resuelve la política de cancelación de una sede.
// Consulta la tabla billing.clinic_cancellation_policies; si no hay registro
// para la sede, retorna DefaultCancellationPolicy() como fallback.
type CancellationPolicyService struct {
	repo   repository.ClinicCancellationPolicyRepository
	logger *slog.Logger
}

func NewCancellationPolicyService(repo repository.ClinicCancellationPolicyRepository) *CancellationPolicyService {
	return &CancellationPolicyService{
		repo:   repo,
		logger: slog.Default().With("service", "CancellationPolicyService"),
	}
}

// GetForClinic retorna la política de cancelación vigente para una sede.
// Si la sede no tiene configuración propia, retorna la política por defecto.
func (s *CancellationPolicyService) GetForClinic(clinicID sharedtypes.ClinicID) valueobject.CancellationPolicy {
	policy, err := s.repo.FindByClinic(context.Background(), clinicID)
	if err != nil {
		s.logger.Warn("error consultando política de cancelación, usando default",
			"clinic_id", clinicID,
			"error", err,
		)
		return valueobject.DefaultCancellationPolicy()
	}
	return policy
}
