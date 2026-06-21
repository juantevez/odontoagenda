// Package repository define los puertos de salida del bounded context Coverage.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── AgreementRepository ───────────────────────────────────────────

type AgreementRepository interface {
	Save(ctx context.Context, a *aggregate.Agreement) error
	Update(ctx context.Context, a *aggregate.Agreement) error
	FindByID(ctx context.Context, id uuid.UUID) (*aggregate.Agreement, error)
	FindByCode(ctx context.Context, code string) (*aggregate.Agreement, error)
	FindActive(ctx context.Context, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Agreement], error)
	FindByProviderType(ctx context.Context, pt valueobject.ProviderType, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Agreement], error)
	ExistsByCode(ctx context.Context, code string) (bool, error)
	// FindExpired retorna convenios cuyo validUntil < before y status != Expired.
	// Usado por el job scheduler de expiración.
	FindExpired(ctx context.Context, before time.Time) ([]*aggregate.Agreement, error)
}

// ── AuthorizationRepository ───────────────────────────────────────

type AuthorizationRepository interface {
	Save(ctx context.Context, ar *aggregate.AuthorizationRequest) error
	Update(ctx context.Context, ar *aggregate.AuthorizationRequest) error
	FindByID(ctx context.Context, id uuid.UUID) (*aggregate.AuthorizationRequest, error)
	// FindPendingByAgreement retorna autorizaciones pendientes de un convenio.
	// Usado por la cola de resolución manual del staff.
	FindPendingByAgreement(ctx context.Context, agreementID uuid.UUID) ([]*aggregate.AuthorizationRequest, error)
	// FindPendingByPatient verifica si ya hay una autorización activa para el
	// mismo paciente + procedimiento (evita duplicados).
	FindPendingByPatient(ctx context.Context, patientID sharedtypes.PatientID, procedureCode string) (*aggregate.AuthorizationRequest, error)
	// FindExpired retorna autorizaciones Pending con expiresAt < before.
	// Usado por el job de expiración automática.
	FindExpired(ctx context.Context, before time.Time) ([]*aggregate.AuthorizationRequest, error)
}

// ── PatientAffiliationRepository ─────────────────────────────────

// PatientAffiliationRepository gestiona la tabla coverage.patient_affiliations.
// Se actualiza al consumir patient.coverage.updated desde Patient BC.
type PatientAffiliationRepository interface {
	// Upsert crea o actualiza la afiliación de un paciente a un plan.
	Upsert(ctx context.Context, affiliation PatientAffiliation) error
	// FindActive busca la afiliación activa de un paciente a un plan.
	FindActive(ctx context.Context, patientID sharedtypes.PatientID, planID uuid.UUID) (*PatientAffiliation, error)
	// SuspendByPatient suspende todas las afiliaciones activas de un paciente.
	SuspendByPatient(ctx context.Context, patientID sharedtypes.PatientID) error
}

// PatientAffiliation es el modelo de datos de la afiliación (no es aggregate,
// es un DTO de persistencia gestionado por este repositorio).
type PatientAffiliation struct {
	ID               uuid.UUID
	PatientID        sharedtypes.PatientID
	AgreementID      uuid.UUID
	PlanID           uuid.UUID
	MembershipNumber string
	AffiliatedSince  time.Time
	Status           valueobject.AffiliationStatus
	CreatedAt        time.Time
}

// ── CoverageCache ─────────────────────────────────────────────────

// CoverageCache es el puerto de salida para el cache de CoverageResult en Redis.
type CoverageCache interface {
	// GetCoverageResult retorna el resultado cacheado. (nil, nil) = cache miss.
	GetCoverageResult(ctx context.Context, planID uuid.UUID, procedureCode string) (*valueobject.CoverageResult, error)
	// SetCoverageResult almacena el resultado con TTL de 5 minutos.
	SetCoverageResult(ctx context.Context, planID uuid.UUID, procedureCode string, result valueobject.CoverageResult) error
	// InvalidatePlan invalida todo el cache de un plan (al modificar sus reglas).
	InvalidatePlan(ctx context.Context, planID uuid.UUID) error
}
