package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/service"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
)

// ── Mock ──────────────────────────────────────────────────────────

type mockAuthRepo struct {
	saveErr                    error
	updateErrors               []error
	updateCallIdx              int
	findByIDResult             *aggregate.AuthorizationRequest
	findByIDErr                error
	findPendingByPatientResult *aggregate.AuthorizationRequest
	findPendingByPatientErr    error
	findExpiredResult          []*aggregate.AuthorizationRequest
	findExpiredErr             error
}

func (m *mockAuthRepo) Save(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return m.saveErr
}
func (m *mockAuthRepo) Update(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	if m.updateCallIdx < len(m.updateErrors) {
		err := m.updateErrors[m.updateCallIdx]
		m.updateCallIdx++
		return err
	}
	return nil
}
func (m *mockAuthRepo) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.AuthorizationRequest, error) {
	return m.findByIDResult, m.findByIDErr
}
func (m *mockAuthRepo) FindPendingByAgreement(_ context.Context, _ uuid.UUID) ([]*aggregate.AuthorizationRequest, error) {
	return nil, nil
}
func (m *mockAuthRepo) FindPendingByPatient(_ context.Context, _ uuid.UUID, _ string) (*aggregate.AuthorizationRequest, error) {
	return m.findPendingByPatientResult, m.findPendingByPatientErr
}
func (m *mockAuthRepo) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.AuthorizationRequest, error) {
	return m.findExpiredResult, m.findExpiredErr
}

// ── helpers ───────────────────────────────────────────────────────

func pendingAR() *aggregate.AuthorizationRequest {
	deadline := time.Now().Add(48 * time.Hour)
	ar, _ := aggregate.NewAuthorizationRequest(
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", nil, &deadline,
	)
	ar.PendingEvents()
	return ar
}

func approvedAR() *aggregate.AuthorizationRequest {
	code := "AUTH-CODE"
	return aggregate.ReconstituteAuthorization(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", nil, time.Now(),
		valueobject.AuthorizationStatusApproved,
		&code, nil, nil, nil, nil, 2,
	)
}

// ── RequestAuthorization ──────────────────────────────────────────

func TestRequestAuthorization_DuplicatePending(t *testing.T) {
	existing := pendingAR()
	repo := &mockAuthRepo{findPendingByPatientResult: existing}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.RequestAuthorization(
		context.Background(),
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", nil,
	)
	if err == nil {
		t.Fatal("expected error: duplicate pending authorization")
	}
}

