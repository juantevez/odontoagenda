package nats_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/repository"
	coveragenats "github.com/juantevez/odontoagenda/context/coverage/infrastructure/nats"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── mocks ─────────────────────────────────────────────────────────

// mockSubBus captures the handlers passed to Subscribe and allows per-call errors.
type mockSubBus struct {
	subscribeErrs []error // per-call Subscribe error (index = call number)
	callIdx       int
	handlers      []pkgevents.Handler
}

func (m *mockSubBus) Subscribe(_ context.Context, _ pkgevents.SubscribeOptions, h pkgevents.Handler) error {
	defer func() { m.callIdx++ }()
	if m.callIdx < len(m.subscribeErrs) && m.subscribeErrs[m.callIdx] != nil {
		return m.subscribeErrs[m.callIdx]
	}
	m.handlers = append(m.handlers, h)
	return nil
}
func (m *mockSubBus) Publish(_ context.Context, _ pkgevents.DomainEvent) error { return nil }
func (m *mockSubBus) Close() error                                               { return nil }

type mockAffRepo struct {
	upsertErr       error
	suspendErr      error
}

func (m *mockAffRepo) Upsert(_ context.Context, _ repository.PatientAffiliation) error {
	return m.upsertErr
}
func (m *mockAffRepo) FindActive(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*repository.PatientAffiliation, error) {
	return nil, nil
}
func (m *mockAffRepo) SuspendByPatient(_ context.Context, _ uuid.UUID) error {
	return m.suspendErr
}

// Minimal auth repo — unused by the subscriber in the current implementation.
type mockAuthRepo struct{}

func (m *mockAuthRepo) Save(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return nil
}
func (m *mockAuthRepo) Update(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return nil
}
func (m *mockAuthRepo) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.AuthorizationRequest, error) {
	return nil, nil
}
func (m *mockAuthRepo) FindPendingByAgreement(_ context.Context, _ uuid.UUID) ([]*aggregate.AuthorizationRequest, error) {
	return nil, nil
}
func (m *mockAuthRepo) FindPendingByPatient(_ context.Context, _ uuid.UUID, _ string) (*aggregate.AuthorizationRequest, error) {
	return nil, nil
}
func (m *mockAuthRepo) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.AuthorizationRequest, error) {
	return nil, nil
}

// ── payload mirrors (unexported in production code) ───────────────

type coverageUpdatedPayload struct {
	PatientID        string    `json:"patient_id"`
	CoverageType     string    `json:"coverage_type"`
	AgreementID      *string   `json:"agreement_id,omitempty"`
	Action           string    `json:"action"`
	OccurredAt       time.Time `json:"occurred_at"`
	PlanID           *string   `json:"plan_id,omitempty"`
	MembershipNumber string    `json:"membership_number,omitempty"`
	ValidFrom        *string   `json:"valid_from,omitempty"`
}

