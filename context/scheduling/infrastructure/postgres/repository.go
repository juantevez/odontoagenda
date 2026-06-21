// Package postgres contiene los adaptadores de salida PostgreSQL del bounded context Scheduling.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── AppointmentPostgresRepository ────────────────────────────────

type AppointmentPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewAppointmentPostgresRepository(pool *pgxpool.Pool) *AppointmentPostgresRepository {
	return &AppointmentPostgresRepository{pool: pool}
}

func (r *AppointmentPostgresRepository) Save(_ context.Context, _ *aggregate.Appointment) error {
	return fmt.Errorf("not implemented")
}

func (r *AppointmentPostgresRepository) Update(_ context.Context, _ *aggregate.Appointment) error {
	return fmt.Errorf("not implemented")
}

func (r *AppointmentPostgresRepository) FindByID(_ context.Context, _ sharedtypes.AppointmentID) (*aggregate.Appointment, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *AppointmentPostgresRepository) FindActiveByPatient(_ context.Context, _ sharedtypes.PatientID) ([]*aggregate.Appointment, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *AppointmentPostgresRepository) FindByProfessionalAndDate(
	_ context.Context,
	_ sharedtypes.ProfessionalID,
	_ sharedtypes.ClinicID,
	_, _ time.Time,
) ([]*aggregate.Appointment, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *AppointmentPostgresRepository) FindByClinicAndDate(
	_ context.Context,
	_ sharedtypes.ClinicID,
	_ time.Time,
) ([]*aggregate.Appointment, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *AppointmentPostgresRepository) CountActiveByPatient(_ context.Context, _ sharedtypes.PatientID) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

// ── AvailabilitySchedulePostgresRepository ────────────────────────

type AvailabilitySchedulePostgresRepository struct {
	pool *pgxpool.Pool
}

func NewAvailabilitySchedulePostgresRepository(pool *pgxpool.Pool) *AvailabilitySchedulePostgresRepository {
	return &AvailabilitySchedulePostgresRepository{pool: pool}
}

func (r *AvailabilitySchedulePostgresRepository) Save(_ context.Context, _ *aggregate.AvailabilitySchedule) error {
	return fmt.Errorf("not implemented")
}

func (r *AvailabilitySchedulePostgresRepository) Update(_ context.Context, _ *aggregate.AvailabilitySchedule) error {
	return fmt.Errorf("not implemented")
}

func (r *AvailabilitySchedulePostgresRepository) FindByProfessionalAndClinic(
	_ context.Context,
	_ sharedtypes.ProfessionalID,
	_ sharedtypes.ClinicID,
) (*aggregate.AvailabilitySchedule, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *AvailabilitySchedulePostgresRepository) FindByClinic(
	_ context.Context,
	_ sharedtypes.ClinicID,
) ([]*aggregate.AvailabilitySchedule, error) {
	return nil, fmt.Errorf("not implemented")
}
