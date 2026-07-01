package query_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/application/query"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/repository"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/service"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── mock schedule repo ────────────────────────────────────────────

type mockScheduleRepo struct {
	schedule *aggregate.AvailabilitySchedule
	findErr  error
}

var _ repository.AvailabilityScheduleRepository = (*mockScheduleRepo)(nil)

func (m *mockScheduleRepo) Save(_ context.Context, _ *aggregate.AvailabilitySchedule) error {
	return nil
}
func (m *mockScheduleRepo) Update(_ context.Context, _ *aggregate.AvailabilitySchedule) error {
	return nil
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
	slots  []aggregate.FreeSlot // nil = cache miss; non-nil = cache hit
	getErr error
}

var _ repository.AvailabilityCache = (*mockCache)(nil)

func (m *mockCache) GetSlots(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ string) ([]aggregate.FreeSlot, error) {
	return m.slots, m.getErr
}
func (m *mockCache) SetSlots(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ string, _ []aggregate.FreeSlot) error {
	return nil
}
func (m *mockCache) InvalidateSchedule(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID) error {
	return nil
}
func (m *mockCache) AcquireSlotLock(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ time.Duration) (bool, error) {
	return false, nil
}
func (m *mockCache) ReleaseSlotLock(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time) error {
	return nil
}

// ── mock hold repo ────────────────────────────────────────────────

type mockHoldRepo struct {
	heldTimes []time.Time
	holdErr   error
}

var _ repository.SlotHoldRepository = (*mockHoldRepo)(nil)

func (m *mockHoldRepo) Create(_ context.Context, _ *repository.SlotHold) error        { return nil }
func (m *mockHoldRepo) Release(_ context.Context, _ uuid.UUID) error                   { return nil }
func (m *mockHoldRepo) DeleteExpired(_ context.Context) (int64, error)                 { return 0, nil }
func (m *mockHoldRepo) ActiveStartTimesForDay(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time) ([]time.Time, error) {
	return m.heldTimes, m.holdErr
}

// ── mock appointment repo ─────────────────────────────────────────

type mockApptRepo struct {
	activeAppts   []*aggregate.Appointment
	byProfAppts   []*aggregate.Appointment
	byClinicAppts []*aggregate.Appointment
	activeErr     error
	byProfErr     error
	byClinicErr   error
	calledByProf  bool
	calledByClinic bool
}

var _ repository.AppointmentRepository = (*mockApptRepo)(nil)

func (m *mockApptRepo) Save(_ context.Context, _ *aggregate.Appointment) error   { return nil }
func (m *mockApptRepo) Update(_ context.Context, _ *aggregate.Appointment) error { return nil }
func (m *mockApptRepo) FindByID(_ context.Context, _ sharedtypes.AppointmentID) (*aggregate.Appointment, error) {
	return nil, nil
}
func (m *mockApptRepo) FindActiveByPatient(_ context.Context, _ sharedtypes.PatientID) ([]*aggregate.Appointment, error) {
	return m.activeAppts, m.activeErr
}
func (m *mockApptRepo) FindByProfessionalAndDate(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _, _ time.Time) ([]*aggregate.Appointment, error) {
	m.calledByProf = true
	return m.byProfAppts, m.byProfErr
}
func (m *mockApptRepo) FindByClinicAndDate(_ context.Context, _ sharedtypes.ClinicID, _ time.Time) ([]*aggregate.Appointment, error) {
	m.calledByClinic = true
	return m.byClinicAppts, m.byClinicErr
}
func (m *mockApptRepo) CountActiveByPatient(_ context.Context, _ sharedtypes.PatientID) (int, error) {
	return 0, nil
}

// ── domain helpers ────────────────────────────────────────────────

// allDaySchedule creates a schedule with working hours for all 7 days (00:00-24:00).
func allDaySchedule(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID) *aggregate.AvailabilitySchedule {
	wh := make([]aggregate.WorkingHour, 7)
	for d := 0; d < 7; d++ {
		wh[d] = aggregate.WorkingHour{Weekday: time.Weekday(d), StartHour: 0, StartMin: 0, EndHour: 24, EndMin: 0}
	}
	return aggregate.NewAvailabilitySchedule(profID, clinicID, wh, map[string]int{})
}

// tomorrow returns midnight UTC of the next day.
func tomorrow() time.Time {
	return time.Now().UTC().AddDate(0, 0, 1).Truncate(24 * time.Hour)
}

