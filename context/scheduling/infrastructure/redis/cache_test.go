package redis_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	schedredis "github.com/juantevez/odontoagenda/context/scheduling/infrastructure/redis"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── mockRedis ─────────────────────────────────────────────────────

type mockRedis struct {
	// comportamiento Get
	store  map[string]string
	getErr error

	// comportamiento Set
	setErr    error
	lastSetKey   string
	lastSetValue string
	lastSetTTL   time.Duration

	// comportamiento Del
	delErr  error
	delKeys []string

	// comportamiento Keys
	keysResult  []string
	keysErr     error
	lastKeysPattern string

	// comportamiento SetNX
	setNXResult bool
	setNXErr    error
	lastSetNXKey string
	lastSetNXTTL time.Duration

	// comportamiento GetDel
	getDelResult string
	getDelErr    error
	lastGetDelKey string
}

var _ schedredis.RedisClient = (*mockRedis)(nil)

func newMockRedis() *mockRedis {
	return &mockRedis{store: make(map[string]string)}
}

func (m *mockRedis) Get(_ context.Context, key string) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	v, ok := m.store[key]
	if !ok {
		return "", errors.New("redis: nil") // simula cache miss
	}
	return v, nil
}

func (m *mockRedis) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.lastSetKey = key
	m.lastSetValue = value
	m.lastSetTTL = ttl
	if m.setErr != nil {
		return m.setErr
	}
	m.store[key] = value
	return nil
}

func (m *mockRedis) Del(_ context.Context, keys ...string) error {
	m.delKeys = keys
	if m.delErr != nil {
		return m.delErr
	}
	for _, k := range keys {
		delete(m.store, k)
	}
	return nil
}

func (m *mockRedis) Keys(_ context.Context, pattern string) ([]string, error) {
	m.lastKeysPattern = pattern
	if m.keysErr != nil {
		return nil, m.keysErr
	}
	return m.keysResult, nil
}

func (m *mockRedis) SetNX(_ context.Context, key, _ string, ttl time.Duration) (bool, error) {
	m.lastSetNXKey = key
	m.lastSetNXTTL = ttl
	if m.setNXErr != nil {
		return false, m.setNXErr
	}
	return m.setNXResult, nil
}

func (m *mockRedis) GetDel(_ context.Context, key string) (string, error) {
	m.lastGetDelKey = key
	if m.getDelErr != nil {
		return "", m.getDelErr
	}
	v := m.getDelResult
	return v, nil
}

// ── helpers ───────────────────────────────────────────────────────

func newCache(r *mockRedis) *schedredis.AvailabilityCacheRedis {
	return schedredis.NewAvailabilityCacheRedis(r)
}

func testIDs() (sharedtypes.ProfessionalID, sharedtypes.ClinicID) {
	return sharedtypes.ProfessionalID(uuid.New()), sharedtypes.ClinicID(uuid.New())
}

func freeSlot(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID) aggregate.FreeSlot {
	start := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	slot, _ := valueobject.NewTimeSlot(start, start.Add(30*time.Minute))
	return aggregate.FreeSlot{
		ProfessionalID: profID,
		ClinicID:       clinicID,
		ProcedureCode:  "D0150",
		Slot:           slot,
		DurationMins:   30,
	}
}

func slotsKey(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID, date time.Time, code string) string {
	return fmt.Sprintf("scheduling:availability:%s:%s:%s:%s",
		uuid.UUID(profID).String(),
		uuid.UUID(clinicID).String(),
		date.Format("2006-01-02"),
		code,
	)
}

func lockKey(profID sharedtypes.ProfessionalID, clinicID sharedtypes.ClinicID, slotStart time.Time) string {
	return fmt.Sprintf("scheduling:slot_lock:%s:%s:%d",
		uuid.UUID(profID).String(),
		uuid.UUID(clinicID).String(),
		slotStart.Unix(),
	)
}

// ── NewAvailabilityCacheRedis ─────────────────────────────────────

func TestNewAvailabilityCacheRedis_NotNil(t *testing.T) {
	c := schedredis.NewAvailabilityCacheRedis(newMockRedis())
	if c == nil {
		t.Fatal("NewAvailabilityCacheRedis() = nil")
	}
}

// ── GetSlots ──────────────────────────────────────────────────────

func TestGetSlots_CacheMiss_RetornaNilNil(t *testing.T) {
	r := newMockRedis()
	// store vacío → Get devuelve error (miss)
	c := newCache(r)
	profID, clinicID := testIDs()

	slots, err := c.GetSlots(context.Background(), profID, clinicID,
		time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "D0150")
	if err != nil {
		t.Errorf("GetSlots() error = %v, quería nil", err)
	}
	if slots != nil {
		t.Errorf("GetSlots() = %v, quería nil (cache miss)", slots)
	}
}

