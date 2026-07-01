package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/application/command"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/repository"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/saga"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/service"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── mock appointment repo ─────────────────────────────────────────

type mockApptRepo struct {
	appts          map[sharedtypes.AppointmentID]*aggregate.Appointment
	saveErr        error
	updateErr      error
	findByIDErr    error
	countActiveVal int
	countActiveErr error
}

var _ repository.AppointmentRepository = (*mockApptRepo)(nil)

func newMockApptRepo() *mockApptRepo {
	return &mockApptRepo{appts: make(map[sharedtypes.AppointmentID]*aggregate.Appointment)}
}

func (m *mockApptRepo) Save(_ context.Context, a *aggregate.Appointment) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.appts[a.ID()] = a
	return nil
}

func (m *mockApptRepo) Update(_ context.Context, a *aggregate.Appointment) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.appts[a.ID()] = a
	return nil
}

func (m *mockApptRepo) FindByID(_ context.Context, id sharedtypes.AppointmentID) (*aggregate.Appointment, error) {
	if m.findByIDErr != nil {
		return nil, m.findByIDErr
	}
	a, ok := m.appts[id]
	if !ok {
		return nil, sharederrors.NewNotFound("Appointment", id.String())
	}
	return a, nil
}

func (m *mockApptRepo) FindActiveByPatient(_ context.Context, _ sharedtypes.PatientID) ([]*aggregate.Appointment, error) {
	return nil, nil
}

func (m *mockApptRepo) FindByProfessionalAndDate(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _, _ time.Time) ([]*aggregate.Appointment, error) {
	return nil, nil
}

func (m *mockApptRepo) FindByClinicAndDate(_ context.Context, _ sharedtypes.ClinicID, _ time.Time) ([]*aggregate.Appointment, error) {
	return nil, nil
}

func (m *mockApptRepo) CountActiveByPatient(_ context.Context, _ sharedtypes.PatientID) (int, error) {
	return m.countActiveVal, m.countActiveErr
}

// ── mock schedule repo ────────────────────────────────────────────

type mockScheduleRepo struct {
	schedule  *aggregate.AvailabilitySchedule
	findErr   error
	updateErr error
}

var _ repository.AvailabilityScheduleRepository = (*mockScheduleRepo)(nil)

func (m *mockScheduleRepo) Save(_ context.Context, _ *aggregate.AvailabilitySchedule) error {
	return nil
}

func (m *mockScheduleRepo) Update(_ context.Context, _ *aggregate.AvailabilitySchedule) error {
	return m.updateErr
}

func (m *mockScheduleRepo) FindByProfessionalAndClinic(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID) (*aggregate.AvailabilitySchedule, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if m.schedule == nil {
		return nil, sharederrors.NewNotFound("AvailabilitySchedule", "")
	}
	return m.schedule, nil
}

func (m *mockScheduleRepo) FindByClinic(_ context.Context, _ sharedtypes.ClinicID) ([]*aggregate.AvailabilitySchedule, error) {
	return nil, nil
}

// ── mock cache ────────────────────────────────────────────────────

type mockCache struct {
	lockAcquired bool
	lockErr      error
}

var _ repository.AvailabilityCache = (*mockCache)(nil)

func (m *mockCache) GetSlots(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ string) ([]aggregate.FreeSlot, error) {
	return nil, nil
}

func (m *mockCache) SetSlots(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ string, _ []aggregate.FreeSlot) error {
	return nil
}

func (m *mockCache) InvalidateSchedule(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID) error {
	return nil
}

func (m *mockCache) AcquireSlotLock(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ time.Duration) (bool, error) {
	return m.lockAcquired, m.lockErr
}

func (m *mockCache) ReleaseSlotLock(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time) error {
	return nil
}

// ── mock hold repo ────────────────────────────────────────────────

type mockHoldRepo struct {
	createErr  error
	releaseErr error
}

var _ repository.SlotHoldRepository = (*mockHoldRepo)(nil)

