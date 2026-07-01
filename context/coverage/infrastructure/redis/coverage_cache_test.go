package redis_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	coverageredis "github.com/juantevez/odontoagenda/context/coverage/infrastructure/redis"
)

// ── mock Redis client ─────────────────────────────────────────────

type mockRedis struct {
	getResult  string
	getErr     error
	setErr     error
	delErr     error
	keysResult []string
	keysErr    error
}

func (m *mockRedis) Get(_ context.Context, _ string) (string, error) {
	return m.getResult, m.getErr
}
func (m *mockRedis) Set(_ context.Context, _, _ string, _ time.Duration) error {
	return m.setErr
}
func (m *mockRedis) Del(_ context.Context, _ ...string) error {
	return m.delErr
}
func (m *mockRedis) Keys(_ context.Context, _ string) ([]string, error) {
	return m.keysResult, m.keysErr
}

// ── GetCoverageResult ─────────────────────────────────────────────

func TestGetCoverageResult_CacheMiss_GetError(t *testing.T) {
	c := coverageredis.NewCoverageCacheRedis(&mockRedis{getErr: errors.New("key not found")})

	result, err := c.GetCoverageResult(context.Background(), uuid.New(), "PROC001")
	if err != nil {
		t.Fatalf("expected nil error on cache miss, got %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result on cache miss")
	}
}

func TestGetCoverageResult_CacheMiss_CorruptedJSON(t *testing.T) {
	c := coverageredis.NewCoverageCacheRedis(&mockRedis{getResult: "not-valid-json"})

	result, err := c.GetCoverageResult(context.Background(), uuid.New(), "PROC001")
	if err != nil {
		t.Fatalf("expected nil error on corrupted data, got %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result on corrupted JSON")
	}
}

func TestGetCoverageResult_CacheHit(t *testing.T) {
	want := valueobject.CoverageResult{
		IsCovered:       true,
		CoveragePercent: 80,
		CoPayType:       valueobject.CoPayTypePercent,
		CoPayValue:      20,
	}
	b, _ := json.Marshal(want)
	c := coverageredis.NewCoverageCacheRedis(&mockRedis{getResult: string(b)})

	result, err := c.GetCoverageResult(context.Background(), uuid.New(), "PROC001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result on cache hit")
	}
	if result.CoveragePercent != want.CoveragePercent {
		t.Fatalf("CoveragePercent: got %d, want %d", result.CoveragePercent, want.CoveragePercent)
	}
	if result.CoPayValue != want.CoPayValue {
		t.Fatalf("CoPayValue: got %d, want %d", result.CoPayValue, want.CoPayValue)
	}
}

// ── SetCoverageResult ─────────────────────────────────────────────

func TestSetCoverageResult_Success(t *testing.T) {
	c := coverageredis.NewCoverageCacheRedis(&mockRedis{})

	err := c.SetCoverageResult(context.Background(), uuid.New(), "PROC001",
		valueobject.CoverageResult{IsCovered: true, CoveragePercent: 70})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetCoverageResult_SetError(t *testing.T) {
	c := coverageredis.NewCoverageCacheRedis(&mockRedis{setErr: errors.New("redis unavailable")})

	err := c.SetCoverageResult(context.Background(), uuid.New(), "PROC001",
		valueobject.CoverageResult{IsCovered: false})
	if err == nil {
		t.Fatal("expected error from Set")
	}
}

// ── InvalidatePlan ────────────────────────────────────────────────

func TestInvalidatePlan_KeysError(t *testing.T) {
	c := coverageredis.NewCoverageCacheRedis(&mockRedis{keysErr: errors.New("redis error")})

	if err := c.InvalidatePlan(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error from Keys")
	}
}

func TestInvalidatePlan_NoKeys(t *testing.T) {
	c := coverageredis.NewCoverageCacheRedis(&mockRedis{keysResult: []string{}})

	if err := c.InvalidatePlan(context.Background(), uuid.New()); err != nil {
		t.Fatalf("unexpected error when no keys found: %v", err)
	}
}

func TestInvalidatePlan_DelSuccess(t *testing.T) {
	planID := uuid.New()
	keys := []string{
		"coverage:result:" + planID.String() + ":PROC001",
		"coverage:result:" + planID.String() + ":PROC002",
	}
	c := coverageredis.NewCoverageCacheRedis(&mockRedis{keysResult: keys})

	if err := c.InvalidatePlan(context.Background(), planID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInvalidatePlan_DelError(t *testing.T) {
	planID := uuid.New()
	c := coverageredis.NewCoverageCacheRedis(&mockRedis{
		keysResult: []string{"coverage:result:" + planID.String() + ":PROC001"},
		delErr:     errors.New("del failed"),
	})

	if err := c.InvalidatePlan(context.Background(), planID); err == nil {
		t.Fatal("expected error from Del")
	}
}
