package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/application/command"
	"github.com/juantevez/odontoagenda/context/scheduling/application/query"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/repository"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/saga"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/service"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	schedhttp "github.com/juantevez/odontoagenda/context/scheduling/infrastructure/http"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

const (
	testSecret = "sched-test-secret-0123456789abcdef"
	testIssuer = "odontoagenda-test"
)

// ── mockApptRepo ──────────────────────────────────────────────────

type mockApptRepo struct {
	appts         map[sharedtypes.AppointmentID]*aggregate.Appointment
	saveErr       error
	updateErr     error
	findByIDErr   error
	countActive   int
	countActiveErr error
	byClinicResult []*aggregate.Appointment
	byProfResult   []*aggregate.Appointment
	activeResult   []*aggregate.Appointment
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
	return m.activeResult, nil
}
func (m *mockApptRepo) FindByProfessionalAndDate(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _, _ time.Time) ([]*aggregate.Appointment, error) {
	return m.byProfResult, nil
}
func (m *mockApptRepo) FindByClinicAndDate(_ context.Context, _ sharedtypes.ClinicID, _ time.Time) ([]*aggregate.Appointment, error) {
	return m.byClinicResult, nil
}
func (m *mockApptRepo) CountActiveByPatient(_ context.Context, _ sharedtypes.PatientID) (int, error) {
	return m.countActive, m.countActiveErr
}

// ── mockScheduleRepo ──────────────────────────────────────────────

type mockScheduleRepo struct {
	schedule       *aggregate.AvailabilitySchedule
	findErr        error
	byClinicResult []*aggregate.AvailabilitySchedule
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
	return m.byClinicResult, nil
}

// ── mockCache ─────────────────────────────────────────────────────

type mockCache struct {
	slots        []aggregate.FreeSlot // nil = cache miss
	lockAcquired bool
	lockErr      error
}

var _ repository.AvailabilityCache = (*mockCache)(nil)