func (m *mockHoldRepo) Create(_ context.Context, _ *repository.SlotHold) error {
	return m.createErr
}

func (m *mockHoldRepo) Release(_ context.Context, _ uuid.UUID) error {
	return m.releaseErr
}

func (m *mockHoldRepo) ActiveStartTimesForDay(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time) ([]time.Time, error) {
	return nil, nil
}

func (m *mockHoldRepo) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

// ── event bus mocks ───────────────────────────────────────────────

type noopBus struct{}

var _ events.Bus = (*noopBus)(nil)

func (noopBus) Publish(_ context.Context, _ events.DomainEvent) error { return nil }
func (noopBus) Subscribe(_ context.Context, _ events.SubscribeOptions, _ events.Handler) error {
	return nil
}
func (noopBus) Close() error { return nil }

type failBus struct{}

var _ events.Bus = (*failBus)(nil)

func (failBus) Publish(_ context.Context, _ events.DomainEvent) error {
	return errors.New("nats down")
}
func (failBus) Subscribe(_ context.Context, _ events.SubscribeOptions, _ events.Handler) error {
	return nil
}
func (failBus) Close() error { return nil }

// ── domain helpers ────────────────────────────────────────────────

// futureSlot returns a 30-minute TimeSlot starting 2 hours from now.
func futureSlot(t *testing.T) valueobject.TimeSlot {
	t.Helper()
	start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Minute)
	slot, err := valueobject.NewTimeSlot(start, start.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("setup: futureSlot: %v", err)
	}
	return slot
}

// allDaySchedule creates an AvailabilitySchedule covering all 7 days from 00:00 to 24:00.
func allDaySchedule(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID) *aggregate.AvailabilitySchedule {
	wh := make([]aggregate.WorkingHour, 7)
	for d := 0; d < 7; d++ {
		wh[d] = aggregate.WorkingHour{Weekday: time.Weekday(d), StartHour: 0, StartMin: 0, EndHour: 24, EndMin: 0}
	}
	return aggregate.NewAvailabilitySchedule(profID, clinicID, wh, map[string]int{})
}

// apptInRepo creates an appointment in the given status and stores it in the repo.
func apptInRepo(t *testing.T, repo *mockApptRepo, status valueobject.AppointmentStatus) *aggregate.Appointment {
	t.Helper()
	slot := futureSlot(t)
	now := time.Now().UTC()
	appt := aggregate.ReconstituteAppointment(
		uuid.New(),
		uuid.New(), uuid.New(),
		uuid.New(), uuid.New(),
		"D1110", slot, status, "OBRA_SOCIAL",
		nil, nil, "", "", "", nil, nil, false,
		now, now, uuid.New(), 1,
	)
	repo.appts[appt.ID()] = appt
	return appt
}

// ── BookAppointmentHandler ────────────────────────────────────────