func TestRequestAuthorization_FindPendingByPatientError_ContinuesAsNoDuplicate(t *testing.T) {
	// FindPendingByPatient returning an error means "treat as no duplicate"
	repo := &mockAuthRepo{
		findPendingByPatientErr: errors.New("repo error"),
	}
	svc := service.NewAuthorizationService(repo)

	id, err := svc.RequestAuthorization(
		context.Background(),
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == nil {
		t.Fatal("expected valid authorization request")
	}
}

func TestRequestAuthorization_EmptyMembershipNumber(t *testing.T) {
	repo := &mockAuthRepo{}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.RequestAuthorization(
		context.Background(),
		uuid.New(), uuid.New(), uuid.New(),
		"", "PROC001", nil,
	)
	if err == nil {
		t.Fatal("expected error for empty membership_number")
	}
}

func TestRequestAuthorization_SaveFails(t *testing.T) {
	repo := &mockAuthRepo{saveErr: errors.New("save error")}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.RequestAuthorization(
		context.Background(),
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", nil,
	)
	if err == nil {
		t.Fatal("expected error when Save fails")
	}
}

func TestRequestAuthorization_Success(t *testing.T) {
	apptID := uuid.New()
	repo := &mockAuthRepo{}
	svc := service.NewAuthorizationService(repo)

	ar, err := svc.RequestAuthorization(
		context.Background(),
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", &apptID,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ar.ID() == uuid.Nil {
		t.Fatal("expected valid ID")
	}
	if ar.Status() != valueobject.AuthorizationStatusPending {
		t.Fatalf("expected Pending, got %s", ar.Status())
	}
	if ar.AppointmentID() == nil || *ar.AppointmentID() != apptID {
		t.Fatal("AppointmentID mismatch")
	}
	if len(ar.PendingEvents()) != 1 {
		t.Fatal("expected 1 pending event (authorization.requested)")
	}
}

// ── ResolveAuthorization ──────────────────────────────────────────

func TestResolveAuthorization_FindByIDError(t *testing.T) {
	repo := &mockAuthRepo{findByIDErr: errors.New("not found")}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.ResolveAuthorization(
		context.Background(),
		uuid.New(), valueobject.AuthorizationStatusApproved,
		"CODE", "", uuid.New(),
	)
	if err == nil {
		t.Fatal("expected error when FindByID fails")
	}
}

func TestResolveAuthorization_Approved_EmptyAuthCode(t *testing.T) {
	repo := &mockAuthRepo{findByIDResult: pendingAR()}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.ResolveAuthorization(
		context.Background(),
		uuid.New(), valueobject.AuthorizationStatusApproved,
		"", "", uuid.New(), // empty authorizationCode
	)
	if err == nil {
		t.Fatal("expected error: authorization_code required for Approved")
	}
}

func TestResolveAuthorization_Approved_ApproveFails(t *testing.T) {
	// approvedAR is already terminal → ar.Approve() returns error
	repo := &mockAuthRepo{findByIDResult: approvedAR()}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.ResolveAuthorization(
		context.Background(),
		uuid.New(), valueobject.AuthorizationStatusApproved,
		"CODE", "", uuid.New(),
	)
	if err == nil {
		t.Fatal("expected error: cannot approve terminal AR")
	}
}

func TestResolveAuthorization_Approved_UpdateFails(t *testing.T) {
	repo := &mockAuthRepo{
		findByIDResult: pendingAR(),
		updateErrors:   []error{errors.New("update error")},
	}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.ResolveAuthorization(
		context.Background(),
		uuid.New(), valueobject.AuthorizationStatusApproved,
		"CODE", "", uuid.New(),
	)
	if err == nil {
		t.Fatal("expected error when Update fails")
	}
}

func TestResolveAuthorization_Approved_Success(t *testing.T) {
	ar := pendingAR()
	repo := &mockAuthRepo{findByIDResult: ar}
	svc := service.NewAuthorizationService(repo)

	resolved, err := svc.ResolveAuthorization(
		context.Background(),
		ar.ID(), valueobject.AuthorizationStatusApproved,
		"AUTH-001", "", uuid.New(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Status() != valueobject.AuthorizationStatusApproved {
		t.Fatalf("expected Approved, got %s", resolved.Status())
	}
	if resolved.AuthorizationCode() == nil || *resolved.AuthorizationCode() != "AUTH-001" {
		t.Fatal("AuthorizationCode mismatch")
	}
}

func TestResolveAuthorization_Rejected_EmptyReason(t *testing.T) {
	repo := &mockAuthRepo{findByIDResult: pendingAR()}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.ResolveAuthorization(
		context.Background(),
		uuid.New(), valueobject.AuthorizationStatusRejected,
		"", "", uuid.New(), // empty rejectionReason
	)
	if err == nil {
		t.Fatal("expected error: rejection_reason required for Rejected")
	}
}

func TestResolveAuthorization_Rejected_RejectFails(t *testing.T) {
	// approvedAR → ar.Reject() returns error (cannot reject approved)
	repo := &mockAuthRepo{findByIDResult: approvedAR()}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.ResolveAuthorization(
		context.Background(),
		uuid.New(), valueobject.AuthorizationStatusRejected,
		"", "no autorizado", uuid.New(),
	)
	if err == nil {
		t.Fatal("expected error: cannot reject approved AR")
	}
}

func TestResolveAuthorization_Rejected_Success(t *testing.T) {
	ar := pendingAR()
	repo := &mockAuthRepo{findByIDResult: ar}
	svc := service.NewAuthorizationService(repo)

	resolved, err := svc.ResolveAuthorization(
		context.Background(),
		ar.ID(), valueobject.AuthorizationStatusRejected,
		"", "Prestación no cubierta", uuid.New(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Status() != valueobject.AuthorizationStatusRejected {
		t.Fatalf("expected Rejected, got %s", resolved.Status())
	}
	if resolved.RejectionReason() == nil || *resolved.RejectionReason() != "Prestación no cubierta" {
		t.Fatal("RejectionReason mismatch")
	}
}

func TestResolveAuthorization_DefaultStatus(t *testing.T) {
	// Passing Pending (not Approved/Rejected) hits the default branch
	repo := &mockAuthRepo{findByIDResult: pendingAR()}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.ResolveAuthorization(
		context.Background(),
		uuid.New(), valueobject.AuthorizationStatusPending,
		"", "", uuid.New(),
	)
	if err == nil {
		t.Fatal("expected error for unresolvable status (default branch)")
	}
}

// ── ExpireStaleAuthorizations ─────────────────────────────────────

func TestExpireStaleAuthorizations_FindExpiredError(t *testing.T) {
	repo := &mockAuthRepo{findExpiredErr: errors.New("db error")}
	svc := service.NewAuthorizationService(repo)

	_, err := svc.ExpireStaleAuthorizations(context.Background())
	if err == nil {
		t.Fatal("expected error when FindExpired fails")
	}
}

func TestExpireStaleAuthorizations_EmptyList(t *testing.T) {
	repo := &mockAuthRepo{findExpiredResult: []*aggregate.AuthorizationRequest{}}
	svc := service.NewAuthorizationService(repo)

	count, err := svc.ExpireStaleAuthorizations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestExpireStaleAuthorizations_SkipsNonPendingAR(t *testing.T) {
	// pending → expires fine; approved → Expire() fails, skipped
	repo := &mockAuthRepo{
		findExpiredResult: []*aggregate.AuthorizationRequest{pendingAR(), approvedAR()},
	}
	svc := service.NewAuthorizationService(repo)

	count, err := svc.ExpireStaleAuthorizations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 expired (approved skipped), got %d", count)
	}
}

func TestExpireStaleAuthorizations_UpdateFails_Skipped(t *testing.T) {
	repo := &mockAuthRepo{
		findExpiredResult: []*aggregate.AuthorizationRequest{pendingAR()},
		updateErrors:      []error{errors.New("update error")},
	}
	svc := service.NewAuthorizationService(repo)

	count, err := svc.ExpireStaleAuthorizations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 (update failed, skipped), got %d", count)
	}
}

func TestExpireStaleAuthorizations_Success(t *testing.T) {
	ar1, ar2 := pendingAR(), pendingAR()
	repo := &mockAuthRepo{
		findExpiredResult: []*aggregate.AuthorizationRequest{ar1, ar2},
	}
	svc := service.NewAuthorizationService(repo)

	count, err := svc.ExpireStaleAuthorizations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 expired, got %d", count)
	}
}
