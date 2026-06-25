// Package repository define los puertos de salida del bounded context Professional.
package repository

import (
	"context"
	"time"

	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ProfessionalRepository es el puerto de salida para la persistencia de Professional.
type ProfessionalRepository interface {
	// Save persiste un Professional nuevo.
	Save(ctx context.Context, p *aggregate.Professional) error

	// Update persiste cambios con optimistic locking.
	// Retorna ErrConflict si la versión en BD difiere.
	Update(ctx context.Context, p *aggregate.Professional) error

	// FindByID busca por UUID. Retorna ErrNotFound si no existe.
	FindByID(ctx context.Context, id sharedtypes.ProfessionalID) (*aggregate.Professional, error)

	// FindByClinic retorna los profesionales activos asignados a una sede,
	// opcionalmente filtrados por especialidad.
	FindByClinic(
		ctx context.Context,
		clinicID sharedtypes.ClinicID,
		specialty *valueobject.SpecialtyCode,
	) ([]*aggregate.Professional, error)

	// FindBySpecialty retorna los profesionales con licencia activa para una especialidad.
	FindBySpecialty(ctx context.Context, code valueobject.SpecialtyCode) ([]*aggregate.Professional, error)

	// FindAvailableAt retorna profesionales disponibles en una sede en un momento dado.
	// Es la query más frecuente del sistema (búsqueda de disponibilidad).
	FindAvailableAt(
		ctx context.Context,
		clinicID sharedtypes.ClinicID,
		at time.Time,
		specialty *valueobject.SpecialtyCode,
	) ([]*aggregate.Professional, error)

	// FindWithExpiringLicenses retorna profesionales con matrículas que vencen
	// en los próximos `withinDays` días. Usado por el job de alertas de vencimiento.
	FindWithExpiringLicenses(ctx context.Context, withinDays int) ([]*aggregate.Professional, error)

	// Search busca profesionales por nombre o código de especialidad (búsqueda parcial).
	// Si clinicID es UUID cero, busca en todas las sedes.
	Search(ctx context.Context, clinicID sharedtypes.ClinicID, q string) ([]*aggregate.Professional, error)

	// ExistsByNationalID verifica unicidad de DNI sin cargar el aggregate.
	ExistsByNationalID(ctx context.Context, nationalID string) (bool, error)
}