func TestBookAppointmentHandler(t *testing.T) {
	makeHandler := func(apptRepo *mockApptRepo, schedRepo *mockScheduleRepo, cache *mockCache, bus events.Bus) *command.BookAppointmentHandler {
		s := saga.NewBookAppointmentSaga(apptRepo, schedRepo, cache, service.NewBookingPolicy(), bus)
		return command.NewBookAppointmentHandler(s, apptRepo)
	}

	baseCmd := func(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID, slot valueobject.TimeSlot) command.BookAppointmentCommand {
		return command.BookAppointmentCommand{
			PatientID:      uuid.New(),
			BookedByID:     uuid.New(),
			ProfessionalID: profID,
			ClinicID:       clinicID,
			ProcedureCode:  "D1110",
			SlotStart:      slot.Start,
			SlotEnd:        slot.End,
			CoverageType:   "OBRA_SOCIAL",
			CreatedBy:      uuid.New(),
		}
	}

	t.Run("reserva exitosa retorna AppointmentID y estado Confirmed", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		h := makeHandler(apptRepo, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: true}, noopBus{})

		result, err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.AppointmentID == uuid.Nil {
			t.Error("se esperaba AppointmentID válido")
		}
		if result.Status != "Confirmed" {
			t.Errorf("Status = %q, se esperaba 'Confirmed'", result.Status)
		}
	})

	t.Run("RequiresAuthorization=true → estado Pending", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		h := makeHandler(apptRepo, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: true}, noopBus{})

		cmd := baseCmd(profID, clinicID, slot)
		cmd.RequiresAuthorization = true
		result, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.Status != "Pending" {
			t.Errorf("Status = %q, se esperaba 'Pending'", result.Status)
		}
	})

	t.Run("error en CountActiveByPatient → ErrInternal", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		apptRepo.countActiveErr = errors.New("db timeout")
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		h := makeHandler(apptRepo, &mockScheduleRepo{}, &mockCache{}, noopBus{})

		_, err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if !sharederrors.IsCode(err, sharederrors.ErrInternal) {
			t.Errorf("se esperaba ErrInternal, error = %v", err)
		}
	})

	t.Run("lock no adquirido → ErrConflict", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		h := makeHandler(apptRepo, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: false}, noopBus{})

		_, err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if !sharederrors.IsCode(err, sharederrors.ErrConflict) {
			t.Errorf("se esperaba ErrConflict, error = %v", err)
		}
	})

	t.Run("schedule no encontrado → ErrPrecondition", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		// scheduleRepo con schedule=nil → FindByProfessionalAndClinic retorna NotFound
		h := makeHandler(apptRepo, &mockScheduleRepo{}, &mockCache{lockAcquired: true}, noopBus{})

		_, err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if !sharederrors.IsCode(err, sharederrors.ErrPrecondition) {
			t.Errorf("se esperaba ErrPrecondition, error = %v", err)
		}
	})

	t.Run("slot fuera del horario laboral → ErrConflict", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		// Schedule sin horarios → IsAvailableAt retorna false → ErrConflict
		emptySchedule := aggregate.NewAvailabilitySchedule(profID, clinicID, nil, nil)
		h := makeHandler(apptRepo, &mockScheduleRepo{schedule: emptySchedule}, &mockCache{lockAcquired: true}, noopBus{})

		_, err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if !sharederrors.IsCode(err, sharederrors.ErrConflict) {
			t.Errorf("se esperaba ErrConflict, error = %v", err)
		}
	})

	t.Run("slot en el pasado viola BookingPolicy → ErrPrecondition", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		profID, clinicID := uuid.New(), uuid.New()
		h := makeHandler(apptRepo, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: true}, noopBus{})

		start := time.Now().UTC().Add(-30 * time.Minute)
		cmd := command.BookAppointmentCommand{
			PatientID: uuid.New(), ProfessionalID: profID, ClinicID: clinicID,
			ProcedureCode: "D1110", SlotStart: start, SlotEnd: start.Add(30 * time.Minute),
		}
		_, err := h.Handle(context.Background(), cmd)
		if !sharederrors.IsCode(err, sharederrors.ErrPrecondition) {
			t.Errorf("se esperaba ErrPrecondition, error = %v", err)
		}
	})

	t.Run("slot inválido (end ≤ start) → ErrInvalidArgument", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		profID, clinicID := uuid.New(), uuid.New()
		h := makeHandler(apptRepo, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: true}, noopBus{})

		now := time.Now().UTC()
		cmd := command.BookAppointmentCommand{
			PatientID: uuid.New(), ProfessionalID: profID, ClinicID: clinicID,
			ProcedureCode: "D1110", SlotStart: now.Add(2 * time.Hour), SlotEnd: now.Add(time.Hour),
		}
		_, err := h.Handle(context.Background(), cmd)
		if !sharederrors.IsCode(err, sharederrors.ErrInvalidArgument) {
			t.Errorf("se esperaba ErrInvalidArgument, error = %v", err)
		}
	})

	t.Run("apptRepo.Save falla → error propagado", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		sentinel := errors.New("db down")
		apptRepo.saveErr = sentinel
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		h := makeHandler(apptRepo, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: true}, noopBus{})

		_, err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
	})

	t.Run("error en Publish no aborta la reserva", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		h := makeHandler(apptRepo, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: true}, failBus{})

		result, err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.AppointmentID == uuid.Nil {
			t.Error("se esperaba AppointmentID válido")
		}
	})
}

