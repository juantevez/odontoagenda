package aggregate_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
)

// ── helpers ───────────────────────────────────────────────────────

func newPendingAR() *aggregate.AuthorizationRequest {
	deadline := time.Now().Add(48 * time.Hour)
	ar, _ := aggregate.NewAuthorizationRequest(
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", nil, &deadline,
	)
	ar.PendingEvents() // drain creation event
	return ar
}

// ── NewAuthorizationRequest ───────────────────────────────────────

func TestNewAuthorizationRequest_EmptyMembershipNumber(t *testing.T) {
	deadline := time.Now().Add(48 * time.Hour)
	_, err := aggregate.NewAuthorizationRequest(
		uuid.New(), uuid.New(), uuid.New(),
		"", "PROC001", nil, &deadline,
	)
	if err == nil {
		t.Fatal("expected error for empty membership_number")
	}
}

func TestNewAuthorizationRequest_EmptyProcedureCode(t *testing.T) {
	deadline := time.Now().Add(48 * time.Hour)
	_, err := aggregate.NewAuthorizationRequest(
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "", nil, &deadline,
	)
	if err == nil {
		t.Fatal("expected error for empty procedure_code")
	}
}

func TestNewAuthorizationRequest_Valid(t *testing.T) {
	apptID := uuid.New()
	deadline := time.Now().Add(48 * time.Hour)
	ar, err := aggregate.NewAuthorizationRequest(
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", &apptID, &deadline,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ar.Status() != valueobject.AuthorizationStatusPending {
		t.Fatalf("expected Pending, got %s", ar.Status())
	}
	if ar.MembershipNumber() != "MEM001" {
		t.Fatal("MembershipNumber mismatch")
	}
	if ar.ProcedureCode() != "PROC001" {
		t.Fatal("ProcedureCode mismatch")
	}
	if ar.AppointmentID() == nil || *ar.AppointmentID() != apptID {
		t.Fatal("AppointmentID mismatch")
	}
	if ar.ExpiresAt() == nil {
		t.Fatal("expected non-nil ExpiresAt")
	}
	if ar.AuthorizationCode() != nil {
		t.Fatal("AuthorizationCode should be nil for new AR")
	}
	if ar.RejectionReason() != nil {
		t.Fatal("RejectionReason should be nil for new AR")
	}
	if ar.ResolvedAt() != nil {
		t.Fatal("ResolvedAt should be nil for new AR")
	}
	evts := ar.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "authorization.requested" {
		t.Fatalf("expected authorization.requested event, got %v", evts)
	}
	if len(ar.PendingEvents()) != 0 {
		t.Fatal("PendingEvents should be cleared after first call")
	}
}

