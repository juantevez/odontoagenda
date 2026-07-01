package nats_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/repository"
	schednat "github.com/juantevez/odontoagenda/context/scheduling/infrastructure/nats"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── mockBus ───────────────────────────────────────────────────────

type mockBus struct {
	handlers     map[string]pkgevents.Handler
	subscribeErr error
}

var _ pkgevents.Bus = (*mockBus)(nil)

func newMockBus() *mockBus {
	return &mockBus{handlers: make(map[string]pkgevents.Handler)}
}

func (m *mockBus) Subscribe(_ context.Context, opts pkgevents.SubscribeOptions, h pkgevents.Handler) error {
	if m.subscribeErr != nil {
		return m.subscribeErr
	}
	m.handlers[opts.Subject] = h
	return nil
}
func (m *mockBus) Publish(_ context.Context, _ pkgevents.DomainEvent) error { return nil }
func (m *mockBus) Close() error                                              { return nil }

// ── mockScheduleRepo ──────────────────────────────────────────────

type mockScheduleRepo struct {
	existing   *aggregate.AvailabilitySchedule
	findErr    error
	saveErr    error
	saveCalled bool
}

var _ repository.AvailabilityScheduleRepository = (*mockScheduleRepo)(nil)

func (m *mockScheduleRepo) Save(_ context.Context, _ *aggregate.AvailabilitySchedule) error {
	m.saveCalled = true
	return m.saveErr
}
func (m *mockScheduleRepo) Update(_ context.Context, _ *aggregate.AvailabilitySchedule) error {
	return nil
}
func (m *mockScheduleRepo) FindByProfessionalAndClinic(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID) (*aggregate.AvailabilitySchedule, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if m.existing == nil {
		return nil, sharederrors.NewNotFound("AvailabilitySchedule", "")
	}
	return m.existing, nil
}
func (m *mockScheduleRepo) FindByClinic(_ context.Context, _ sharedtypes.ClinicID) ([]*aggregate.AvailabilitySchedule, error) {
	return nil, nil
}

// ── mockCache ─────────────────────────────────────────────────────

type mockCache struct {
	invalidateCalled bool
	invalidateErr    error
}

var _ repository.AvailabilityCache = (*mockCache)(nil)

func (m *mockCache) GetSlots(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ string) ([]aggregate.FreeSlot, error) {
	return nil, nil
}
func (m *mockCache) SetSlots(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ string, _ []aggregate.FreeSlot) error {
	return nil
}
func (m *mockCache) InvalidateSchedule(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID) error {
	m.invalidateCalled = true
	return m.invalidateErr
}
func (m *mockCache) AcquireSlotLock(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ time.Duration) (bool, error) {
	return true, nil
}
func (m *mockCache) ReleaseSlotLock(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time) error {
	return nil
}

// ── mockApptRepo ──────────────────────────────────────────────────

type mockApptRepo struct {
	activeAppts      []*aggregate.Appointment
	findActiveErr    error
	findActiveCalled bool
}

var _ repository.AppointmentRepository = (*mockApptRepo)(nil)

func (m *mockApptRepo) Save(_ context.Context, _ *aggregate.Appointment) error      { return nil }
func (m *mockApptRepo) Update(_ context.Context, _ *aggregate.Appointment) error    { return nil }
func (m *mockApptRepo) FindByID(_ context.Context, _ sharedtypes.AppointmentID) (*aggregate.Appointment, error) {
	return nil, sharederrors.NewNotFound("Appointment", "")
}
func (m *mockApptRepo) FindActiveByPatient(_ context.Context, _ sharedtypes.PatientID) ([]*aggregate.Appointment, error) {
	m.findActiveCalled = true
	return m.activeAppts, m.findActiveErr
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

// ── helpers ───────────────────────────────────────────────────────

type deps struct {
	schedRepo *mockScheduleRepo
	cache     *mockCache
	apptRepo  *mockApptRepo
	bus       *mockBus
}

func newDeps() *deps {
	return &deps{
		schedRepo: &mockScheduleRepo{},
		cache:     &mockCache{},
		apptRepo:  &mockApptRepo{},
		bus:       newMockBus(),
	}
}

func (d *deps) subscriber() *schednat.SchedulingEventSubscriber {
	return schednat.NewSchedulingEventSubscriber(d.bus, d.schedRepo, d.cache, d.apptRepo)
}

// registerAll llama RegisterAll y devuelve el mapa de handlers capturados.
func (d *deps) registerAll(t *testing.T) map[string]pkgevents.Handler {
	t.Helper()
	sub := d.subscriber()
	if err := sub.RegisterAll(context.Background()); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}
	return d.bus.handlers
}