// newTestAppt builds a reconstituted appointment in the given status.
func newTestAppt(status valueobject.AppointmentStatus) *aggregate.Appointment {
	now := time.Now().UTC()
	start := now.Add(2 * time.Hour).Truncate(time.Minute)
	slot, _ := valueobject.NewTimeSlot(start, start.Add(30*time.Minute))
	return aggregate.ReconstituteAppointment(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"D1110", slot, status, "OBRA_SOCIAL",
		nil, nil, "", "", "", nil, nil, false,
		now, now, uuid.New(), 1,
	)
}

// newFreeSlot builds a FreeSlot at the given start time.
func newFreeSlot(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID, start time.Time) aggregate.FreeSlot {
	slot, _ := valueobject.NewTimeSlot(start, start.Add(30*time.Minute))
	return aggregate.FreeSlot{
		ProfessionalID: profID,
		ClinicID:       clinicID,
		ProcedureCode:  "D1110",
		Slot:           slot,
		DurationMins:   30,
	}
}

// ── GetAvailabilityHandler ────────────────────────────────────────

func TestGetAvailabilityHandler(t *testing.T) {
	calc := service.NewSlotCalculator()

	makeHandler := func(schedRepo *mockScheduleRepo, holdRepo repository.SlotHoldRepository, cache *mockCache) *query.GetAvailabilityHandler {
		return query.NewGetAvailabilityHandler(schedRepo, holdRepo, cache, calc)
	}

	t.Run("cache hit retorna slots sin recalcular", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Minute)
		cachedSlots := []aggregate.FreeSlot{newFreeSlot(profID, clinicID, start)}
		cache := &mockCache{slots: cachedSlots}
		h := makeHandler(&mockScheduleRepo{}, &mockHoldRepo{}, cache)

		result, err := h.Handle(context.Background(), query.GetAvailabilityQuery{
			ProfessionalID: profID, ClinicID: clinicID,
			ProcedureCode: "D1110", Date: tomorrow(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.TotalFree != 1 {
			t.Errorf("TotalFree = %d, se esperaba 1", result.TotalFree)
		}
		if result.ProfessionalID != profID.String() {
			t.Errorf("ProfessionalID = %q, se esperaba %q", result.ProfessionalID, profID.String())
		}
	})

	t.Run("cache hit filtra holds activos", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		s1 := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Minute)
		s2 := s1.Add(30 * time.Minute)
		cachedSlots := []aggregate.FreeSlot{
			newFreeSlot(profID, clinicID, s1),
			newFreeSlot(profID, clinicID, s2),
		}
		// s1 está retenido → debe filtrarse
		holdRepo := &mockHoldRepo{heldTimes: []time.Time{s1.UTC()}}
		h := makeHandler(&mockScheduleRepo{}, holdRepo, &mockCache{slots: cachedSlots})

		result, err := h.Handle(context.Background(), query.GetAvailabilityQuery{
			ProfessionalID: profID, ClinicID: clinicID,
			ProcedureCode: "D1110", Date: tomorrow(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.TotalFree != 1 {
			t.Errorf("TotalFree = %d, se esperaba 1 (s1 filtrado)", result.TotalFree)
		}
	})

	t.Run("error en holdRepo se ignora (devuelve todos los slots)", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Minute)
		cachedSlots := []aggregate.FreeSlot{newFreeSlot(profID, clinicID, start)}
		holdRepo := &mockHoldRepo{holdErr: errors.New("redis down")}
		h := makeHandler(&mockScheduleRepo{}, holdRepo, &mockCache{slots: cachedSlots})

		result, err := h.Handle(context.Background(), query.GetAvailabilityQuery{
			ProfessionalID: profID, ClinicID: clinicID, Date: tomorrow(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.TotalFree != 1 {
			t.Errorf("TotalFree = %d, se esperaba 1", result.TotalFree)
		}
	})

	t.Run("cache miss calcula slots desde schedule", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		schedule := allDaySchedule(profID, clinicID)
		h := makeHandler(
			&mockScheduleRepo{schedule: schedule},
			&mockHoldRepo{},
			&mockCache{}, // slots=nil → cache miss
		)

		result, err := h.Handle(context.Background(), query.GetAvailabilityQuery{
			ProfessionalID: profID, ClinicID: clinicID,
			ProcedureCode: "D1110", Date: tomorrow(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.TotalFree == 0 {
			t.Error("se esperaban slots libres para el día siguiente")
		}
		if result.Date != tomorrow().Format("2006-01-02") {
			t.Errorf("Date = %q, se esperaba %q", result.Date, tomorrow().Format("2006-01-02"))
		}
	})

	t.Run("cache miss: schedule no encontrado → error", func(t *testing.T) {
		h := makeHandler(&mockScheduleRepo{}, nil, &mockCache{})

		_, err := h.Handle(context.Background(), query.GetAvailabilityQuery{
			ProfessionalID: uuid.New(), ClinicID: uuid.New(), Date: tomorrow(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("cache miss: schedule sin horarios → 0 slots", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		emptySchedule := aggregate.NewAvailabilitySchedule(profID, clinicID, nil, nil)
		h := makeHandler(&mockScheduleRepo{schedule: emptySchedule}, nil, &mockCache{})

		result, err := h.Handle(context.Background(), query.GetAvailabilityQuery{
			ProfessionalID: profID, ClinicID: clinicID, Date: tomorrow(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.TotalFree != 0 {
			t.Errorf("TotalFree = %d, se esperaba 0", result.TotalFree)
		}
	})

	t.Run("error en cache.GetSlots se trata como cache miss", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		schedule := allDaySchedule(profID, clinicID)
		cache := &mockCache{getErr: errors.New("redis timeout")} // error → treated as miss
		h := makeHandler(&mockScheduleRepo{schedule: schedule}, &mockHoldRepo{}, cache)

		result, err := h.Handle(context.Background(), query.GetAvailabilityQuery{
			ProfessionalID: profID, ClinicID: clinicID, Date: tomorrow(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.TotalFree == 0 {
			t.Error("se esperaban slots libres calculados desde el schedule")
		}
	})

	t.Run("schedule con durationMins específico para procedimiento", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		wh := []aggregate.WorkingHour{{Weekday: tomorrow().Weekday(), StartHour: 8, StartMin: 0, EndHour: 10, EndMin: 0}}
		schedule := aggregate.NewAvailabilitySchedule(profID, clinicID, wh, map[string]int{"D0150": 60})
		h := makeHandler(&mockScheduleRepo{schedule: schedule}, nil, &mockCache{})

		// Con duración de 60 min en 2 horas: 2 slots (08:00-09:00, 09:00-10:00)
		result, err := h.Handle(context.Background(), query.GetAvailabilityQuery{
			ProfessionalID: profID, ClinicID: clinicID,
			ProcedureCode: "D0150", Date: tomorrow(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.TotalFree != 2 {
			t.Errorf("TotalFree = %d, se esperaban 2 slots de 60 min en 2 horas", result.TotalFree)
		}
	})
}

// ── GetAvailabilityRangeHandler ───────────────────────────────────

func TestGetAvailabilityRangeHandler(t *testing.T) {
	calc := service.NewSlotCalculator()

	makeHandler := func(schedRepo *mockScheduleRepo) *query.GetAvailabilityRangeHandler {
		return query.NewGetAvailabilityRangeHandler(schedRepo, calc)
	}

	t.Run("retorna slots en rango", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		schedule := allDaySchedule(profID, clinicID)
		h := makeHandler(&mockScheduleRepo{schedule: schedule})

		from := tomorrow()
		to := from.AddDate(0, 0, 1)
		result, err := h.Handle(context.Background(), query.GetAvailabilityRangeQuery{
			ProfessionalID: profID, ClinicID: clinicID,
			ProcedureCode: "D1110", From: from, To: to, MaxResults: 5,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(result) == 0 {
			t.Error("se esperaban slots libres en el rango")
		}
		if len(result) > 5 {
			t.Errorf("len = %d, se esperaban ≤ 5 (MaxResults)", len(result))
		}
	})

	t.Run("schedule no encontrado → error", func(t *testing.T) {
		h := makeHandler(&mockScheduleRepo{})

		_, err := h.Handle(context.Background(), query.GetAvailabilityRangeQuery{
			ProfessionalID: uuid.New(), ClinicID: uuid.New(),
			From: tomorrow(), To: tomorrow().AddDate(0, 0, 1), MaxResults: 10,
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("rango inválido (To < From) → ErrInvalidArgument", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		schedule := allDaySchedule(profID, clinicID)
		h := makeHandler(&mockScheduleRepo{schedule: schedule})

		from := tomorrow()
		to := from.AddDate(0, 0, -1) // to < from
		_, err := h.Handle(context.Background(), query.GetAvailabilityRangeQuery{
			ProfessionalID: profID, ClinicID: clinicID,
			From: from, To: to, MaxResults: 10,
		})
		if !sharederrors.IsCode(err, sharederrors.ErrInvalidArgument) {
			t.Errorf("se esperaba ErrInvalidArgument, error = %v", err)
		}
	})

	t.Run("MaxResults=0 usa default (50) y no falla", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		schedule := allDaySchedule(profID, clinicID)
		h := makeHandler(&mockScheduleRepo{schedule: schedule})

		result, err := h.Handle(context.Background(), query.GetAvailabilityRangeQuery{
			ProfessionalID: profID, ClinicID: clinicID,
			ProcedureCode: "D1110", From: tomorrow(), To: tomorrow(), MaxResults: 0,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		// MaxResults=0 → default 50; tomorrow has 48 slots → devuelve 48
		if len(result) == 0 {
			t.Error("se esperaban slots con MaxResults default")
		}
	})

	t.Run("schedule sin horarios en rango → slice vacío", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		emptySchedule := aggregate.NewAvailabilitySchedule(profID, clinicID, nil, nil)
		h := makeHandler(&mockScheduleRepo{schedule: emptySchedule})

		result, err := h.Handle(context.Background(), query.GetAvailabilityRangeQuery{
			ProfessionalID: profID, ClinicID: clinicID,
			From: tomorrow(), To: tomorrow(), MaxResults: 10,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(result) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(result))
		}
	})
}

// ── GetDayScheduleHandler ─────────────────────────────────────────

func TestGetDayScheduleHandler(t *testing.T) {
	calc := service.NewSlotCalculator()

	makeHandler := func(apptRepo *mockApptRepo, schedRepo *mockScheduleRepo) *query.GetDayScheduleHandler {
		return query.NewGetDayScheduleHandler(apptRepo, schedRepo, calc)
	}

	t.Run("con ProfessionalID usa FindByProfessionalAndDate", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		apptRepo := &mockApptRepo{byProfAppts: []*aggregate.Appointment{newTestAppt(valueobject.StatusConfirmed)}}
		h := makeHandler(apptRepo, &mockScheduleRepo{schedule: allDaySchedule(profID, clinicID)})

		result, err := h.Handle(context.Background(), query.GetDayScheduleQuery{
			ProfessionalID: profID, ClinicID: clinicID, Date: tomorrow(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if !apptRepo.calledByProf {
			t.Error("se esperaba llamada a FindByProfessionalAndDate")
		}
		if apptRepo.calledByClinic {
			t.Error("no se esperaba llamada a FindByClinicAndDate")
		}
		if result.TotalBooked != 1 {
			t.Errorf("TotalBooked = %d, se esperaba 1", result.TotalBooked)
		}
	})

	t.Run("sin ProfessionalID (uuid.Nil) usa FindByClinicAndDate", func(t *testing.T) {
		clinicID := uuid.New()
		apptRepo := &mockApptRepo{byClinicAppts: []*aggregate.Appointment{
			newTestAppt(valueobject.StatusConfirmed),
			newTestAppt(valueobject.StatusInProgress),
		}}
		schedRepo := &mockScheduleRepo{} // FindByProfessionalAndClinic falla → FreeSlots vacíos (ignorado)
		h := makeHandler(apptRepo, schedRepo)

		result, err := h.Handle(context.Background(), query.GetDayScheduleQuery{
			ProfessionalID: uuid.Nil, ClinicID: clinicID, Date: tomorrow(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if !apptRepo.calledByClinic {
			t.Error("se esperaba llamada a FindByClinicAndDate")
		}
		if apptRepo.calledByProf {
			t.Error("no se esperaba llamada a FindByProfessionalAndDate")
		}
		if result.TotalBooked != 2 {
			t.Errorf("TotalBooked = %d, se esperaba 2", result.TotalBooked)
		}
	})

	t.Run("apptRepo falla → error", func(t *testing.T) {
		sentinel := errors.New("db down")
		apptRepo := &mockApptRepo{byProfErr: sentinel}
		h := makeHandler(apptRepo, &mockScheduleRepo{})

		_, err := h.Handle(context.Background(), query.GetDayScheduleQuery{
			ProfessionalID: uuid.New(), ClinicID: uuid.New(), Date: tomorrow(),
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
	})

	t.Run("scheduleRepo falla → DayScheduleDTO con FreeSlots vacíos", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		apptRepo := &mockApptRepo{byProfAppts: []*aggregate.Appointment{newTestAppt(valueobject.StatusConfirmed)}}
		h := makeHandler(apptRepo, &mockScheduleRepo{findErr: errors.New("timeout")})

		result, err := h.Handle(context.Background(), query.GetDayScheduleQuery{
			ProfessionalID: profID, ClinicID: clinicID, Date: tomorrow(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v (scheduleRepo error debe ignorarse)", err)
		}
		if result.TotalFree != 0 {
			t.Errorf("TotalFree = %d, se esperaba 0", result.TotalFree)
		}
		if result.TotalBooked != 1 {
			t.Errorf("TotalBooked = %d, se esperaba 1", result.TotalBooked)
		}
	})

	t.Run("DTO incluye fecha y IDs correctos", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		h := makeHandler(&mockApptRepo{}, &mockScheduleRepo{})
		date := tomorrow()

		result, err := h.Handle(context.Background(), query.GetDayScheduleQuery{
			ProfessionalID: profID, ClinicID: clinicID, Date: date,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.ProfessionalID != profID.String() {
			t.Errorf("ProfessionalID = %q, se esperaba %q", result.ProfessionalID, profID.String())
		}
		if result.ClinicID != clinicID.String() {
			t.Errorf("ClinicID = %q, se esperaba %q", result.ClinicID, clinicID.String())
		}
		if result.Date != date.Format("2006-01-02") {
			t.Errorf("Date = %q, se esperaba %q", result.Date, date.Format("2006-01-02"))
		}
	})
}

// ── GetPatientAppointmentsHandler ────────────────────────────────

func TestGetPatientAppointmentsHandler(t *testing.T) {
	makeHandler := func(apptRepo *mockApptRepo) *query.GetPatientAppointmentsHandler {
		return query.NewGetPatientAppointmentsHandler(apptRepo)
	}

	t.Run("OnlyActive=false retorna todas las citas", func(t *testing.T) {
		active := newTestAppt(valueobject.StatusConfirmed)
		completed := newTestAppt(valueobject.StatusCompleted)
		apptRepo := &mockApptRepo{activeAppts: []*aggregate.Appointment{active, completed}}
		h := makeHandler(apptRepo)

		result, err := h.Handle(context.Background(), query.GetPatientAppointmentsQuery{
			PatientID: uuid.New(), OnlyActive: false,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(result) != 2 {
			t.Errorf("len = %d, se esperaban 2 citas", len(result))
		}
	})

	t.Run("OnlyActive=true filtra citas no activas", func(t *testing.T) {
		apptRepo := &mockApptRepo{activeAppts: []*aggregate.Appointment{
			newTestAppt(valueobject.StatusConfirmed),  // activa
			newTestAppt(valueobject.StatusCompleted),  // no activa → filtrada
			newTestAppt(valueobject.StatusInProgress), // activa
			newTestAppt(valueobject.StatusCancelled),  // no activa → filtrada
		}}
		h := makeHandler(apptRepo)

		result, err := h.Handle(context.Background(), query.GetPatientAppointmentsQuery{
			PatientID: uuid.New(), OnlyActive: true,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(result) != 2 {
			t.Errorf("len = %d, se esperaban 2 citas activas", len(result))
		}
		for _, dto := range result {
			if dto.Status == "Completed" || dto.Status == "Cancelled" {
				t.Errorf("cita no activa incluida: status = %q", dto.Status)
			}
		}
	})

	t.Run("apptRepo falla → error", func(t *testing.T) {
		sentinel := errors.New("db timeout")
		apptRepo := &mockApptRepo{activeErr: sentinel}
		h := makeHandler(apptRepo)

		_, err := h.Handle(context.Background(), query.GetPatientAppointmentsQuery{PatientID: uuid.New()})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba envolver %v", err, sentinel)
		}
	})

	t.Run("sin citas → slice vacío (no nil)", func(t *testing.T) {
		h := makeHandler(&mockApptRepo{activeAppts: []*aggregate.Appointment{}})

		result, err := h.Handle(context.Background(), query.GetPatientAppointmentsQuery{PatientID: uuid.New()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result == nil {
			t.Error("se esperaba slice vacío, no nil")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(result))
		}
	})

	t.Run("AppointmentDTO mapea campos correctamente", func(t *testing.T) {
		appt := newTestAppt(valueobject.StatusConfirmed)
		h := makeHandler(&mockApptRepo{activeAppts: []*aggregate.Appointment{appt}})

		result, err := h.Handle(context.Background(), query.GetPatientAppointmentsQuery{PatientID: uuid.New()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("len = %d, se esperaba 1", len(result))
		}
		dto := result[0]
		if dto.ID != appt.ID().String() {
			t.Errorf("ID = %q, se esperaba %q", dto.ID, appt.ID().String())
		}
		if dto.Status != "Confirmed" {
			t.Errorf("Status = %q, se esperaba 'Confirmed'", dto.Status)
		}
		if dto.ProcedureCode != "D1110" {
			t.Errorf("ProcedureCode = %q, se esperaba 'D1110'", dto.ProcedureCode)
		}
		if dto.DurationMins != 30 {
			t.Errorf("DurationMins = %d, se esperaba 30", dto.DurationMins)
		}
	})
}
