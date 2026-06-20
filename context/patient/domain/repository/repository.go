// Package repository define los puertos de salida del bounded context Patient.
package repository

import (
	"context"

	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── PatientRepository — Puerto de salida principal ────────────────

// PatientRepository es la interfaz que debe implementar cualquier
// adaptador de persistencia para el aggregate Patient.
type PatientRepository interface {
	// Save persiste un Patient nuevo. Retorna ErrAlreadyExists si el DNI ya existe.
	Save(ctx context.Context, patient *aggregate.Patient) error

	// Update persiste cambios con optimistic locking (campo version).
	// Retorna ErrConflict si la versión en BD difiere.
	Update(ctx context.Context, patient *aggregate.Patient) error

	// FindByID busca un Patient por su UUID. Retorna ErrNotFound si no existe.
	FindByID(ctx context.Context, id sharedtypes.PatientID) (*aggregate.Patient, error)

	// FindByNationalID busca por tipo+número de documento.
	FindByNationalID(ctx context.Context, nationalID sharedvo.NationalID) (*aggregate.Patient, error)

	// FindByUserID busca el Patient vinculado a una cuenta IAM.
	FindByUserID(ctx context.Context, userID sharedtypes.UserID) (*aggregate.Patient, error)

	// Search realiza búsqueda fuzzy por nombre, documento o teléfono.
	// Usa pg_trgm (trigram similarity) en PostgreSQL para tolerancia a typos.
	Search(ctx context.Context, query string, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error)

	// FindNearClinic retorna pacientes en un radio dado de una clínica
	// ordenados por distancia. Usa ST_DWithin de PostGIS.
	FindNearClinic(ctx context.Context, clinicID sharedtypes.ClinicID, radiusMeters float64, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error)

	// ExistsByNationalID verifica unicidad sin cargar el aggregate completo.
	ExistsByNationalID(ctx context.Context, nationalID sharedvo.NationalID) (bool, error)

	// FindPotentialDuplicates retorna pacientes con nombre y/o fecha de nacimiento similares.
	// Usado por el servicio de detección de duplicados antes de registrar.
	FindPotentialDuplicates(ctx context.Context, fullName string, phone string) ([]*aggregate.Patient, error)

	// Archive realiza la baja lógica: setea status=Archived, no elimina.
	Archive(ctx context.Context, id sharedtypes.PatientID, reason string, by sharedtypes.UserID) error
}

// ── CoverageHistoryRepository — Puerto de salida para historial ───

// CoverageHistoryRepository persiste el log inmutable de cambios de cobertura.
type CoverageHistoryRepository interface {
	// Append agrega una entrada al historial (append-only, sin Update ni Delete).
	Append(ctx context.Context, entry coverage.CoverageHistoryEntry) error

	// FindByPatientID retorna el historial completo de un paciente, paginado.
	FindByPatientID(ctx context.Context, patientID sharedtypes.PatientID, page sharedtypes.Page) (sharedtypes.PagedResult[coverage.CoverageHistoryEntry], error)
}