// ── CancelAppointmentHandler ──────────────────────────────────────

func TestCancelAppointmentHandler(t *testing.T) {
	makeHandler := func(apptRepo *mockApptRepo, schedRepo *mockScheduleRepo, cache *mockCache, bus events.Bus) *command.CancelAppointmentHandler {
		return command.NewCancelAppointmentHandler(apptRepo, schedRepo, cache, bus)
	}

	t.Run("cancelación exitosa de cita Confirmed", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, &mockScheduleRepo{schedule: allDaySchedule(appt.ProfessionalID(), appt.ClinicID())}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.CancelAppointmentCommand{
			AppointmentID: appt.ID(),
			Reason:        "patient_request",
			CancelledBy:   uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("appointment no encontrado → ErrNotFound", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		h := makeHandler(apptRepo, &mockScheduleRepo{}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.CancelAppointmentCommand{
			AppointmentID: uuid.New(),
			Reason:        "patient_request",
			CancelledBy:   uuid.New(),
		})
		if !sharederrors.IsCode(err, sharederrors.ErrNotFound) {
			t.Errorf("se esperaba ErrNotFound, error = %v", err)
		}
	})

	t.Run("appointment en estado terminal → ErrPrecondition", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusCompleted)
		h := makeHandler(apptRepo, &mockScheduleRepo{}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.CancelAppointmentCommand{
			AppointmentID: appt.ID(),
			Reason:        "patient_request",
			CancelledBy:   uuid.New(),
		})
		if !sharederrors.IsCode(err, sharederrors.ErrPrecondition) {
			t.Errorf("se esperaba ErrPrecondition, error = %v", err)
		}
	})

	t.Run("razón de cancelación inválida → ErrInvalidArgument", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, &mockScheduleRepo{}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.CancelAppointmentCommand{
			AppointmentID: appt.ID(),
			Reason:        "motivo_inventado",
			CancelledBy:   uuid.New(),
		})
		if !sharederrors.IsCode(err, sharederrors.ErrInvalidArgument) {
			t.Errorf("se esperaba ErrInvalidArgument, error = %v", err)
		}
	})

	t.Run("apptRepo.Update falla → error propagado", func(t *testing.T) {
		sentinel := errors.New("db down")
		apptRepo := newMockApptRepo()
		apptRepo.updateErr = sentinel
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, &mockScheduleRepo{}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.CancelAppointmentCommand{
			AppointmentID: appt.ID(),
			Reason:        "patient_request",
			CancelledBy:   uuid.New(),
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
	})

	t.Run("scheduleRepo.FindByProfessionalAndClinic falla → no aborta la cancelación", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, &mockScheduleRepo{findErr: errors.New("timeout")}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.CancelAppointmentCommand{
			AppointmentID: appt.ID(),
			Reason:        "staff_request",
			CancelledBy:   uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v (se esperaba nil)", err)
		}
	})

	t.Run("error en Publish no aborta la cancelación", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, &mockScheduleRepo{}, &mockCache{}, failBus{})

		err := h.Handle(context.Background(), command.CancelAppointmentCommand{
			AppointmentID: appt.ID(),
			Reason:        "patient_request",
			CancelledBy:   uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v (se esperaba nil)", err)
		}
	})
}

// ── CompleteAppointmentHandler ────────────────────────────────────