func TestNewAuthorizationRequest_NilExpiresAt(t *testing.T) {
	ar, err := aggregate.NewAuthorizationRequest(
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ar.ExpiresAt() != nil {
		t.Fatal("expected nil ExpiresAt")
	}
}

// ── AuthorizationRequest.Approve ─────────────────────────────────

func TestAR_Approve_AlreadyApproved(t *testing.T) {
	ar := newPendingAR()
	_ = ar.Approve("CODE1", uuid.New())
	ar.PendingEvents()
	if err := ar.Approve("CODE2", uuid.New()); err == nil {
		t.Fatal("expected error: already approved (terminal)")
	}
}

func TestAR_Approve_AlreadyRejected(t *testing.T) {
	ar := newPendingAR()
	_ = ar.Reject("reason", uuid.New())
	ar.PendingEvents()
	if err := ar.Approve("CODE", uuid.New()); err == nil {
		t.Fatal("expected error: already rejected (terminal)")
	}
}

func TestAR_Approve_AlreadyExpired(t *testing.T) {
	ar := newPendingAR()
	_ = ar.Expire()
	ar.PendingEvents()
	if err := ar.Approve("CODE", uuid.New()); err == nil {
		t.Fatal("expected error: already expired (terminal)")
	}
}

func TestAR_Approve_Success(t *testing.T) {
	ar := newPendingAR()
	resolvedBy := uuid.New()
	if err := ar.Approve("AUTH-001", resolvedBy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ar.Status() != valueobject.AuthorizationStatusApproved {
		t.Fatalf("expected Approved, got %s", ar.Status())
	}
	if ar.AuthorizationCode() == nil || *ar.AuthorizationCode() != "AUTH-001" {
		t.Fatalf("unexpected AuthorizationCode: %v", ar.AuthorizationCode())
	}
	if ar.ResolvedAt() == nil {
		t.Fatal("expected non-nil ResolvedAt")
	}
	if ar.ResolvedBy() == nil || *ar.ResolvedBy() != resolvedBy {
		t.Fatal("ResolvedBy mismatch")
	}
	evts := ar.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "authorization.resolved" {
		t.Fatalf("expected authorization.resolved event, got %v", evts)
	}
}

// ── AuthorizationRequest.Reject ───────────────────────────────────

func TestAR_Reject_AlreadyApproved(t *testing.T) {
	ar := newPendingAR()
	_ = ar.Approve("CODE", uuid.New())
	ar.PendingEvents()
	if err := ar.Reject("reason", uuid.New()); err == nil {
		t.Fatal("expected error: cannot reject approved (specific invariant)")
	}
}

func TestAR_Reject_AlreadyRejected(t *testing.T) {
	ar := newPendingAR()
	_ = ar.Reject("first reason", uuid.New())
	ar.PendingEvents()
	if err := ar.Reject("second reason", uuid.New()); err == nil {
		t.Fatal("expected error: already rejected (terminal)")
	}
}

func TestAR_Reject_AlreadyExpired(t *testing.T) {
	ar := newPendingAR()
	_ = ar.Expire()
	ar.PendingEvents()
	if err := ar.Reject("reason", uuid.New()); err == nil {
		t.Fatal("expected error: already expired (terminal)")
	}
}

func TestAR_Reject_Success(t *testing.T) {
	ar := newPendingAR()
	if err := ar.Reject("No autorizado por la prepaga", uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ar.Status() != valueobject.AuthorizationStatusRejected {
		t.Fatalf("expected Rejected, got %s", ar.Status())
	}
	if ar.RejectionReason() == nil || *ar.RejectionReason() != "No autorizado por la prepaga" {
		t.Fatalf("unexpected RejectionReason: %v", ar.RejectionReason())
	}
	if ar.ResolvedAt() == nil {
		t.Fatal("expected non-nil ResolvedAt")
	}
	evts := ar.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "authorization.resolved" {
		t.Fatalf("expected authorization.resolved event, got %v", evts)
	}
}

// ── AuthorizationRequest.Expire ───────────────────────────────────

func TestAR_Expire_NotPending(t *testing.T) {
	ar := newPendingAR()
	_ = ar.Approve("CODE", uuid.New())
	ar.PendingEvents()
	if err := ar.Expire(); err == nil {
		t.Fatal("expected error: can only expire Pending AR")
	}
}

func TestAR_Expire_Success(t *testing.T) {
	ar := newPendingAR()
	if err := ar.Expire(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ar.Status() != valueobject.AuthorizationStatusExpired {
		t.Fatalf("expected Expired, got %s", ar.Status())
	}
	if ar.ResolvedAt() == nil {
		t.Fatal("expected non-nil ResolvedAt after expire")
	}
	evts := ar.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "authorization.expired" {
		t.Fatalf("expected authorization.expired event, got %v", evts)
	}
}

// ── AuthorizationRequest.AssignAppointment ────────────────────────

func TestAR_AssignAppointment(t *testing.T) {
	ar := newPendingAR()
	v0 := ar.Version()
	apptID := uuid.New()
	ar.AssignAppointment(apptID)
	if ar.AppointmentID() == nil || *ar.AppointmentID() != apptID {
		t.Fatal("AppointmentID not assigned")
	}
	if ar.Version() != v0+1 {
		t.Fatalf("expected version bumped to %d, got %d", v0+1, ar.Version())
	}
}

// ── AuthorizationRequest.BumpVersion ─────────────────────────────

func TestAR_BumpVersion(t *testing.T) {
	ar := newPendingAR()
	v0 := ar.Version()
	ar.BumpVersion()
	if ar.Version() != v0+1 {
		t.Fatalf("expected version %d, got %d", v0+1, ar.Version())
	}
}

// ── AuthorizationRequest getters ──────────────────────────────────

func TestAR_Getters(t *testing.T) {
	agreementID := uuid.New()
	planID := uuid.New()
	patientID := uuid.New()
	apptID := uuid.New()
	deadline := time.Now().Add(48 * time.Hour)

	ar, _ := aggregate.NewAuthorizationRequest(
		agreementID, planID, patientID,
		"MEM123", "PROC999", &apptID, &deadline,
	)

	if ar.ID() == uuid.Nil {
		t.Fatal("ID must not be nil")
	}
	if ar.AgreementID() != agreementID {
		t.Fatal("AgreementID mismatch")
	}
	if ar.PlanID() != planID {
		t.Fatal("PlanID mismatch")
	}
	if ar.PatientID() != patientID {
		t.Fatal("PatientID mismatch")
	}
	if ar.MembershipNumber() != "MEM123" {
		t.Fatal("MembershipNumber mismatch")
	}
	if ar.ProcedureCode() != "PROC999" {
		t.Fatal("ProcedureCode mismatch")
	}
	if ar.AppointmentID() == nil || *ar.AppointmentID() != apptID {
		t.Fatal("AppointmentID mismatch")
	}
	if ar.RequestedAt().IsZero() {
		t.Fatal("RequestedAt must not be zero")
	}
	if ar.Version() != 1 {
		t.Fatal("expected initial version=1")
	}
}

// ── ReconstituteAuthorization ─────────────────────────────────────

func TestReconstituteAuthorization_NoPendingEvents(t *testing.T) {
	code := "AUTH-REC"
	reason := "rechazo"
	now := time.Now()
	resolvedBy := uuid.New()
	ar := aggregate.ReconstituteAuthorization(
		uuid.New(), uuid.New(), uuid.New(),
		uuid.New(), "MEM001", "PROC001",
		nil, now,
		valueobject.AuthorizationStatusApproved,
		&code, &now, &reason, &now, &resolvedBy,
		3,
	)
	if len(ar.PendingEvents()) != 0 {
		t.Fatal("ReconstituteAuthorization should not generate events")
	}
	if ar.Status() != valueobject.AuthorizationStatusApproved {
		t.Fatal("status mismatch")
	}
	if ar.Version() != 3 {
		t.Fatal("version mismatch")
	}
	if ar.AuthorizationCode() == nil || *ar.AuthorizationCode() != "AUTH-REC" {
		t.Fatal("AuthorizationCode mismatch")
	}
	if ar.RejectionReason() == nil || *ar.RejectionReason() != "rechazo" {
		t.Fatal("RejectionReason mismatch")
	}
	if ar.ResolvedAt() == nil {
		t.Fatal("ResolvedAt mismatch")
	}
	if ar.ResolvedBy() == nil || *ar.ResolvedBy() != resolvedBy {
		t.Fatal("ResolvedBy mismatch")
	}
}