func TestGetSlots_CacheHit_RetornaSlots(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()
	date := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	code := "D0150"

	expected := []aggregate.FreeSlot{freeSlot(profID, clinicID)}
	data, _ := json.Marshal(expected)
	r.store[slotsKey(profID, clinicID, date, code)] = string(data)

	got, err := c.GetSlots(context.Background(), profID, clinicID, date, code)
	if err != nil {
		t.Fatalf("GetSlots() error = %v", err)
	}
	if len(got) != 1 {
		t.Errorf("GetSlots() len = %d, quería 1", len(got))
	}
}

func TestGetSlots_JSONCorrupto_TratadoComoMiss(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()
	date := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	r.store[slotsKey(profID, clinicID, date, "D0150")] = "{no-es-json}"

	got, err := c.GetSlots(context.Background(), profID, clinicID, date, "D0150")
	if err != nil {
		t.Errorf("GetSlots() error = %v, quería nil para JSON corrupto", err)
	}
	if got != nil {
		t.Errorf("GetSlots() = %v, quería nil para JSON corrupto", got)
	}
}

func TestGetSlots_ErrorDeRedis_TratadoComoMiss(t *testing.T) {
	r := newMockRedis()
	r.getErr = errors.New("redis: connection refused")
	c := newCache(r)
	profID, clinicID := testIDs()

	got, err := c.GetSlots(context.Background(), profID, clinicID,
		time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "D0150")
	if err != nil {
		t.Errorf("GetSlots() error = %v, quería nil (error de redis tratado como miss)", err)
	}
	if got != nil {
		t.Errorf("GetSlots() = %v, quería nil", got)
	}
}

func TestGetSlots_UsaClaveCorrecta(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()
	date := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	code := "D0150"

	// Ponemos el valor en la clave exacta que esperamos.
	expected := []aggregate.FreeSlot{freeSlot(profID, clinicID)}
	data, _ := json.Marshal(expected)
	r.store[slotsKey(profID, clinicID, date, code)] = string(data)

	got, _ := c.GetSlots(context.Background(), profID, clinicID, date, code)
	if len(got) != 1 {
		t.Errorf("la clave usada por GetSlots no coincide con el formato esperado")
	}
}

// ── SetSlots ──────────────────────────────────────────────────────

func TestSetSlots_GuardaSlots(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()
	date := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	slots := []aggregate.FreeSlot{freeSlot(profID, clinicID)}

	if err := c.SetSlots(context.Background(), profID, clinicID, date, "D0150", slots); err != nil {
		t.Fatalf("SetSlots() error = %v", err)
	}

	expectedKey := slotsKey(profID, clinicID, date, "D0150")
	if r.lastSetKey != expectedKey {
		t.Errorf("Set key = %q, quería %q", r.lastSetKey, expectedKey)
	}
	if r.lastSetValue == "" {
		t.Error("Set value vacío")
	}
}

func TestSetSlots_UsaTTLDeCincoMinutos(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()

	_ = c.SetSlots(context.Background(), profID, clinicID,
		time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "D0150",
		[]aggregate.FreeSlot{freeSlot(profID, clinicID)})

	if r.lastSetTTL != 5*time.Minute {
		t.Errorf("TTL = %v, quería 5m", r.lastSetTTL)
	}
}

func TestSetSlots_SlicesSerializadosCorrectamente(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()
	date := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	original := []aggregate.FreeSlot{freeSlot(profID, clinicID)}

	_ = c.SetSlots(context.Background(), profID, clinicID, date, "D0150", original)

	var decoded []aggregate.FreeSlot
	if err := json.Unmarshal([]byte(r.lastSetValue), &decoded); err != nil {
		t.Fatalf("valor almacenado no es JSON válido: %v", err)
	}
	if len(decoded) != 1 {
		t.Errorf("decoded len = %d, quería 1", len(decoded))
	}
}

func TestSetSlots_SetFalla_PropagaError(t *testing.T) {
	r := newMockRedis()
	r.setErr = errors.New("redis: out of memory")
	c := newCache(r)
	profID, clinicID := testIDs()

	err := c.SetSlots(context.Background(), profID, clinicID,
		time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "D0150",
		[]aggregate.FreeSlot{freeSlot(profID, clinicID)})
	if err == nil {
		t.Fatal("SetSlots() debería propagar error de Set")
	}
}