func TestCompleteAppointmentHandler(t *testing.T) {
	makeHandler := func(apptRepo *mockApptRepo, bus events.Bus) *command.CompleteAppointmentHandler {
		return command.NewCompleteAppointmentHandler(apptRepo, bus)
	}

	t.Run("completar cita InProgress → éxito", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusInProgress)
		h := makeHandler(apptRepo, noopBus{})

		err := h.Handle(context.Background(), command.CompleteAppointmentCommand{
			AppointmentID: appt.ID(),
			ClinicalNotes: "todo correcto",
			CompletedBy:   uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("appointment no encontrado → ErrNotFound", func(t *testing.T) {
		h := makeHandler(newMockApptRepo(), noopBus{})

		err := h.Handle(context.Background(), command.CompleteAppointmentCommand{
			AppointmentID: uuid.New(),
		})
		if !sharederrors.IsCode(err, sharederrors.ErrNotFound) {
			t.Errorf("se esperaba ErrNotFound, error = %v", err)
		}
	})

	t.Run("appointment en estado incorrecto (Confirmed) → ErrPrecondition", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, noopBus{})

		err := h.Handle(context.Background(), command.CompleteAppointmentCommand{AppointmentID: appt.ID()})
		if !sharederrors.IsCode(err, sharederrors.ErrPrecondition) {
			t.Errorf("se esperaba ErrPrecondition, error = %v", err)
		}
	})

	t.Run("apptRepo.Update falla → error propagado", func(t *testing.T) {
		sentinel := errors.New("db down")
		apptRepo := newMockApptRepo()
		apptRepo.updateErr = sentinel
		appt := apptInRepo(t, apptRepo, valueobject.StatusInProgress)
		h := makeHandler(apptRepo, noopBus{})

		err := h.Handle(context.Background(), command.CompleteAppointmentCommand{AppointmentID: appt.ID()})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
	})

	t.Run("error en Publish no aborta el completado", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusInProgress)
		h := makeHandler(apptRepo, failBus{})

		err := h.Handle(context.Background(), command.CompleteAppointmentCommand{AppointmentID: appt.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v (se esperaba nil)", err)
		}
	})
}

// ── CheckInAppointmentHandler ─────────────────────────────────────

func TestCheckInAppointmentHandler(t *testing.T) {
	makeHandler := func(apptRepo *mockApptRepo, bus events.Bus) *command.CheckInAppointmentHandler {
		return command.NewCheckInAppointmentHandler(apptRepo, bus)
	}

	t.Run("check-in exitoso de cita Confirmed", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, noopBus{})

		err := h.Handle(context.Background(), command.CheckInAppointmentCommand{
			AppointmentID: appt.ID(),
			CheckedInBy:   uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("appointment no encontrado → ErrNotFound", func(t *testing.T) {
		h := makeHandler(newMockApptRepo(), noopBus{})

		err := h.Handle(context.Background(), command.CheckInAppointmentCommand{AppointmentID: uuid.New()})
		if !sharederrors.IsCode(err, sharederrors.ErrNotFound) {
			t.Errorf("se esperaba ErrNotFound, error = %v", err)
		}
	})

	t.Run("appointment en estado incorrecto (Pending) → ErrPrecondition", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusPending)
		h := makeHandler(apptRepo, noopBus{})

		err := h.Handle(context.Background(), command.CheckInAppointmentCommand{AppointmentID: appt.ID()})
		if !sharederrors.IsCode(err, sharederrors.ErrPrecondition) {
			t.Errorf("se esperaba ErrPrecondition, error = %v", err)
		}
	})

	t.Run("apptRepo.Update falla → error propagado", func(t *testing.T) {
		sentinel := errors.New("db down")
		apptRepo := newMockApptRepo()
		apptRepo.updateErr = sentinel
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, noopBus{})

		err := h.Handle(context.Background(), command.CheckInAppointmentCommand{AppointmentID: appt.ID()})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
	})

	t.Run("error en Publish no aborta el check-in", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, failBus{})

		err := h.Handle(context.Background(), command.CheckInAppointmentCommand{AppointmentID: appt.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v (se esperaba nil)", err)
		}
	})
}

