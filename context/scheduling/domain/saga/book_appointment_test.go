package saga_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
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
	saveErr     error
	saveCalled  bool
}

var _ repository.AppointmentRepository = (*mockApptRepo)(nil)

func (m *mockApptRepo) Save(_ context.Context, _ *aggregate.Appointment) error {
	m.saveCalled = true
	return m.saveErr
}
func (m *mockApptRepo) Update(_ context.Context, _ *aggregate.Appointment) error { return nil }
func (m *mockApptRepo) FindByID(_ context.Context, _ sharedtypes.AppointmentID) (*aggregate.Appointment, error) {
	return nil, nil
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
	return 0, nil
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
	lockAcquired  bool
	lockErr       error
	releaseCalled bool
	releaseErr    error
	invalidateErr error
}

var _ repository.AvailabilityCache = (*mockCache)(nil)

func (m *mockCache) GetSlots(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ string) ([]aggregate.FreeSlot, error) {
	return nil, nil
}
func (m *mockCache) SetSlots(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ string, _ []aggregate.FreeSlot) error {
	return nil
}
func (m *mockCache) InvalidateSchedule(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID) error {
	return m.invalidateErr
}
func (m *mockCache) AcquireSlotLock(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ time.Duration) (bool, error) {
	return m.lockAcquired, m.lockErr
}
func (m *mockCache) ReleaseSlotLock(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time) error {
	m.releaseCalled = true
	return m.releaseErr
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

// allDaySchedule returns a schedule covering all 7 days 00:00-24:00.
func allDaySchedule(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID) *aggregate.AvailabilitySchedule {
	wh := make([]aggregate.WorkingHour, 7)
	for d := 0; d < 7; d++ {
		wh[d] = aggregate.WorkingHour{Weekday: time.Weekday(d), StartHour: 0, StartMin: 0, EndHour: 24, EndMin: 0}
	}
	return aggregate.NewAvailabilitySchedule(profID, clinicID, wh, map[string]int{})
}

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

// baseInput builds a valid BookAppointmentInput with default constraints.
func baseInput(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID, slot valueobject.TimeSlot) saga.BookAppointmentInput {
	return saga.BookAppointmentInput{
		PatientID:      uuid.New(),
		BookedByID:     uuid.New(),
		ProfessionalID: profID,
		ClinicID:       clinicID,
		ProcedureCode:  "D1110",
		SlotStart:      slot.Start,
		SlotEnd:        slot.End,
		CoverageType:   "OBRA_SOCIAL",
		Constraints:    valueobject.DefaultBookingConstraints(),
		CreatedBy:      uuid.New(),
	}
}

// newSaga creates a BookAppointmentSaga with the given dependencies.
func newSaga(apptRepo repository.AppointmentRepository, schedRepo repository.AvailabilityScheduleRepository, cache repository.AvailabilityCache, bus events.Bus) *saga.BookAppointmentSaga {
	return saga.NewBookAppointmentSaga(apptRepo, schedRepo, cache, service.NewBookingPolicy(), bus)
}

// ── Tests ─────────────────────────────────────────────────────────

func TestBookAppointmentSaga_Execute(t *testing.T) {

	t.Run("reserva exitosa retorna AppointmentID y estado Confirmed", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		cache := &mockCache{lockAcquired: true}
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, cache, noopBus{})

		result, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.AppointmentID == uuid.Nil {
			t.Error("se esperaba AppointmentID válido")
		}
		if result.Status != valueobject.StatusConfirmed {
			t.Errorf("Status = %q, se esperaba Confirmed", result.Status)
		}
	})

	t.Run("RequiresAuthorization=true → estado Pending", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: true}, noopBus{})

		input := baseInput(profID, clinicID, slot)
		input.RequiresAuthorization = true
		result, err := s.Execute(context.Background(), input)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Status != valueobject.StatusPending {
			t.Errorf("Status = %q, se esperaba Pending", result.Status)
		}
	})

	t.Run("slot inválido (end ≤ start) → ErrInvalidArgument", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		now := time.Now().UTC()
		input := saga.BookAppointmentInput{
			ProfessionalID: profID, ClinicID: clinicID,
			SlotStart: now.Add(2 * time.Hour), SlotEnd: now.Add(time.Hour), // end < start
		}
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{}, &mockCache{}, noopBus{})

		_, err := s.Execute(context.Background(), input)
		if !sharederrors.IsCode(err, sharederrors.ErrInvalidArgument) {
			t.Errorf("se esperaba ErrInvalidArgument, error = %v", err)
		}
	})

	t.Run("lock no adquirido → ErrConflict", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: false}, noopBus{})

		_, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if !sharederrors.IsCode(err, sharederrors.ErrConflict) {
			t.Errorf("se esperaba ErrConflict, error = %v", err)
		}
	})

	t.Run("error en AcquireSlotLock con lockAcquired=true → continúa en modo degradado", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		// lockAcquired=true pero cache devuelve error → saga continúa loggeando el error
		cache := &mockCache{lockAcquired: true, lockErr: errors.New("redis timeout")}
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, cache, noopBus{})

		result, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if err != nil {
			t.Fatalf("Execute() error = %v (modo degradado no debe fallar)", err)
		}
		if result == nil || result.AppointmentID == uuid.Nil {
			t.Error("se esperaba resultado válido en modo degradado")
		}
	})

	t.Run("schedule no encontrado (NotFound) → ErrPrecondition", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		// scheduleRepo con schedule=nil → FindByProfessionalAndClinic retorna NotFound
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{}, &mockCache{lockAcquired: true}, noopBus{})

		_, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if !sharederrors.IsCode(err, sharederrors.ErrPrecondition) {
			t.Errorf("se esperaba ErrPrecondition, error = %v", err)
		}
	})

	t.Run("schedule error genérico → ErrInternal", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		schedRepo := &mockScheduleRepo{findErr: errors.New("connection refused")}
		s := newSaga(&mockApptRepo{}, schedRepo, &mockCache{lockAcquired: true}, noopBus{})

		_, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if !sharederrors.IsCode(err, sharederrors.ErrInternal) {
			t.Errorf("se esperaba ErrInternal, error = %v", err)
		}
	})

	t.Run("slot fuera del horario laboral → ErrConflict", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		// Schedule sin horarios → IsAvailableAt = false
		emptySchedule := aggregate.NewAvailabilitySchedule(profID, clinicID, nil, nil)
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{schedule: emptySchedule}, &mockCache{lockAcquired: true}, noopBus{})

		_, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if !sharederrors.IsCode(err, sharederrors.ErrConflict) {
			t.Errorf("se esperaba ErrConflict, error = %v", err)
		}
	})

	t.Run("violación única de BookingPolicy → ErrPrecondition", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		// Slot en 30 min: viola MinAdvanceHours=1 pero no no_past_booking
		soon := time.Now().UTC().Add(30 * time.Minute)
		input := saga.BookAppointmentInput{
			PatientID: uuid.New(), BookedByID: uuid.New(),
			ProfessionalID: profID, ClinicID: clinicID,
			ProcedureCode: "D1110",
			SlotStart:     soon, SlotEnd: soon.Add(30 * time.Minute),
			Constraints: valueobject.DefaultBookingConstraints(),
		}
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: true}, noopBus{})

		_, err := s.Execute(context.Background(), input)
		if !sharederrors.IsCode(err, sharederrors.ErrPrecondition) {
			t.Errorf("se esperaba ErrPrecondition, error = %v", err)
		}
	})

	t.Run("múltiples violaciones → ErrPrecondition con mensaje combinado", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		// Slot en el pasado: dispara no_past_booking + min_advance (≥2 violaciones)
		past := time.Now().UTC().Add(-time.Hour)
		input := saga.BookAppointmentInput{
			PatientID: uuid.New(), BookedByID: uuid.New(),
			ProfessionalID: profID, ClinicID: clinicID,
			ProcedureCode: "D1110",
			SlotStart:     past, SlotEnd: past.Add(30 * time.Minute),
			Constraints:             valueobject.DefaultBookingConstraints(),
			ActiveAppointmentsCount: 5, // también dispara max_active_appointments
		}
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: true}, noopBus{})

		_, err := s.Execute(context.Background(), input)
		if !sharederrors.IsCode(err, sharederrors.ErrPrecondition) {
			t.Fatalf("se esperaba ErrPrecondition, error = %v", err)
		}
		// El mensaje debe indicar que hay restricciones adicionales
		de, _ := sharederrors.As(err)
		if de != nil && len(de.Message) == 0 {
			t.Error("se esperaba mensaje descriptivo en ErrPrecondition")
		}
	})

	t.Run("apptRepo.Save falla → error propagado, lock liberado", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		sentinel := errors.New("db connection lost")
		apptRepo := &mockApptRepo{saveErr: sentinel}
		cache := &mockCache{lockAcquired: true}
		s := newSaga(apptRepo, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, cache, noopBus{})

		_, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
		if !cache.releaseCalled {
			t.Error("ReleaseSlotLock debe llamarse aunque Save falle (defer)")
		}
	})

	t.Run("scheduleRepo.Update falla → saga exitosa (compensación loggeada)", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		apptRepo := &mockApptRepo{}
		schedRepo := &mockScheduleRepo{
			schedule:  allDaySchedule(profID, clinicID),
			updateErr: errors.New("schedule db timeout"),
		}
		s := newSaga(apptRepo, schedRepo, &mockCache{lockAcquired: true}, noopBus{})

		result, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if err != nil {
			t.Fatalf("Execute() error = %v (fallo en Update debe ignorarse)", err)
		}
		if result == nil || result.AppointmentID == uuid.Nil {
			t.Error("se esperaba resultado válido aunque Update falle")
		}
		if !apptRepo.saveCalled {
			t.Error("apptRepo.Save debe haberse llamado")
		}
	})

	t.Run("cache.InvalidateSchedule falla → saga exitosa", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		cache := &mockCache{lockAcquired: true, invalidateErr: errors.New("redis down")}
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, cache, noopBus{})

		result, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if err != nil {
			t.Fatalf("Execute() error = %v (fallo en InvalidateSchedule debe ignorarse)", err)
		}
		if result == nil {
			t.Error("se esperaba resultado válido aunque InvalidateSchedule falle")
		}
	})

	t.Run("eventBus.Publish falla → saga exitosa", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, &mockCache{lockAcquired: true}, failBus{})

		result, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if err != nil {
			t.Fatalf("Execute() error = %v (fallo en Publish debe ignorarse)", err)
		}
		if result == nil {
			t.Error("se esperaba resultado válido aunque Publish falle")
		}
	})

	t.Run("ReleaseSlotLock se ejecuta siempre (defer) aunque la saga falle", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		cache := &mockCache{lockAcquired: true}
		// Forzar fallo en el paso 3 (schedule no encontrado → ErrPrecondition)
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{}, cache, noopBus{})

		_, err := s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if err == nil {
			t.Fatal("se esperaba error para validar el defer")
		}
		if !cache.releaseCalled {
			t.Error("ReleaseSlotLock debe ejecutarse siempre via defer, incluso ante error")
		}
	})

	t.Run("ReleaseSlotLock NO se llama si el lock nunca fue adquirido", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		slot := futureSlot(t)
		cache := &mockCache{lockAcquired: false} // lock no adquirido → retorna antes del defer
		s := newSaga(&mockApptRepo{}, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)}, cache, noopBus{})

		_, _ = s.Execute(context.Background(), baseInput(profID, clinicID, slot))
		if cache.releaseCalled {
			t.Error("ReleaseSlotLock no debe llamarse si el lock no fue adquirido")
		}
	})
}