func TestSetSlots_UsaClaveCorrecta(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()
	date := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	code := "D0150"

	_ = c.SetSlots(context.Background(), profID, clinicID, date, code,
		[]aggregate.FreeSlot{freeSlot(profID, clinicID)})

	expected := slotsKey(profID, clinicID, date, code)
	if r.lastSetKey != expected {
		t.Errorf("Set key = %q, quería %q", r.lastSetKey, expected)
	}
}

func TestSetSlots_ProcedureCodeVacio_GeneraClaveDistinta(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()
	date := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	_ = c.SetSlots(context.Background(), profID, clinicID, date, "", nil)
	keyEmpty := r.lastSetKey

	_ = c.SetSlots(context.Background(), profID, clinicID, date, "D0150", nil)
	keyFull := r.lastSetKey

	if keyEmpty == keyFull {
		t.Error("claves con procedure_code distinto no deben ser iguales")
	}
}

// ── InvalidateSchedule ────────────────────────────────────────────

func TestInvalidateSchedule_SinKeys_NoLlamaDel(t *testing.T) {
	r := newMockRedis()
	r.keysResult = []string{} // ninguna clave en cache
	c := newCache(r)
	profID, clinicID := testIDs()

	if err := c.InvalidateSchedule(context.Background(), profID, clinicID); err != nil {
		t.Fatalf("InvalidateSchedule() error = %v", err)
	}
	if len(r.delKeys) != 0 {
		t.Errorf("Del fue llamado con %v, no debería llamarse con lista vacía", r.delKeys)
	}
}

func TestInvalidateSchedule_ConKeys_LlamaDel(t *testing.T) {
	r := newMockRedis()
	profID, clinicID := testIDs()
	r.keysResult = []string{
		slotsKey(profID, clinicID, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "D0150"),
		slotsKey(profID, clinicID, time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC), "D0150"),
	}
	c := newCache(r)

	if err := c.InvalidateSchedule(context.Background(), profID, clinicID); err != nil {
		t.Fatalf("InvalidateSchedule() error = %v", err)
	}
	if len(r.delKeys) != 2 {
		t.Errorf("Del keys = %d, quería 2", len(r.delKeys))
	}
}

func TestInvalidateSchedule_UsaPatronCorrecto(t *testing.T) {
	r := newMockRedis()
	r.keysResult = nil
	c := newCache(r)
	profID, clinicID := testIDs()

	_ = c.InvalidateSchedule(context.Background(), profID, clinicID)

	expectedPattern := fmt.Sprintf("scheduling:availability:%s:%s:*",
		uuid.UUID(profID).String(),
		uuid.UUID(clinicID).String(),
	)
	if r.lastKeysPattern != expectedPattern {
		t.Errorf("Keys pattern = %q, quería %q", r.lastKeysPattern, expectedPattern)
	}
}

func TestInvalidateSchedule_KeysFalla_PropagaError(t *testing.T) {
	r := newMockRedis()
	r.keysErr = errors.New("redis: connection reset")
	c := newCache(r)
	profID, clinicID := testIDs()

	err := c.InvalidateSchedule(context.Background(), profID, clinicID)
	if err == nil {
		t.Fatal("InvalidateSchedule() debería propagar error de Keys")
	}
}

func TestInvalidateSchedule_DelFalla_PropagaError(t *testing.T) {
	r := newMockRedis()
	profID, clinicID := testIDs()
	r.keysResult = []string{slotsKey(profID, clinicID, time.Now(), "D0150")}
	r.delErr = errors.New("redis: write timeout")
	c := newCache(r)

	err := c.InvalidateSchedule(context.Background(), profID, clinicID)
	if err == nil {
		t.Fatal("InvalidateSchedule() debería propagar error de Del")
	}
}

// ── AcquireSlotLock ───────────────────────────────────────────────

func TestAcquireSlotLock_Adquirido_RetornaTrue(t *testing.T) {
	r := newMockRedis()
	r.setNXResult = true
	c := newCache(r)
	profID, clinicID := testIDs()
	slotStart := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)

	acquired, err := c.AcquireSlotLock(context.Background(), profID, clinicID, slotStart, 30*time.Second)
	if err != nil {
		t.Fatalf("AcquireSlotLock() error = %v", err)
	}
	if !acquired {
		t.Error("AcquireSlotLock() = false, quería true")
	}
}

func TestAcquireSlotLock_YaOcupado_RetornaFalse(t *testing.T) {
	r := newMockRedis()
	r.setNXResult = false // lock ya tomado por otro proceso
	c := newCache(r)
	profID, clinicID := testIDs()

	acquired, err := c.AcquireSlotLock(context.Background(), profID, clinicID,
		time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC), 30*time.Second)
	if err != nil {
		t.Fatalf("AcquireSlotLock() error = %v", err)
	}
	if acquired {
		t.Error("AcquireSlotLock() = true, quería false (ya ocupado)")
	}
}