// ── MarkNoShowHandler ─────────────────────────────────────────────

func TestMarkNoShowHandler(t *testing.T) {
	makeHandler := func(apptRepo *mockApptRepo, schedRepo *mockScheduleRepo, cache *mockCache, bus events.Bus) *command.MarkNoShowHandler {
		return command.NewMarkNoShowHandler(apptRepo, schedRepo, cache, bus)
	}

	t.Run("no-show exitoso de cita Confirmed", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		schedRepo := &mockScheduleRepo{schedule: allDaySchedule(appt.ProfessionalID(), appt.ClinicID())}
		h := makeHandler(apptRepo, schedRepo, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.MarkNoShowCommand{
			AppointmentID: appt.ID(),
			MarkedBy:      uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("appointment no encontrado → ErrNotFound", func(t *testing.T) {
		h := makeHandler(newMockApptRepo(), &mockScheduleRepo{}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.MarkNoShowCommand{AppointmentID: uuid.New()})
		if !sharederrors.IsCode(err, sharederrors.ErrNotFound) {
			t.Errorf("se esperaba ErrNotFound, error = %v", err)
		}
	})

	t.Run("appointment en estado terminal → ErrPrecondition", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusCancelled)
		h := makeHandler(apptRepo, &mockScheduleRepo{}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.MarkNoShowCommand{AppointmentID: appt.ID()})
		if !sharederrors.IsCode(err, sharederrors.ErrPrecondition) {
			t.Errorf("se esperaba ErrPrecondition, error = %v", err)
		}
	})

	t.Run("apptRepo.Update falla → error propagado", func(t *testing.T) {
		sentinel := errors.New("db down")
		apptRepo := newMockApptRepo()
		apptRepo.updateErr = sentinel
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, &mockScheduleRepo{}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.MarkNoShowCommand{AppointmentID: appt.ID()})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
	})

	t.Run("scheduleRepo falla → no aborta el no-show", func(t *testing.T) {
		apptRepo := newMockApptRepo()
		appt := apptInRepo(t, apptRepo, valueobject.StatusConfirmed)
		h := makeHandler(apptRepo, &mockScheduleRepo{findErr: errors.New("timeout")}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), command.MarkNoShowCommand{AppointmentID: appt.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v (se esperaba nil)", err)
		}
	})
}

// ── BlockSlotHandler ──────────────────────────────────────────────