type archivedPayload struct {
	PatientID  string    `json:"patient_id"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}

// ── helpers ───────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }

func makeEnvelope(payload any) pkgevents.Envelope {
	b, _ := json.Marshal(payload)
	return pkgevents.Envelope{
		EventID: uuid.New().String(),
		Payload: b,
	}
}

// newRegisteredSub creates a subscriber with RegisterAll already called.
// Returns the subscriber, the bus (so callers can inspect captured handlers),
// the affiliation repo mock, and both captured handlers:
//
//	[0] = patient.coverage.updated handler
//	[1] = patient.archived handler
func newRegisteredSub(t *testing.T, affRepo *mockAffRepo) ([]pkgevents.Handler, *mockSubBus) {
	t.Helper()
	bus := &mockSubBus{}
	sub := coveragenats.NewCoverageEventSubscriber(bus, affRepo, &mockAuthRepo{})
	if err := sub.RegisterAll(context.Background()); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	if len(bus.handlers) != 2 {
		t.Fatalf("expected 2 captured handlers, got %d", len(bus.handlers))
	}
	return bus.handlers, bus
}

// ── RegisterAll ───────────────────────────────────────────────────

func TestRegisterAll_FirstSubscribeFails(t *testing.T) {
	bus := &mockSubBus{subscribeErrs: []error{errors.New("nats error"), nil}}
	sub := coveragenats.NewCoverageEventSubscriber(bus, &mockAffRepo{}, &mockAuthRepo{})
	if err := sub.RegisterAll(context.Background()); err == nil {
		t.Fatal("expected error when first Subscribe fails")
	}
}

func TestRegisterAll_SecondSubscribeFails(t *testing.T) {
	bus := &mockSubBus{subscribeErrs: []error{nil, errors.New("nats error")}}
	sub := coveragenats.NewCoverageEventSubscriber(bus, &mockAffRepo{}, &mockAuthRepo{})
	if err := sub.RegisterAll(context.Background()); err == nil {
		t.Fatal("expected error when second Subscribe fails")
	}
}

func TestRegisterAll_Success(t *testing.T) {
	bus := &mockSubBus{}
	sub := coveragenats.NewCoverageEventSubscriber(bus, &mockAffRepo{}, &mockAuthRepo{})
	if err := sub.RegisterAll(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bus.handlers) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(bus.handlers))
	}
}

// ── handlePatientCoverageUpdated ──────────────────────────────────

func TestCoverageUpdated_InvalidPayload(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := pkgevents.Envelope{Payload: json.RawMessage(`not-valid-json`)}
	err := handlers[0](context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Fatalf("expected ErrSkipRetry, got %v", err)
	}
}

func TestCoverageUpdated_InvalidPatientID(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(coverageUpdatedPayload{PatientID: "not-a-uuid", Action: "added"})
	err := handlers[0](context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Fatalf("expected ErrSkipRetry, got %v", err)
	}
}

func TestCoverageUpdated_ActionAdded_MissingAgreementOrPlanID(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	// Both AgreementID and PlanID are nil → skips sync, returns nil
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID: uuid.New().String(),
		Action:    "added",
		// AgreementID and PlanID intentionally omitted
	})
	err := handlers[0](context.Background(), env)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCoverageUpdated_ActionAdded_OnlyAgreementIDMissing(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID: uuid.New().String(),
		Action:    "added",
		PlanID:    strPtr(uuid.New().String()),
		// AgreementID nil → skips
	})
	err := handlers[0](context.Background(), env)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCoverageUpdated_ActionAdded_InvalidAgreementID(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID:   uuid.New().String(),
		Action:      "added",
		AgreementID: strPtr("bad-uuid"),
		PlanID:      strPtr(uuid.New().String()),
	})
	err := handlers[0](context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Fatalf("expected ErrSkipRetry, got %v", err)
	}
}

func TestCoverageUpdated_ActionAdded_InvalidPlanID(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID:   uuid.New().String(),
		Action:      "added",
		AgreementID: strPtr(uuid.New().String()),
		PlanID:      strPtr("bad-plan-id"),
	})
	err := handlers[0](context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Fatalf("expected ErrSkipRetry, got %v", err)
	}
}

func TestCoverageUpdated_ActionAdded_UpsertError(t *testing.T) {
	affRepo := &mockAffRepo{upsertErr: errors.New("db error")}
	handlers, _ := newRegisteredSub(t, affRepo)
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID:        uuid.New().String(),
		Action:           "added",
		AgreementID:      strPtr(uuid.New().String()),
		PlanID:           strPtr(uuid.New().String()),
		MembershipNumber: "MEM001",
	})
	err := handlers[0](context.Background(), env)
	if err == nil {
		t.Fatal("expected error from Upsert")
	}
}

func TestCoverageUpdated_ActionAdded_NoValidFrom(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID:        uuid.New().String(),
		Action:           "added",
		AgreementID:      strPtr(uuid.New().String()),
		PlanID:           strPtr(uuid.New().String()),
		MembershipNumber: "MEM001",
		// ValidFrom nil → uses time.Now()
	})
	if err := handlers[0](context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCoverageUpdated_ActionAdded_ValidValidFrom(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID:        uuid.New().String(),
		Action:           "added",
		AgreementID:      strPtr(uuid.New().String()),
		PlanID:           strPtr(uuid.New().String()),
		MembershipNumber: "MEM001",
		ValidFrom:        strPtr("2024-01-15"),
	})
	if err := handlers[0](context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCoverageUpdated_ActionAdded_InvalidValidFrom_FallsBackToNow(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID:        uuid.New().String(),
		Action:           "added",
		AgreementID:      strPtr(uuid.New().String()),
		PlanID:           strPtr(uuid.New().String()),
		MembershipNumber: "MEM001",
		ValidFrom:        strPtr("not-a-date"), // silently ignored → uses time.Now()
	})
	if err := handlers[0](context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCoverageUpdated_ActionSuspended(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID: uuid.New().String(),
		Action:    "suspended",
	})
	if err := handlers[0](context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCoverageUpdated_ActionExpired(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID: uuid.New().String(),
		Action:    "expired",
	})
	if err := handlers[0](context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCoverageUpdated_ActionSuspended_SuspendError(t *testing.T) {
	affRepo := &mockAffRepo{suspendErr: errors.New("suspend error")}
	handlers, _ := newRegisteredSub(t, affRepo)
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID: uuid.New().String(),
		Action:    "suspended",
	})
	if err := handlers[0](context.Background(), env); err == nil {
		t.Fatal("expected error from SuspendByPatient")
	}
}

func TestCoverageUpdated_UnknownAction(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(coverageUpdatedPayload{
		PatientID: uuid.New().String(),
		Action:    "unknown_action",
	})
	err := handlers[0](context.Background(), env)
	if err != nil {
		t.Fatalf("unknown action should return nil, got %v", err)
	}
}

// ── handlePatientArchived ─────────────────────────────────────────

func TestPatientArchived_InvalidPayload(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := pkgevents.Envelope{Payload: json.RawMessage(`bad-json`)}
	err := handlers[1](context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Fatalf("expected ErrSkipRetry, got %v", err)
	}
}

func TestPatientArchived_InvalidPatientID(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(archivedPayload{PatientID: "not-a-uuid", Reason: "fraud"})
	err := handlers[1](context.Background(), env)
	if !errors.Is(err, pkgevents.ErrSkipRetry) {
		t.Fatalf("expected ErrSkipRetry, got %v", err)
	}
}

func TestPatientArchived_SuspendError(t *testing.T) {
	affRepo := &mockAffRepo{suspendErr: errors.New("db error")}
	handlers, _ := newRegisteredSub(t, affRepo)
	env := makeEnvelope(archivedPayload{PatientID: uuid.New().String(), Reason: "inactivity"})
	if err := handlers[1](context.Background(), env); err == nil {
		t.Fatal("expected error from SuspendByPatient")
	}
}

func TestPatientArchived_Success(t *testing.T) {
	handlers, _ := newRegisteredSub(t, &mockAffRepo{})
	env := makeEnvelope(archivedPayload{
		PatientID:  uuid.New().String(),
		Reason:     "patient request",
		OccurredAt: time.Now(),
	})
	if err := handlers[1](context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── PatientID type alias sanity check ────────────────────────────

// Ensures uuid.UUID satisfies sharedtypes.PatientID (type alias) at compile time.
var _ sharedtypes.PatientID = uuid.UUID{}