func TestAcquireSlotLock_SetNXFalla_PropagaError(t *testing.T) {
	r := newMockRedis()
	r.setNXErr = errors.New("redis: timeout")
	c := newCache(r)
	profID, clinicID := testIDs()

	acquired, err := c.AcquireSlotLock(context.Background(), profID, clinicID,
		time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC), 30*time.Second)
	if err == nil {
		t.Fatal("AcquireSlotLock() debería propagar error de SetNX")
	}
	if acquired {
		t.Error("AcquireSlotLock() = true aunque SetNX falló")
	}
}

func TestAcquireSlotLock_UsaClaveCorrecta(t *testing.T) {
	r := newMockRedis()
	r.setNXResult = true
	c := newCache(r)
	profID, clinicID := testIDs()
	slotStart := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)

	_, _ = c.AcquireSlotLock(context.Background(), profID, clinicID, slotStart, 30*time.Second)

	expected := lockKey(profID, clinicID, slotStart)
	if r.lastSetNXKey != expected {
		t.Errorf("SetNX key = %q, quería %q", r.lastSetNXKey, expected)
	}
}

func TestAcquireSlotLock_UsaTTLProvisto(t *testing.T) {
	r := newMockRedis()
	r.setNXResult = true
	c := newCache(r)
	profID, clinicID := testIDs()
	ttl := 45 * time.Second

	_, _ = c.AcquireSlotLock(context.Background(), profID, clinicID,
		time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC), ttl)

	if r.lastSetNXTTL != ttl {
		t.Errorf("SetNX TTL = %v, quería %v", r.lastSetNXTTL, ttl)
	}
}

// ── ReleaseSlotLock ───────────────────────────────────────────────

func TestReleaseSlotLock_Exitoso_RetornaNil(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()

	err := c.ReleaseSlotLock(context.Background(), profID, clinicID,
		time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Errorf("ReleaseSlotLock() error = %v, quería nil", err)
	}
}

func TestReleaseSlotLock_GetDelFalla_RetornaNil(t *testing.T) {
	// Si el lock ya expiró (key no existe), GetDel falla — no es error.
	r := newMockRedis()
	r.getDelErr = errors.New("redis: nil (key not found)")
	c := newCache(r)
	profID, clinicID := testIDs()

	err := c.ReleaseSlotLock(context.Background(), profID, clinicID,
		time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Errorf("ReleaseSlotLock() error = %v, quería nil (GetDel error ignorado)", err)
	}
}

func TestReleaseSlotLock_UsaClaveCorrecta(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()
	slotStart := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)

	_ = c.ReleaseSlotLock(context.Background(), profID, clinicID, slotStart)

	expected := lockKey(profID, clinicID, slotStart)
	if r.lastGetDelKey != expected {
		t.Errorf("GetDel key = %q, quería %q", r.lastGetDelKey, expected)
	}
}

// ── GetSlots / SetSlots round-trip ────────────────────────────────

func TestSetSlots_GetSlots_RoundTrip(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()
	date := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	code := "D0150"

	original := []aggregate.FreeSlot{freeSlot(profID, clinicID)}
	if err := c.SetSlots(context.Background(), profID, clinicID, date, code, original); err != nil {
		t.Fatalf("SetSlots() error = %v", err)
	}

	got, err := c.GetSlots(context.Background(), profID, clinicID, date, code)
	if err != nil {
		t.Fatalf("GetSlots() error = %v", err)
	}
	if len(got) != len(original) {
		t.Errorf("round-trip len = %d, quería %d", len(got), len(original))
	}
	if got[0].DurationMins != original[0].DurationMins {
		t.Errorf("DurationMins = %d, quería %d", got[0].DurationMins, original[0].DurationMins)
	}
}

func TestSetSlots_InvalidateSchedule_BorraEntradas(t *testing.T) {
	r := newMockRedis()
	c := newCache(r)
	profID, clinicID := testIDs()
	date := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	// Cargar el cache
	_ = c.SetSlots(context.Background(), profID, clinicID, date, "D0150",
		[]aggregate.FreeSlot{freeSlot(profID, clinicID)})

	// Simular que Keys devuelve la clave que acabamos de escribir
	r.keysResult = []string{slotsKey(profID, clinicID, date, "D0150")}

	if err := c.InvalidateSchedule(context.Background(), profID, clinicID); err != nil {
		t.Fatalf("InvalidateSchedule() error = %v", err)
	}

	// Verificar que Del fue llamado con la clave correcta
	if len(r.delKeys) != 1 {
		t.Errorf("Del keys = %d, quería 1", len(r.delKeys))
	}
}