func TestBlockSlotHandler(t *testing.T) {
	makeHandler := func(schedRepo *mockScheduleRepo, cache *mockCache, bus events.Bus) *command.BlockSlotHandler {
		return command.NewBlockSlotHandler(schedRepo, cache, bus)
	}

	baseCmd := func(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID, slot valueobject.TimeSlot) command.BlockSlotCommand {
		return command.BlockSlotCommand{
			ProfessionalID: profID,
			ClinicID:       clinicID,
			SlotStart:      slot.Start,
			SlotEnd:        slot.End,
			Reason:         "vacation",
			BlockedBy:      uuid.New(),
		}
	}

	t.Run("bloqueo exitoso", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		schedRepo := &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}
		h := makeHandler(schedRepo, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("schedule no encontrado → error", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		h := makeHandler(&mockScheduleRepo{}, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("slot inválido (end ≤ start) → ErrInvalidArgument", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		schedRepo := &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}
		h := makeHandler(schedRepo, &mockCache{}, noopBus{})

		now := time.Now().UTC()
		err := h.Handle(context.Background(), command.BlockSlotCommand{
			ProfessionalID: profID, ClinicID: clinicID,
			SlotStart: now.Add(2 * time.Hour), SlotEnd: now.Add(time.Hour),
			Reason: "vacation",
		})
		if !sharederrors.IsCode(err, sharederrors.ErrInvalidArgument) {
			t.Errorf("se esperaba ErrInvalidArgument, error = %v", err)
		}
	})

	t.Run("razón inválida → ErrInvalidArgument", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		schedRepo := &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}
		h := makeHandler(schedRepo, &mockCache{}, noopBus{})

		cmd := baseCmd(profID, clinicID, slot)
		cmd.Reason = "razon_inexistente"
		err := h.Handle(context.Background(), cmd)
		if !sharederrors.IsCode(err, sharederrors.ErrInvalidArgument) {
			t.Errorf("se esperaba ErrInvalidArgument, error = %v", err)
		}
	})

	t.Run("slot superpuesto → ErrConflict desde AddBlockedSlot", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		schedule := allDaySchedule(profID, clinicID)
		// Pre-bloquear el mismo slot para causar conflicto
		reason := valueobject.BlockedSlotReason("vacation")
		_ = schedule.AddBlockedSlot(slot, reason, "")
		schedRepo := &mockScheduleRepo{schedule: schedule}
		h := makeHandler(schedRepo, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if !sharederrors.IsCode(err, sharederrors.ErrConflict) {
			t.Errorf("se esperaba ErrConflict, error = %v", err)
		}
	})

	t.Run("scheduleRepo.Update falla → error propagado", func(t *testing.T) {
		sentinel := errors.New("db down")
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		schedRepo := &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID), updateErr: sentinel}
		h := makeHandler(schedRepo, &mockCache{}, noopBus{})

		err := h.Handle(context.Background(), baseCmd(profID, clinicID, slot))
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
	})
}

// ── HoldSlotHandler ───────────────────────────────────────────────

func TestHoldSlotHandler(t *testing.T) {
	makeHandler := func(holdRepo *mockHoldRepo) *command.HoldSlotHandler {
		return command.NewHoldSlotHandler(holdRepo)
	}

	t.Run("hold creado exitosamente retorna HoldID y ExpiresAt", func(t *testing.T) {
		h := makeHandler(&mockHoldRepo{})
		slot := futureSlot(t)

		result, err := h.Handle(context.Background(), command.HoldSlotCommand{
			ProfessionalID: uuid.New(),
			ClinicID:       uuid.New(),
			SlotStart:      slot.Start,
			SlotEnd:        slot.End,
			HeldBy:         uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.HoldID == uuid.Nil {
			t.Error("se esperaba HoldID válido")
		}
		if result.ExpiresAt.IsZero() {
			t.Error("se esperaba ExpiresAt no zero")
		}
		if result.ExpiresAt.Before(time.Now()) {
			t.Error("ExpiresAt debería ser en el futuro")
		}
	})

	t.Run("holdRepo.Create falla → error propagado", func(t *testing.T) {
		sentinel := errors.New("conflict")
		h := makeHandler(&mockHoldRepo{createErr: sentinel})
		slot := futureSlot(t)

		_, err := h.Handle(context.Background(), command.HoldSlotCommand{
			ProfessionalID: uuid.New(), ClinicID: uuid.New(),
			SlotStart: slot.Start, SlotEnd: slot.End, HeldBy: uuid.New(),
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
	})
}

// ── ReleaseHoldHandler ────────────────────────────────────────────

func TestReleaseHoldHandler(t *testing.T) {
	makeHandler := func(holdRepo *mockHoldRepo) *command.ReleaseHoldHandler {
		return command.NewReleaseHoldHandler(holdRepo)
	}

	t.Run("release exitoso", func(t *testing.T) {
		h := makeHandler(&mockHoldRepo{})

		err := h.Handle(context.Background(), command.ReleaseHoldCommand{HoldID: uuid.New()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("holdRepo.Release falla → error propagado", func(t *testing.T) {
		sentinel := errors.New("not found")
		h := makeHandler(&mockHoldRepo{releaseErr: sentinel})

		err := h.Handle(context.Background(), command.ReleaseHoldCommand{HoldID: uuid.New()})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
	})
}
