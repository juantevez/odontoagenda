// Package postgres contiene los adaptadores de salida PostgreSQL del bounded context Scheduling.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── AppointmentPostgresRepository ────────────────────────────────

type AppointmentPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewAppointmentPostgresRepository(pool *pgxpool.Pool) *AppointmentPostgresRepository {
	return &AppointmentPostgresRepository{pool: pool}
}

func (r *AppointmentPostgresRepository) Save(ctx context.Context, a *aggregate.Appointment) error {
	slot := a.Slot()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO scheduling.appointments
			(id, patient_id, booked_by_id, professional_id, clinic_id, procedure_code,
			 slot_start, slot_end, status, coverage_type, agreement_id, requires_auth_id,
			 clinical_notes, cancellation_reason, cancellation_note,
			 cancelled_at, cancelled_by_user_id, is_late_cancellation,
			 created_at, updated_at, created_by, version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
		a.ID(), a.PatientID(), a.BookedByID(), a.ProfessionalID(), a.ClinicID(), a.ProcedureCode(),
		slot.Start, slot.End, string(a.Status()),
		nullString(a.CoverageType()), a.AgreementID(), a.RequiresAuthID(),
		nullString(a.ClinicalNotes()),
		nullString(string(a.CancellationReason())), nullString(a.CancellationNote()),
		a.CancelledAt(), a.CancelledByUserID(), a.IsLateCancellation(),
		a.CreatedAt(), a.UpdatedAt(), a.CreatedBy(), a.Version(),
	)
	return err
}

func (r *AppointmentPostgresRepository) Update(ctx context.Context, a *aggregate.Appointment) error {
	slot := a.Slot()
	tag, err := r.pool.Exec(ctx, `
		UPDATE scheduling.appointments SET
			status=$1, clinical_notes=$2,
			cancellation_reason=$3, cancellation_note=$4,
			cancelled_at=$5, cancelled_by_user_id=$6, is_late_cancellation=$7,
			updated_at=$8, version=version+1
		WHERE id=$9 AND version=$10`,
		string(a.Status()), nullString(a.ClinicalNotes()),
		nullString(string(a.CancellationReason())), nullString(a.CancellationNote()),
		a.CancelledAt(), a.CancelledByUserID(), a.IsLateCancellation(),
		time.Now().UTC(), a.ID(), a.Version(),
	)
	_ = slot
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("optimistic lock conflict: appointment %s modified concurrently", a.ID())
	}
	return nil
}

func (r *AppointmentPostgresRepository) FindByID(ctx context.Context, id sharedtypes.AppointmentID) (*aggregate.Appointment, error) {
	row := r.pool.QueryRow(ctx, appointmentSelectCols+`
		WHERE id=$1`, id)
	return scanAppointment(row)
}