func (m *mockCache) GetSlots(_ context.Context, _ sharedtypes.ProfessionalID, _ sharedtypes.ClinicID, _ time.Time, _ string) ([]aggregate.FreeSlot, error) {
	return m.slots, nil
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

// ── mockHoldRepo ──────────────────────────────────────────────────

type mockHoldRepo struct {
	lastHold *repository.SlotHold
	createErr error
	releaseErr error
}

var _ repository.SlotHoldRepository = (*mockHoldRepo)(nil)

func (m *mockHoldRepo) Create(_ context.Context, hold *repository.SlotHold) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.lastHold = hold
	return nil
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

// ── noopBus ───────────────────────────────────────────────────────

type noopBus struct{}

var _ events.Bus = (*noopBus)(nil)

func (noopBus) Publish(_ context.Context, _ events.DomainEvent) error { return nil }
func (noopBus) Subscribe(_ context.Context, _ events.SubscribeOptions, _ events.Handler) error {
	return nil
}
func (noopBus) Close() error { return nil }

// ── testServer ────────────────────────────────────────────────────

type testServer struct {
	router       chi.Router
	apptRepo     *mockApptRepo
	scheduleRepo *mockScheduleRepo
	cache        *mockCache
	holdRepo     *mockHoldRepo
}

func newTestServer() *testServer {
	apptRepo := newMockApptRepo()
	schedRepo := &mockScheduleRepo{}
	cache := &mockCache{lockAcquired: true}
	holdRepo := &mockHoldRepo{}
	bus := &noopBus{}
	calc := service.NewSlotCalculator()
	policy := service.NewBookingPolicy()

	bookSaga := saga.NewBookAppointmentSaga(apptRepo, schedRepo, cache, policy, bus)

	bookH := command.NewBookAppointmentHandler(bookSaga, apptRepo)
	cancelH := command.NewCancelAppointmentHandler(apptRepo, schedRepo, cache, bus)
	completeH := command.NewCompleteAppointmentHandler(apptRepo, bus)
	checkInH := command.NewCheckInAppointmentHandler(apptRepo, bus)
	noShowH := command.NewMarkNoShowHandler(apptRepo, schedRepo, cache, bus)
	blockSlotH := command.NewBlockSlotHandler(schedRepo, cache, bus)
	holdH := command.NewHoldSlotHandler(holdRepo)
	releaseH := command.NewReleaseHoldHandler(holdRepo)

	getAvailH := query.NewGetAvailabilityHandler(schedRepo, holdRepo, cache, calc)
	getAvailRangeH := query.NewGetAvailabilityRangeHandler(schedRepo, calc)
	getDayH := query.NewGetDayScheduleHandler(apptRepo, schedRepo, calc)
	getPatientH := query.NewGetPatientAppointmentsHandler(apptRepo)

	jwtCfg := middleware.JWTConfig{SecretKey: []byte(testSecret), Issuer: testIssuer}
	r := chi.NewRouter()
	schedhttp.RegisterRoutes(r, jwtCfg,
		bookH, cancelH, completeH, checkInH, noShowH, blockSlotH, holdH, releaseH,
		getAvailH, getAvailRangeH, getDayH, getPatientH,
	)
	return &testServer{
		router: r, apptRepo: apptRepo, scheduleRepo: schedRepo,
		cache: cache, holdRepo: holdRepo,
	}
}

func (s *testServer) do(t *testing.T, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Buffer
	if body != nil {
		reqBody = bytes.NewBuffer(body)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// ── JWT helpers ───────────────────────────────────────────────────

func makeToken(t *testing.T, role middleware.Role) string {
	t.Helper()
	userID := uuid.New()
	claims := &middleware.UserClaims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuer,
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("makeToken: %v", err)
	}
	return tok
}

func staffToken(t *testing.T) string  { return makeToken(t, middleware.RoleReceptionist) }
func adminToken(t *testing.T) string  { return makeToken(t, middleware.RoleClinicAdmin) }
func profToken(t *testing.T) string   { return makeToken(t, middleware.RoleProfessional) }
func patientToken(t *testing.T) string { return makeToken(t, middleware.RolePatient) }

// ── domain helpers ─────────────────────────────────────────────────

// tomorrowSlot devuelve start/end RFC3339 para mañana a las 10:00-10:30 UTC.
func tomorrowSlot() (start, end time.Time) {
	d := time.Now().UTC().Add(24 * time.Hour)
	start = time.Date(d.Year(), d.Month(), d.Day(), 10, 0, 0, 0, time.UTC)
	end = start.Add(30 * time.Minute)
	return
}

// allDaySchedule devuelve un AvailabilitySchedule activo con horario 8-18 para todos los días de la semana.
func allDaySchedule(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID) *aggregate.AvailabilitySchedule {
	var wh []aggregate.WorkingHour
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		wh = append(wh, aggregate.WorkingHour{Weekday: wd, StartHour: 8, StartMin: 0, EndHour: 18, EndMin: 0})
	}
	return aggregate.ReconstituteSchedule(
		uuid.New(), profID, clinicID,
		wh,
		[]aggregate.ExceptionDay{},
		[]aggregate.BlockedSlot{},
		[]aggregate.BookedSlot{},
		map[string]int{"D0150": 30},
		true, time.Now().UTC(), 1,
	)
}

// pendingAppt crea una cita en estado Pending (requiresAuthorization=true).
func pendingAppt(t *testing.T, apptRepo *mockApptRepo) *aggregate.Appointment {
	t.Helper()
	start, end := tomorrowSlot()
	slot, _ := valueobject.NewTimeSlot(start, end)
	appt, err := aggregate.NewAppointment(
		sharedtypes.PatientID(uuid.New()),
		sharedtypes.PatientID(uuid.New()),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
		"D0150", slot, "", nil, true, uuid.New(), // requiresAuthorization=true → Pending
	)
	if err != nil {
		t.Fatalf("pendingAppt: NewAppointment: %v", err)
	}
	appt.PendingEvents()
	apptRepo.appts[appt.ID()] = appt
	return appt
}

// confirmedAppt crea una cita en estado Confirmed (requiresAuthorization=false).
func confirmedAppt(t *testing.T, apptRepo *mockApptRepo) *aggregate.Appointment {
	t.Helper()
	start, end := tomorrowSlot()
	slot, _ := valueobject.NewTimeSlot(start, end)
	appt, err := aggregate.NewAppointment(
		sharedtypes.PatientID(uuid.New()),
		sharedtypes.PatientID(uuid.New()),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
		"D0150", slot, "", nil, false, uuid.New(), // requiresAuthorization=false → Confirmed
	)
	if err != nil {
		t.Fatalf("confirmedAppt: NewAppointment: %v", err)
	}
	appt.PendingEvents()
	apptRepo.appts[appt.ID()] = appt
	return appt
}

func bodyJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ── GET /scheduling/availability ─────────────────────────────────

func TestGetAvailability_400_MissingProfessionalID(t *testing.T) {
	s := newTestServer()
	tok := staffToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/availability?clinic_id=%s&date=2026-08-01", uuid.New()),
		tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestGetAvailability_400_MissingClinicID(t *testing.T) {
	s := newTestServer()
	tok := staffToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/availability?professional_id=%s&date=2026-08-01", uuid.New()),
		tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestGetAvailability_400_InvalidDate(t *testing.T) {
	s := newTestServer()
	tok := staffToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/availability?professional_id=%s&clinic_id=%s&date=no-es-fecha",
			uuid.New(), uuid.New()),
		tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestGetAvailability_401_SinToken(t *testing.T) {
	s := newTestServer()
	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/availability?professional_id=%s&clinic_id=%s&date=2026-08-01",
			uuid.New(), uuid.New()),
		"", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, quería 401", rec.Code)
	}
}

func TestGetAvailability_200_CacheHit(t *testing.T) {
	s := newTestServer()
	start, end := tomorrowSlot()
	slot, _ := valueobject.NewTimeSlot(start, end)
	s.cache.slots = []aggregate.FreeSlot{
		{ProfessionalID: sharedtypes.ProfessionalID(uuid.New()), Slot: slot, DurationMins: 30},
	}
	tok := staffToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/availability?professional_id=%s&clinic_id=%s&date=2026-08-01",
			uuid.New(), uuid.New()),
		tok, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// ── GET /scheduling/availability/range ───────────────────────────

func TestGetAvailabilityRange_400_InvalidFrom(t *testing.T) {
	s := newTestServer()
	tok := staffToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/availability/range?professional_id=%s&clinic_id=%s&from=bad&to=2026-08-10",
			uuid.New(), uuid.New()),
		tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestGetAvailabilityRange_400_InvalidTo(t *testing.T) {
	s := newTestServer()
	tok := staffToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/availability/range?professional_id=%s&clinic_id=%s&from=2026-08-01&to=bad",
			uuid.New(), uuid.New()),
		tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestGetAvailabilityRange_200_ConSchedule(t *testing.T) {
	s := newTestServer()
	profID := sharedtypes.ProfessionalID(uuid.New())
	clinicID := sharedtypes.ClinicID(uuid.New())
	s.scheduleRepo.schedule = allDaySchedule(profID, clinicID)
	tok := staffToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/availability/range?professional_id=%s&clinic_id=%s&from=2026-08-01&to=2026-08-03&procedure_code=D0150",
			profID, clinicID),
		tok, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// ── GET /scheduling/day-schedule ─────────────────────────────────

func TestGetDaySchedule_200_PorClinica(t *testing.T) {
	s := newTestServer()
	s.apptRepo.byClinicResult = []*aggregate.Appointment{}
	tok := staffToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/day-schedule?clinic_id=%s&date=2026-08-01", uuid.New()),
		tok, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGetDaySchedule_200_SinDateUsaHoy(t *testing.T) {
	s := newTestServer()
	s.apptRepo.byClinicResult = []*aggregate.Appointment{}
	tok := staffToken(t)

	// Sin date → handler usa time.Now().UTC()
	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/day-schedule?clinic_id=%s", uuid.New()),
		tok, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// ── GET /scheduling/patients/{patientId}/appointments ─────────────

func TestGetPatientAppointments_400_PatientIDInvalido(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)

	rec := s.do(t, http.MethodGet, "/scheduling/patients/no-es-uuid/appointments", tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestGetPatientAppointments_200_ListaVacia(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/patients/%s/appointments", uuid.New()),
		tok, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGetPatientAppointments_200_ConCitas(t *testing.T) {
	s := newTestServer()
	appt := pendingAppt(t, s.apptRepo)
	s.apptRepo.activeResult = []*aggregate.Appointment{appt}
	tok := patientToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/patients/%s/appointments?only_active=true", appt.PatientID()),
		tok, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("len = %d, quería 1", len(result))
	}
}

// ── POST /scheduling/appointments (BookAppointment) ────────────────

func TestBookAppointment_401_SinClaims(t *testing.T) {
	s := newTestServer()

	rec := s.do(t, http.MethodPost, "/scheduling/appointments", "", bodyJSON(map[string]any{}))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, quería 401", rec.Code)
	}
}

func TestBookAppointment_400_CuerpoInvalido(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)

	rec := s.do(t, http.MethodPost, "/scheduling/appointments", tok, []byte("no-es-json{"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestBookAppointment_400_PatientIDInvalido(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)
	start, end := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"patient_id":      "no-es-uuid",
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"procedure_code":  "D0150",
		"slot_start":      start.Format(time.RFC3339),
		"slot_end":        end.Format(time.RFC3339),
	})

	rec := s.do(t, http.MethodPost, "/scheduling/appointments", tok, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestBookAppointment_400_SlotStartInvalido(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)
	body := bodyJSON(map[string]any{
		"patient_id":      uuid.New().String(),
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"procedure_code":  "D0150",
		"slot_start":      "no-es-rfc3339",
		"slot_end":        time.Now().Add(25 * time.Hour).Format(time.RFC3339),
	})

	rec := s.do(t, http.MethodPost, "/scheduling/appointments", tok, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestBookAppointment_400_SlotEndInvalido(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)
	start, _ := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"patient_id":      uuid.New().String(),
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"procedure_code":  "D0150",
		"slot_start":      start.Format(time.RFC3339),
		"slot_end":        "no-es-rfc3339",
	})

	rec := s.do(t, http.MethodPost, "/scheduling/appointments", tok, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestBookAppointment_201_Exitoso(t *testing.T) {
	s := newTestServer()
	profID := uuid.New()
	clinicID := uuid.New()
	s.scheduleRepo.schedule = allDaySchedule(
		sharedtypes.ProfessionalID(profID),
		sharedtypes.ClinicID(clinicID),
	)
	tok := patientToken(t)
	start, end := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"patient_id":      uuid.New().String(),
		"professional_id": profID.String(),
		"clinic_id":       clinicID.String(),
		"procedure_code":  "D0150",
		"slot_start":      start.Format(time.RFC3339),
		"slot_end":        end.Format(time.RFC3339),
	})

	rec := s.do(t, http.MethodPost, "/scheduling/appointments", tok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, err := uuid.Parse(resp["appointment_id"]); err != nil {
		t.Errorf("appointment_id no es UUID válido: %q", resp["appointment_id"])
	}
	if resp["status"] == "" {
		t.Error("status vacío en respuesta")
	}
}

// ── POST /scheduling/appointments/{appointmentId}/cancel ──────────

func TestCancelAppointment_401_SinClaims(t *testing.T) {
	s := newTestServer()
	appt := pendingAppt(t, s.apptRepo)

	url := fmt.Sprintf("/scheduling/appointments/%s/cancel", appt.ID())
	rec := s.do(t, http.MethodPost, url, "", bodyJSON(map[string]any{"reason": "patient_request"}))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, quería 401", rec.Code)
	}
}

func TestCancelAppointment_400_IDInvalido(t *testing.T) {
	s := newTestServer()
	tok := staffToken(t)

	rec := s.do(t, http.MethodPost, "/scheduling/appointments/no-es-uuid/cancel", tok,
		bodyJSON(map[string]any{"reason": "patient_request"}))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestCancelAppointment_404_NoExiste(t *testing.T) {
	s := newTestServer()
	tok := staffToken(t)

	url := fmt.Sprintf("/scheduling/appointments/%s/cancel", uuid.New())
	rec := s.do(t, http.MethodPost, url, tok, bodyJSON(map[string]any{"reason": "patient_request"}))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, quería 404", rec.Code)
	}
}

func TestCancelAppointment_204_Exitoso(t *testing.T) {
	s := newTestServer()
	appt := pendingAppt(t, s.apptRepo)
	tok := staffToken(t)

	url := fmt.Sprintf("/scheduling/appointments/%s/cancel", appt.ID())
	rec := s.do(t, http.MethodPost, url, tok,
		bodyJSON(map[string]any{"reason": "patient_request", "note": "el paciente llamó"}))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// ── POST /scheduling/appointments/{appointmentId}/check-in ─────────

func TestCheckIn_400_IDInvalido(t *testing.T) {
	s := newTestServer()
	tok := staffToken(t)

	rec := s.do(t, http.MethodPost, "/scheduling/appointments/invalido/check-in", tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestCheckIn_204_Exitoso(t *testing.T) {
	s := newTestServer()
	appt := confirmedAppt(t, s.apptRepo)
	tok := staffToken(t)

	url := fmt.Sprintf("/scheduling/appointments/%s/check-in", appt.ID())
	rec := s.do(t, http.MethodPost, url, tok, nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// ── POST /scheduling/appointments/{appointmentId}/complete ─────────

func TestComplete_400_IDInvalido(t *testing.T) {
	s := newTestServer()
	tok := profToken(t)

	rec := s.do(t, http.MethodPost, "/scheduling/appointments/invalido/complete", tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestComplete_204_Exitoso(t *testing.T) {
	s := newTestServer()
	// Appointment debe estar InProgress para poder completarse.
	appt := confirmedAppt(t, s.apptRepo)
	if err := appt.CheckIn(); err != nil {
		t.Fatalf("CheckIn: %v", err)
	}
	appt.PendingEvents()
	s.apptRepo.appts[appt.ID()] = appt
	tok := profToken(t)

	url := fmt.Sprintf("/scheduling/appointments/%s/complete", appt.ID())
	rec := s.do(t, http.MethodPost, url, tok, bodyJSON(map[string]any{"clinical_notes": "todo bien"}))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// ── POST /scheduling/appointments/{appointmentId}/no-show ──────────

func TestMarkNoShow_204_Exitoso(t *testing.T) {
	s := newTestServer()
	appt := confirmedAppt(t, s.apptRepo)
	tok := staffToken(t)

	url := fmt.Sprintf("/scheduling/appointments/%s/no-show", appt.ID())
	rec := s.do(t, http.MethodPost, url, tok, nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestMarkNoShow_404_NoExiste(t *testing.T) {
	s := newTestServer()
	tok := staffToken(t)

	url := fmt.Sprintf("/scheduling/appointments/%s/no-show", uuid.New())
	rec := s.do(t, http.MethodPost, url, tok, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, quería 404", rec.Code)
	}
}

// ── POST /scheduling/block-slot ───────────────────────────────────

func TestBlockSlot_401_SinClaims(t *testing.T) {
	s := newTestServer()
	start, end := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"slot_start":      start.Format(time.RFC3339),
		"slot_end":        end.Format(time.RFC3339),
		"reason":          "vacation",
	})

	rec := s.do(t, http.MethodPost, "/scheduling/block-slot", "", body)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, quería 401", rec.Code)
	}
}

func TestBlockSlot_400_CuerpoInvalido(t *testing.T) {
	s := newTestServer()
	tok := adminToken(t)

	rec := s.do(t, http.MethodPost, "/scheduling/block-slot", tok, []byte("{no-json"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestBlockSlot_204_Exitoso(t *testing.T) {
	s := newTestServer()
	profID := uuid.New()
	clinicID := uuid.New()
	s.scheduleRepo.schedule = allDaySchedule(
		sharedtypes.ProfessionalID(profID),
		sharedtypes.ClinicID(clinicID),
	)
	tok := adminToken(t)
	start, end := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"professional_id": profID.String(),
		"clinic_id":       clinicID.String(),
		"slot_start":      start.Format(time.RFC3339),
		"slot_end":        end.Format(time.RFC3339),
		"reason":          "vacation",
	})

	rec := s.do(t, http.MethodPost, "/scheduling/block-slot", tok, body)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// ── POST /scheduling/hold ─────────────────────────────────────────

func TestHoldSlot_401_SinClaims(t *testing.T) {
	s := newTestServer()
	start, end := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"slot_start":      start.Format(time.RFC3339),
		"slot_end":        end.Format(time.RFC3339),
	})

	rec := s.do(t, http.MethodPost, "/scheduling/hold", "", body)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, quería 401", rec.Code)
	}
}

func TestHoldSlot_400_CuerpoInvalido(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)

	rec := s.do(t, http.MethodPost, "/scheduling/hold", tok, []byte("{no-json"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestHoldSlot_400_ProfessionalIDInvalido(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)
	start, end := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"professional_id": "no-es-uuid",
		"clinic_id":       uuid.New().String(),
		"slot_start":      start.Format(time.RFC3339),
		"slot_end":        end.Format(time.RFC3339),
	})

	rec := s.do(t, http.MethodPost, "/scheduling/hold", tok, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestHoldSlot_400_ClinicIDInvalido(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)
	start, end := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       "no-es-uuid",
		"slot_start":      start.Format(time.RFC3339),
		"slot_end":        end.Format(time.RFC3339),
	})

	rec := s.do(t, http.MethodPost, "/scheduling/hold", tok, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestHoldSlot_400_SlotStartInvalido(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)
	_, end := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"slot_start":      "no-es-rfc3339",
		"slot_end":        end.Format(time.RFC3339),
	})

	rec := s.do(t, http.MethodPost, "/scheduling/hold", tok, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestHoldSlot_201_Exitoso(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)
	start, end := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"slot_start":      start.Format(time.RFC3339),
		"slot_end":        end.Format(time.RFC3339),
	})

	rec := s.do(t, http.MethodPost, "/scheduling/hold", tok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, err := uuid.Parse(resp["hold_id"]); err != nil {
		t.Errorf("hold_id no es UUID válido: %q", resp["hold_id"])
	}
	if resp["expires_at"] == "" {
		t.Error("expires_at vacío en respuesta")
	}
}

func TestHoldSlot_409_Conflicto(t *testing.T) {
	s := newTestServer()
	s.holdRepo.createErr = sharederrors.NewConflict("slot already held by another user", nil)
	tok := patientToken(t)
	start, end := tomorrowSlot()
	body := bodyJSON(map[string]any{
		"professional_id": uuid.New().String(),
		"clinic_id":       uuid.New().String(),
		"slot_start":      start.Format(time.RFC3339),
		"slot_end":        end.Format(time.RFC3339),
	})

	rec := s.do(t, http.MethodPost, "/scheduling/hold", tok, body)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, quería 409", rec.Code)
	}
}

// ── DELETE /scheduling/hold/{holdId} ─────────────────────────────

func TestReleaseHold_400_IDInvalido(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)

	rec := s.do(t, http.MethodDelete, "/scheduling/hold/no-es-uuid", tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", rec.Code)
	}
}

func TestReleaseHold_204_Exitoso(t *testing.T) {
	s := newTestServer()
	tok := patientToken(t)

	url := fmt.Sprintf("/scheduling/hold/%s", uuid.New())
	rec := s.do(t, http.MethodDelete, url, tok, nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// ── writeErrorFromDomain — domain error mapping ───────────────────

func TestErrorBody_ContieneCodigo(t *testing.T) {
	s := newTestServer()
	tok := staffToken(t)

	// not found → 404 con código en cuerpo
	url := fmt.Sprintf("/scheduling/appointments/%s/cancel", uuid.New())
	rec := s.do(t, http.MethodPost, url, tok, bodyJSON(map[string]any{"reason": "patient_request"}))

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] == "" {
		t.Errorf("respuesta de error sin campo 'code': %s", rec.Body.String())
	}
	if body["message"] == "" {
		t.Errorf("respuesta de error sin campo 'message': %s", rec.Body.String())
	}
}

// ── Content-Type ──────────────────────────────────────────────────

func TestResponseContentType_EsJSON(t *testing.T) {
	s := newTestServer()
	s.apptRepo.byClinicResult = []*aggregate.Appointment{}
	tok := staffToken(t)

	rec := s.do(t, http.MethodGet,
		fmt.Sprintf("/scheduling/day-schedule?clinic_id=%s&date=2026-08-01", uuid.New()),
		tok, nil)

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, quería application/json", ct)
	}
}
