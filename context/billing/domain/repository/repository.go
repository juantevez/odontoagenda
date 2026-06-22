// Package repository define los puertos de salida del bounded context Billing.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/billing/domain/aggregate"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// QuoteRepository es el puerto de salida para la persistencia de Quote.
type QuoteRepository interface {
	// Save persiste un Quote nuevo. Retorna ErrAlreadyExists si ya existe
	// un Quote activo para el mismo appointmentID (INV-05).
	Save(ctx context.Context, q *aggregate.Quote) error

	// Update actualiza con optimistic locking (version).
	// Retorna ErrConflict si la versión difiere.
	Update(ctx context.Context, q *aggregate.Quote) error

	// FindByID busca por UUID. Retorna ErrNotFound si no existe.
	FindByID(ctx context.Context, id uuid.UUID) (*aggregate.Quote, error)

	// FindByAppointmentID busca el Quote activo de un appointment.
	// Retorna ErrNotFound si no existe ningún Quote para ese appointment.
	FindByAppointmentID(ctx context.Context, appointmentID uuid.UUID) (*aggregate.Quote, error)

	// FindActiveByPatient retorna los Quotes activos de un paciente paginados.
	// "Activo" = Draft | Confirmed | PartialPaid | ChargedFee.
	FindActiveByPatient(ctx context.Context, patientID sharedtypes.PatientID, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Quote], error)

	// FindByPatient retorna todos los Quotes de un paciente (historial completo).
	FindByPatient(ctx context.Context, patientID sharedtypes.PatientID, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Quote], error)

	// FindByClinicAndDate retorna los Quotes de una sede en una fecha dada.
	// Usado para el reporte de caja diaria.
	FindByClinicAndDate(ctx context.Context, clinicID sharedtypes.ClinicID, date time.Time) ([]*aggregate.Quote, error)

	// ExistsByAppointmentID verifica si ya hay un Quote activo para un appointment.
	// Usado para garantizar idempotencia al procesar appointment.booked.
	ExistsByAppointmentID(ctx context.Context, appointmentID uuid.UUID) (bool, error)
}