func (r *AppointmentPostgresRepository) FindActiveByPatient(ctx context.Context, patientID sharedtypes.PatientID) ([]*aggregate.Appointment, error) {
	rows, err := r.pool.Query(ctx, appointmentSelectCols+`
		WHERE patient_id=$1 AND status IN ('Pending','Confirmed','InProgress')
		ORDER BY slot_start`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAppointments(rows)
}

func (r *AppointmentPostgresRepository) FindByProfessionalAndDate(
	ctx context.Context,
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	from, to time.Time,
) ([]*aggregate.Appointment, error) {
	rows, err := r.pool.Query(ctx, appointmentSelectCols+`
		WHERE professional_id=$1 AND clinic_id=$2
		  AND slot_start >= $3 AND slot_start < $4
		ORDER BY slot_start`, professionalID, clinicID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAppointments(rows)
}

func (r *AppointmentPostgresRepository) FindByClinicAndDate(
	ctx context.Context,
	clinicID sharedtypes.ClinicID,
	date time.Time,
) ([]*aggregate.Appointment, error) {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	rows, err := r.pool.Query(ctx, appointmentSelectCols+`
		WHERE clinic_id=$1 AND slot_start >= $2 AND slot_start < $3
		  AND status NOT IN ('Cancelled','NoShow')
		ORDER BY slot_start`, clinicID, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAppointments(rows)
}

func (r *AppointmentPostgresRepository) CountActiveByPatient(ctx context.Context, patientID sharedtypes.PatientID) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM scheduling.appointments
		 WHERE patient_id=$1 AND status IN ('Pending','Confirmed','InProgress')`,
		patientID).Scan(&count)
	return count, err
}

// ── SQL helpers ───────────────────────────────────────────────────

const appointmentSelectCols = `
	SELECT id, patient_id, booked_by_id, professional_id, clinic_id, procedure_code,
	       slot_start, slot_end, status, coverage_type, agreement_id, requires_auth_id,
	       clinical_notes, cancellation_reason, cancellation_note,
	       cancelled_at, cancelled_by_user_id, is_late_cancellation,
	       created_at, updated_at, created_by, version
	FROM scheduling.appointments `

func scanAppointment(row pgx.Row) (*aggregate.Appointment, error) {
	return scanApptRow(row)
}

func scanAppointments(rows pgx.Rows) ([]*aggregate.Appointment, error) {
	var result []*aggregate.Appointment
	for rows.Next() {
		a, err := scanApptRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	if result == nil {
		result = []*aggregate.Appointment{}
	}
	return result, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanApptRow(row rowScanner) (*aggregate.Appointment, error) {
	var (
		id, patientID, bookedByID, professionalID, clinicID uuid.UUID
		procedureCode                                        string
		slotStart, slotEnd                                   time.Time
		status                                               string
		coverageType                                         *string
		agreementID                                          *uuid.UUID
		requiresAuthID                                       *string
		clinicalNotes                                        *string
		cancellationReason                                   *string
		cancellationNote                                     *string
		cancelledAt                                          *time.Time
		cancelledByUserID                                    *uuid.UUID
		isLateCancellation                                   bool
		createdAt, updatedAt                                 time.Time
		createdBy                                            uuid.UUID
		version                                              int64
	)

	err := row.Scan(
		&id, &patientID, &bookedByID, &professionalID, &clinicID, &procedureCode,
		&slotStart, &slotEnd, &status, &coverageType, &agreementID, &requiresAuthID,
		&clinicalNotes, &cancellationReason, &cancellationNote,
		&cancelledAt, &cancelledByUserID, &isLateCancellation,
		&createdAt, &updatedAt, &createdBy, &version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("appointment not found")
		}
		return nil, err
	}

	slot, err := valueobject.NewTimeSlot(slotStart, slotEnd)
	if err != nil {
		return nil, err
	}
	apptStatus, err := valueobject.ParseAppointmentStatus(status)
	if err != nil {
		return nil, err
	}

	var cancelReason valueobject.CancellationReason
	if cancellationReason != nil {
		cancelReason = valueobject.CancellationReason(*cancellationReason)
	}

	return aggregate.ReconstituteAppointment(
		id, patientID, bookedByID, professionalID, clinicID, procedureCode,
		slot, apptStatus,
		derefString(coverageType), agreementID, requiresAuthID,
		derefString(clinicalNotes),
		cancelReason, derefString(cancellationNote),
		cancelledAt, cancelledByUserID, isLateCancellation,
		createdAt, updatedAt, createdBy, version,
	), nil
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ── AvailabilitySchedulePostgresRepository ────────────────────────

type AvailabilitySchedulePostgresRepository struct {
	pool *pgxpool.Pool
}

func NewAvailabilitySchedulePostgresRepository(pool *pgxpool.Pool) *AvailabilitySchedulePostgresRepository {
	return &AvailabilitySchedulePostgresRepository{pool: pool}
}

func (r *AvailabilitySchedulePostgresRepository) Save(_ context.Context, _ *aggregate.AvailabilitySchedule) error {
	return nil
}

func (r *AvailabilitySchedulePostgresRepository) Update(_ context.Context, _ *aggregate.AvailabilitySchedule) error {
	return nil
}

func (r *AvailabilitySchedulePostgresRepository) FindByProfessionalAndClinic(ctx context.Context, professionalID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID) (*aggregate.AvailabilitySchedule, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, professional_id, clinic_id, working_hours, exception_days,
		       blocked_slots, booked_slots, procedure_durations, is_active, updated_at, version
		FROM scheduling.availability_schedules
		WHERE professional_id=$1 AND clinic_id=$2 AND is_active=true
		LIMIT 1`, professionalID, clinicID)

	return scanAvailabilitySchedule(row)
}

func (r *AvailabilitySchedulePostgresRepository) FindByClinic(ctx context.Context, clinicID sharedtypes.ClinicID) ([]*aggregate.AvailabilitySchedule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, professional_id, clinic_id, working_hours, exception_days,
		       blocked_slots, booked_slots, procedure_durations, is_active, updated_at, version
		FROM scheduling.availability_schedules
		WHERE clinic_id=$1 AND is_active=true`, clinicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []*aggregate.AvailabilitySchedule
	for rows.Next() {
		s, err := scanAvailabilitySchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	if schedules == nil {
		schedules = []*aggregate.AvailabilitySchedule{}
	}
	return schedules, rows.Err()
}

type scheduleScanner interface {
	Scan(dest ...any) error
}

func scanAvailabilitySchedule(row scheduleScanner) (*aggregate.AvailabilitySchedule, error) {
	var (
		id, professionalID, clinicID uuid.UUID
		workingHoursJSON             []byte
		exceptionDaysJSON            []byte
		blockedSlotsJSON             []byte
		bookedSlotsJSON              []byte
		procedureDurationsJSON       []byte
		isActive                     bool
		updatedAt                    time.Time
		version                      int64
	)

	err := row.Scan(
		&id, &professionalID, &clinicID,
		&workingHoursJSON, &exceptionDaysJSON, &blockedSlotsJSON,
		&bookedSlotsJSON, &procedureDurationsJSON,
		&isActive, &updatedAt, &version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("schedule not found")
		}
		return nil, err
	}

	// Parse working hours
	var rawHours []struct {
		Weekday   int `json:"weekday"`
		StartHour int `json:"start_hour"`
		StartMin  int `json:"start_min"`
		EndHour   int `json:"end_hour"`
		EndMin    int `json:"end_min"`
	}
	_ = jsonUnmarshal(workingHoursJSON, &rawHours)
	workingHours := make([]aggregate.WorkingHour, len(rawHours))
	for i, h := range rawHours {
		workingHours[i] = aggregate.WorkingHour{
			Weekday:   time.Weekday(h.Weekday),
			StartHour: h.StartHour,
			StartMin:  h.StartMin,
			EndHour:   h.EndHour,
			EndMin:    h.EndMin,
		}
	}

	// Parse procedure durations
	var procedureDurations map[string]int
	_ = jsonUnmarshal(procedureDurationsJSON, &procedureDurations)
	if procedureDurations == nil {
		procedureDurations = map[string]int{}
	}

	return aggregate.ReconstituteSchedule(
		id,
		sharedtypes.ProfessionalID(professionalID),
		sharedtypes.ClinicID(clinicID),
		workingHours,
		[]aggregate.ExceptionDay{},
		[]aggregate.BlockedSlot{},
		[]aggregate.BookedSlot{},
		procedureDurations,
		isActive,
		updatedAt,
		version,
	), nil
}

func jsonUnmarshal(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}
