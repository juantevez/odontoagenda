// Package repository define los puertos de salida (interfaces) del bounded context IAM.
// Las implementaciones concretas viven en infrastructure/postgres/.
// El dominio solo conoce las interfaces, nunca los adaptadores.
package repository

import (
	"context"

	"github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── UserRepository ────────────────────────────────────────────────

// UserRepository es el puerto de salida para la persistencia de User.
// Toda operación de escritura trabaja con el Aggregate completo.
type UserRepository interface {
	// Save persiste un User nuevo. Retorna ErrAlreadyExists si el email ya existe.
	Save(ctx context.Context, user *aggregate.User) error

	// Update persiste los cambios de un User existente.
	// Implementa optimistic locking: retorna ErrConflict si la versión difiere.
	Update(ctx context.Context, user *aggregate.User) error

	// FindByID busca un User por su ID. Retorna ErrNotFound si no existe.
	FindByID(ctx context.Context, id sharedtypes.UserID) (*aggregate.User, error)

	// FindByEmail busca un User por email. Retorna ErrNotFound si no existe.
	FindByEmail(ctx context.Context, email sharedvo.Email) (*aggregate.User, error)

	// ExistsByEmail reporta si ya existe un usuario con ese email.
	ExistsByEmail(ctx context.Context, email sharedvo.Email) (bool, error)
}

// ── FamilyRepository ─────────────────────────────────────────────

// FamilyRepository es el puerto de salida para FamilyAccount.
type FamilyRepository interface {
	// Save persiste una FamilyAccount nueva.
	Save(ctx context.Context, family *aggregate.FamilyAccount) error

	// Update actualiza una FamilyAccount existente con optimistic locking.
	Update(ctx context.Context, family *aggregate.FamilyAccount) error

	// FindByID busca una FamilyAccount por su ID.
	FindByID(ctx context.Context, id sharedtypes.FamilyID) (*aggregate.FamilyAccount, error)

	// FindByPatientID busca la FamilyAccount activa de un paciente.
	// Un paciente puede pertenecer a máximo 1 cuenta familiar activa.
	FindByPatientID(ctx context.Context, patientID sharedtypes.PatientID) (*aggregate.FamilyAccount, error)
}