// envelope construye un pkgevents.Envelope con el payload serializado.
func envelope(subject string, payload any) pkgevents.Envelope {
	data, _ := json.Marshal(payload)
	return pkgevents.Envelope{
		EventID:   uuid.New().String(),
		EventType: subject,
		Payload:   json.RawMessage(data),
	}
}

// ── RegisterAll ───────────────────────────────────────────────────

func TestRegisterAll_RegistraCuatroHandlers(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)

	expected := []string{
		"professional.assigned_to_clinic",
		"professional.schedule.updated",
		"professional.suspended",
		"patient.archived",
	}
	for _, subj := range expected {
		if _, ok := handlers[subj]; !ok {
			t.Errorf("handler no registrado para subject %q", subj)
		}
	}
	if len(handlers) != len(expected) {
		t.Errorf("handlers registrados = %d, quería %d", len(handlers), len(expected))
	}
}

func TestRegisterAll_RetornaErrorSiBusFalla(t *testing.T) {
	d := newDeps()
	d.bus.subscribeErr = errors.New("NATS no disponible")

	sub := d.subscriber()
	if err := sub.RegisterAll(context.Background()); err == nil {
		t.Fatal("RegisterAll() debería retornar error si bus.Subscribe falla")
	}
}

// ── handleProfessionalAssigned ────────────────────────────────────

func TestHandleProfessionalAssigned_Valido_CreaSchedule(t *testing.T) {
	d := newDeps()
	// schedRepo.existing = nil → schedule no existe → debe crearse
	handlers := d.registerAll(t)
	h := handlers["professional.assigned_to_clinic"]

	env := envelope("professional.assigned_to_clinic", map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"assignment_id":   uuid.New().String(),
	})

	if err := h(context.Background(), env); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if !d.schedRepo.saveCalled {
		t.Error("scheduleRepo.Save no fue llamado")
	}
}

func TestHandleProfessionalAssigned_Idempotente_ScheduleYaExiste(t *testing.T) {
	d := newDeps()
	profID := sharedtypes.ProfessionalID(uuid.New())
	clinicID := sharedtypes.ClinicID(uuid.New())
	d.schedRepo.existing = aggregate.NewAvailabilitySchedule(profID, clinicID, nil, nil)
	handlers := d.registerAll(t)
	h := handlers["professional.assigned_to_clinic"]

	env := envelope("professional.assigned_to_clinic", map[string]any{
		"professional_id": uuid.UUID(profID).String(),
		"clinic_id":       uuid.UUID(clinicID).String(),
	})

	if err := h(context.Background(), env); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if d.schedRepo.saveCalled {
		t.Error("scheduleRepo.Save fue llamado aunque el schedule ya existía")
	}
}

func TestHandleProfessionalAssigned_PayloadInvalido_SkipRetry(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["professional.assigned_to_clinic"]

	env := pkgevents.Envelope{
		EventID:   uuid.New().String(),
		EventType: "professional.assigned_to_clinic",
		Payload:   json.RawMessage(`{esto-no-es-json`),
	}

	err := h(context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestHandleProfessionalAssigned_ProfessionalIDInvalido_SkipRetry(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["professional.assigned_to_clinic"]

	env := envelope("professional.assigned_to_clinic", map[string]any{
		"professional_id": "no-es-uuid",
		"clinic_id":       uuid.New().String(),
	})

	err := h(context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestHandleProfessionalAssigned_ClinicIDInvalido_SkipRetry(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["professional.assigned_to_clinic"]

	env := envelope("professional.assigned_to_clinic", map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       "no-es-uuid",
	})

	err := h(context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestHandleProfessionalAssigned_SaveFalla_PropagaError(t *testing.T) {
	d := newDeps()
	sentinel := errors.New("db timeout")
	d.schedRepo.saveErr = sentinel
	handlers := d.registerAll(t)
	h := handlers["professional.assigned_to_clinic"]

	env := envelope("professional.assigned_to_clinic", map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
	})

	err := h(context.Background(), env)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería el error sentinel", err)
	}
}

// ── handleScheduleUpdated ─────────────────────────────────────────

func TestHandleScheduleUpdated_Valido_InvalidaCache(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["professional.schedule.updated"]

	env := envelope("professional.schedule.updated", map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"change_type":     "working_hours",
	})

	if err := h(context.Background(), env); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if !d.cache.invalidateCalled {
		t.Error("cache.InvalidateSchedule no fue llamado")
	}
}

func TestHandleScheduleUpdated_PayloadInvalido_SkipRetry(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["professional.schedule.updated"]

	env := pkgevents.Envelope{
		EventType: "professional.schedule.updated",
		Payload:   json.RawMessage(`{no-json`),
	}

	err := h(context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestHandleScheduleUpdated_ProfessionalIDInvalido_SkipRetry(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["professional.schedule.updated"]

	env := envelope("professional.schedule.updated", map[string]any{
		"professional_id": "no-es-uuid",
		"clinic_id":       uuid.New().String(),
	})

	err := h(context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestHandleScheduleUpdated_ClinicIDInvalido_SkipRetry(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["professional.schedule.updated"]

	env := envelope("professional.schedule.updated", map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       "no-es-uuid",
	})

	err := h(context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestHandleScheduleUpdated_CacheErrorNoBloquea(t *testing.T) {
	// El error de cache se loguea pero no se propaga — consistencia eventual.
	d := newDeps()
	d.cache.invalidateErr = errors.New("redis no disponible")
	handlers := d.registerAll(t)
	h := handlers["professional.schedule.updated"]

	env := envelope("professional.schedule.updated", map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"change_type":     "working_hours",
	})

	if err := h(context.Background(), env); err != nil {
		t.Errorf("handler() debería retornar nil aunque cache falle, got: %v", err)
	}
}

// ── handleProfessionalSuspended ───────────────────────────────────

func TestHandleProfessionalSuspended_Valido_RetornaNil(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["professional.suspended"]

	env := envelope("professional.suspended", map[string]any{
		"professional_id": uuid.New().String(),
		"reason":          "license_expired",
	})

	if err := h(context.Background(), env); err != nil {
		t.Errorf("handler() error = %v, quería nil", err)
	}
}

func TestHandleProfessionalSuspended_PayloadInvalido_SkipRetry(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["professional.suspended"]

	env := pkgevents.Envelope{
		EventType: "professional.suspended",
		Payload:   json.RawMessage(`{no-json`),
	}

	err := h(context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestHandleProfessionalSuspended_ProfessionalIDInvalido_SkipRetry(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["professional.suspended"]

	env := envelope("professional.suspended", map[string]any{
		"professional_id": "no-es-uuid",
		"reason":          "misconduct",
	})

	err := h(context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

// ── handlePatientArchived ─────────────────────────────────────────

func TestHandlePatientArchived_Valido_ConsultaCitasActivas(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["patient.archived"]

	env := envelope("patient.archived", map[string]any{
		"patient_id":  uuid.New().String(),
		"reason":      "deceased",
		"archived_by": uuid.New().String(),
	})

	if err := h(context.Background(), env); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if !d.apptRepo.findActiveCalled {
		t.Error("apptRepo.FindActiveByPatient no fue llamado")
	}
}

func TestHandlePatientArchived_ConCitasActivas_RetornaNil(t *testing.T) {
	// La implementación actual registra las citas pero no las cancela todavía (TODO).
	d := newDeps()
	d.apptRepo.activeAppts = []*aggregate.Appointment{} // sin citas
	handlers := d.registerAll(t)
	h := handlers["patient.archived"]

	env := envelope("patient.archived", map[string]any{
		"patient_id": uuid.New().String(),
		"reason":     "gdpr_request",
	})

	if err := h(context.Background(), env); err != nil {
		t.Errorf("handler() error = %v, quería nil", err)
	}
}

func TestHandlePatientArchived_PayloadInvalido_SkipRetry(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["patient.archived"]

	env := pkgevents.Envelope{
		EventType: "patient.archived",
		Payload:   json.RawMessage(`{no-json`),
	}

	err := h(context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestHandlePatientArchived_PatientIDInvalido_SkipRetry(t *testing.T) {
	d := newDeps()
	handlers := d.registerAll(t)
	h := handlers["patient.archived"]

	env := envelope("patient.archived", map[string]any{
		"patient_id": "no-es-uuid",
		"reason":     "gdpr_request",
	})

	err := h(context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Errorf("error = %v, quería ErrSkipRetry", err)
	}
}

func TestHandlePatientArchived_RepoFalla_PropagaError(t *testing.T) {
	d := newDeps()
	sentinel := errors.New("db connection lost")
	d.apptRepo.findActiveErr = sentinel
	handlers := d.registerAll(t)
	h := handlers["patient.archived"]

	env := envelope("patient.archived", map[string]any{
		"patient_id": uuid.New().String(),
		"reason":     "deceased",
	})

	err := h(context.Background(), env)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería el error sentinel", err)
	}
}
