package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/repository"
	"github.com/juantevez/odontoagenda/context/coverage/domain/service"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── Mocks ─────────────────────────────────────────────────────────

type mockAgreementRepo struct {
	saveErr          error
	updateErr        error
	findByIDResult   *aggregate.Agreement
	findByIDErr      error
	findActiveResult sharedtypes.PagedResult[*aggregate.Agreement]
	findActiveErr    error
	findByProvResult sharedtypes.PagedResult[*aggregate.Agreement]
	findByProvErr    error
}

func (m *mockAgreementRepo) Save(_ context.Context, _ *aggregate.Agreement) error { return m.saveErr }
func (m *mockAgreementRepo) Update(_ context.Context, _ *aggregate.Agreement) error {
	return m.updateErr
}
func (m *mockAgreementRepo) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.Agreement, error) {
	return m.findByIDResult, m.findByIDErr
}
func (m *mockAgreementRepo) FindByCode(_ context.Context, _ string) (*aggregate.Agreement, error) {
	return m.findByIDResult, m.findByIDErr
}
func (m *mockAgreementRepo) FindActive(_ context.Context, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Agreement], error) {
	return m.findActiveResult, m.findActiveErr
}
func (m *mockAgreementRepo) FindByProviderType(_ context.Context, _ valueobject.ProviderType, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Agreement], error) {
	return m.findByProvResult, m.findByProvErr
}
func (m *mockAgreementRepo) ExistsByCode(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *mockAgreementRepo) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.Agreement, error) {
	return nil, nil
}

type mockAuthorizationRepo struct {
	findByIDResult              *aggregate.AuthorizationRequest
	findByIDErr                 error
	findPendingByAgreementResult []*aggregate.AuthorizationRequest
	findPendingByAgreementErr   error
}

func (m *mockAuthorizationRepo) Save(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return nil
}
func (m *mockAuthorizationRepo) Update(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return nil
}
func (m *mockAuthorizationRepo) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.AuthorizationRequest, error) {
	return m.findByIDResult, m.findByIDErr
}
func (m *mockAuthorizationRepo) FindPendingByAgreement(_ context.Context, _ uuid.UUID) ([]*aggregate.AuthorizationRequest, error) {
	return m.findPendingByAgreementResult, m.findPendingByAgreementErr
}
func (m *mockAuthorizationRepo) FindPendingByPatient(_ context.Context, _ sharedtypes.PatientID, _ string) (*aggregate.AuthorizationRequest, error) {
	return nil, nil
}
func (m *mockAuthorizationRepo) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.AuthorizationRequest, error) {
	return nil, nil
}

type mockAffiliationRepo struct {
	findActiveResult *repository.PatientAffiliation
	findActiveErr    error
}

func (m *mockAffiliationRepo) Upsert(_ context.Context, _ repository.PatientAffiliation) error {
	return nil
}
func (m *mockAffiliationRepo) FindActive(_ context.Context, _ sharedtypes.PatientID, _ uuid.UUID) (*repository.PatientAffiliation, error) {
	return m.findActiveResult, m.findActiveErr
}
func (m *mockAffiliationRepo) SuspendByPatient(_ context.Context, _ sharedtypes.PatientID) error {
	return nil
}

type mockCoverageCache struct {
	getResult *valueobject.CoverageResult
	getErr    error
	setCalled bool
}

func (m *mockCoverageCache) GetCoverageResult(_ context.Context, _ uuid.UUID, _ string) (*valueobject.CoverageResult, error) {
	return m.getResult, m.getErr
}
func (m *mockCoverageCache) SetCoverageResult(_ context.Context, _ uuid.UUID, _ string, _ valueobject.CoverageResult) error {
	m.setCalled = true
	return nil
}
func (m *mockCoverageCache) InvalidatePlan(_ context.Context, _ uuid.UUID) error { return nil }

// ── Helpers ───────────────────────────────────────────────────────

func activeAgreement(pt valueobject.ProviderType, plans ...aggregate.Plan) *aggregate.Agreement {
	email, _ := sharedvo.NewEmail("contact@test.com")
	phone, _ := sharedvo.NewPhoneNumber("+54911000001")
	return aggregate.Reconstitute(
		uuid.New(), "AGR001", "Proveedor Test",
		pt, valueobject.AgreementStatusActive,
		time.Now().AddDate(-1, 0, 0), nil,
		email, phone, 0,
		plans, time.Now(), time.Now(), nil, 1,
	)
}

func activeAgreementWithValidUntil() *aggregate.Agreement {
	email, _ := sharedvo.NewEmail("contact@test.com")
	phone, _ := sharedvo.NewPhoneNumber("+54911000001")
	validUntil := time.Now().AddDate(1, 0, 0)
	return aggregate.Reconstitute(
		uuid.New(), "AGR001", "Proveedor Test",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusActive,
		time.Now().AddDate(-1, 0, 0), &validUntil,
		email, phone, 5,
		[]aggregate.Plan{}, time.Now(), time.Now(), nil, 1,
	)
}

func suspendedAgreement() *aggregate.Agreement {
	email, _ := sharedvo.NewEmail("contact@test.com")
	phone, _ := sharedvo.NewPhoneNumber("+54911000001")
	return aggregate.Reconstitute(
		uuid.New(), "AGR001", "Proveedor Test",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusSuspended,
		time.Now().AddDate(-1, 0, 0), nil,
		email, phone, 0,
		[]aggregate.Plan{}, time.Now(), time.Now(), nil, 1,
	)
}

func buildCoveredAgreement() (a *aggregate.Agreement, planID uuid.UUID, patientID uuid.UUID) {
	planID = uuid.New()
	patientID = uuid.New()
	rule := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 80,
	}
	plan := aggregate.ReconstitutePlan(
		planID, "P001", "Plan Básico",
		valueobject.CoPayTypeNone, 0, false, nil,
		[]aggregate.ProcedureRule{rule},
		valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	a = activeAgreement(valueobject.ProviderTypeObraSocial, plan)
	return
}

func pendingAR() *aggregate.AuthorizationRequest {
	deadline := time.Now().Add(48 * time.Hour)
	ar, _ := aggregate.NewAuthorizationRequest(
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", nil, &deadline,
	)
	return ar
}

// ── CalculateCoverageHandler ──────────────────────────────────────

func TestCalculateCoverageHandler_CacheHit(t *testing.T) {
	cached := valueobject.CoverageResult{IsCovered: true, CoveragePercent: 80}
	cache := &mockCoverageCache{getResult: &cached}
	calc := service.NewCoverageCalculator(&mockAffiliationRepo{})
	h := NewCalculateCoverageHandler(&mockAgreementRepo{}, calc, cache)

	result, err := h.Handle(context.Background(), CalculateCoverageQuery{
		AgreementID: uuid.New(), PlanID: uuid.New(), ProcedureCode: "PROC001",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsCovered || result.CoveragePercent != 80 {
		t.Fatalf("expected cached result, got %+v", result)
	}
}

func TestCalculateCoverageHandler_FindByIDError(t *testing.T) {
	cache := &mockCoverageCache{}
	repo := &mockAgreementRepo{findByIDErr: errors.New("not found")}
	calc := service.NewCoverageCalculator(&mockAffiliationRepo{})
	h := NewCalculateCoverageHandler(repo, calc, cache)

	_, err := h.Handle(context.Background(), CalculateCoverageQuery{
		AgreementID: uuid.New(), PlanID: uuid.New(), ProcedureCode: "PROC001",
	})
	if err == nil {
		t.Fatal("expected error when FindByID fails")
	}
}

func TestCalculateCoverageHandler_NotCovered_AgreementNotValid(t *testing.T) {
	a := suspendedAgreement()
	cache := &mockCoverageCache{}
	repo := &mockAgreementRepo{findByIDResult: a}
	calc := service.NewCoverageCalculator(&mockAffiliationRepo{})
	h := NewCalculateCoverageHandler(repo, calc, cache)

	result, err := h.Handle(context.Background(), CalculateCoverageQuery{
		AgreementID:     a.ID(),
		PlanID:          uuid.New(),
		ProcedureCode:   "PROC001",
		AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("expected not covered for suspended agreement")
	}
	if cache.setCalled {
		t.Fatal("cache should not be set when not covered")
	}
}

func TestCalculateCoverageHandler_Privado_NoCache(t *testing.T) {
	a := activeAgreement(valueobject.ProviderTypePrivado)
	cache := &mockCoverageCache{}
	repo := &mockAgreementRepo{findByIDResult: a}
	calc := service.NewCoverageCalculator(&mockAffiliationRepo{})
	h := NewCalculateCoverageHandler(repo, calc, cache)

	result, err := h.Handle(context.Background(), CalculateCoverageQuery{
		AgreementID:     a.ID(),
		PlanID:          uuid.New(),
		ProcedureCode:   "PROC001",
		AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsCovered {
		t.Fatal("Privado should return IsCovered=true (100% bolsillo)")
	}
	if cache.setCalled {
		t.Fatal("cache should not be set for Privado agreements")
	}
}

func TestCalculateCoverageHandler_Covered_NonPrivado_CacheSet(t *testing.T) {
	a, planID, patientID := buildCoveredAgreement()
	affiliation := &repository.PatientAffiliation{
		ID:               uuid.New(),
		PatientID:        patientID,
		AgreementID:      a.ID(),
		PlanID:           planID,
		MembershipNumber: "M001",
		AffiliatedSince:  time.Now().AddDate(-1, 0, 0),
		Status:           valueobject.AffiliationStatusActive,
	}
	cache := &mockCoverageCache{}
	repo := &mockAgreementRepo{findByIDResult: a}
	affRepo := &mockAffiliationRepo{findActiveResult: affiliation}
	calc := service.NewCoverageCalculator(affRepo)
	h := NewCalculateCoverageHandler(repo, calc, cache)

	result, err := h.Handle(context.Background(), CalculateCoverageQuery{
		AgreementID:     a.ID(),
		PlanID:          planID,
		ProcedureCode:   "PROC001",
		PatientID:       patientID,
		PatientAge:      30,
		AppointmentDate: time.Now(),
		VisitsThisYear:  0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsCovered {
		t.Fatalf("expected covered, reason: %s", result.RejectionReason)
	}
	if result.CoveragePercent != 80 {
		t.Fatalf("expected 80%% coverage, got %d", result.CoveragePercent)
	}
	if !cache.setCalled {
		t.Fatal("cache should be set for covered non-Privado")
	}
}

// ── VerifyAffiliationHandler ──────────────────────────────────────

func TestVerifyAffiliationHandler_AgreementNotFound_ReturnsCancelled(t *testing.T) {
	repo := &mockAgreementRepo{findByIDErr: errors.New("not found")}
	verifier := service.NewAffiliationVerifier(repo, &mockAffiliationRepo{})
	h := NewVerifyAffiliationHandler(verifier)

	result, err := h.Handle(context.Background(), VerifyAffiliationQuery{
		AgreementID: uuid.New(), PlanID: uuid.New(),
		PatientID: uuid.New(), AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobject.AffiliationStatusCancelled {
		t.Fatalf("expected Cancelled, got %s", result.Status)
	}
}

func TestVerifyAffiliationHandler_AgreementNotValid_ReturnsSuspended(t *testing.T) {
	a := suspendedAgreement()
	repo := &mockAgreementRepo{findByIDResult: a}
	verifier := service.NewAffiliationVerifier(repo, &mockAffiliationRepo{})
	h := NewVerifyAffiliationHandler(verifier)

	result, err := h.Handle(context.Background(), VerifyAffiliationQuery{
		AgreementID: a.ID(), PlanID: uuid.New(),
		PatientID: uuid.New(), AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobject.AffiliationStatusSuspended {
		t.Fatalf("expected Suspended, got %s", result.Status)
	}
}

func TestVerifyAffiliationHandler_PlanNotFound_ReturnsCancelled(t *testing.T) {
	a := activeAgreement(valueobject.ProviderTypeObraSocial) // no plans
	repo := &mockAgreementRepo{findByIDResult: a}
	verifier := service.NewAffiliationVerifier(repo, &mockAffiliationRepo{})
	h := NewVerifyAffiliationHandler(verifier)

	result, err := h.Handle(context.Background(), VerifyAffiliationQuery{
		AgreementID: a.ID(), PlanID: uuid.New(), // non-existent plan
		PatientID: uuid.New(), AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobject.AffiliationStatusCancelled {
		t.Fatalf("expected Cancelled, got %s", result.Status)
	}
}

func TestVerifyAffiliationHandler_AffiliationNotFound_ReturnsCancelled(t *testing.T) {
	planID := uuid.New()
	plan := aggregate.ReconstitutePlan(
		planID, "P001", "Plan Básico",
		valueobject.CoPayTypeNone, 0, false, nil,
		[]aggregate.ProcedureRule{}, valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, plan)
	repo := &mockAgreementRepo{findByIDResult: a}
	affRepo := &mockAffiliationRepo{findActiveResult: nil}
	verifier := service.NewAffiliationVerifier(repo, affRepo)
	h := NewVerifyAffiliationHandler(verifier)

	result, err := h.Handle(context.Background(), VerifyAffiliationQuery{
		AgreementID: a.ID(), PlanID: planID,
		PatientID: uuid.New(), AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobject.AffiliationStatusCancelled {
		t.Fatalf("expected Cancelled, got %s", result.Status)
	}
}

func TestVerifyAffiliationHandler_Success_ReturnsActive(t *testing.T) {
	planID := uuid.New()
	patientID := uuid.New()
	plan := aggregate.ReconstitutePlan(
		planID, "P001", "Plan Básico",
		valueobject.CoPayTypeNone, 0, false, nil,
		[]aggregate.ProcedureRule{}, valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, plan)
	repo := &mockAgreementRepo{findByIDResult: a}
	affiliation := &repository.PatientAffiliation{
		ID:               uuid.New(),
		PatientID:        patientID,
		AgreementID:      a.ID(),
		PlanID:           planID,
		MembershipNumber: "M001",
		AffiliatedSince:  time.Now().AddDate(-1, 0, 0),
		Status:           valueobject.AffiliationStatusActive,
	}
	affRepo := &mockAffiliationRepo{findActiveResult: affiliation}
	verifier := service.NewAffiliationVerifier(repo, affRepo)
	h := NewVerifyAffiliationHandler(verifier)

	result, err := h.Handle(context.Background(), VerifyAffiliationQuery{
		AgreementID: a.ID(), PlanID: planID,
		PatientID: patientID, AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobject.AffiliationStatusActive {
		t.Fatalf("expected Active, got %s: %s", result.Status, result.Reason)
	}
}

// ── GetAgreementHandler ───────────────────────────────────────────

func TestGetAgreementHandler_FindByIDError(t *testing.T) {
	repo := &mockAgreementRepo{findByIDErr: errors.New("not found")}
	h := NewGetAgreementHandler(repo)
	_, err := h.Handle(context.Background(), GetAgreementQuery{AgreementID: uuid.New()})
	if err == nil {
		t.Fatal("expected error when FindByID fails")
	}
}

func TestGetAgreementHandler_Success_NoValidUntil_NoPlans(t *testing.T) {
	a := activeAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewGetAgreementHandler(repo)

	dto, err := h.Handle(context.Background(), GetAgreementQuery{AgreementID: a.ID()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto == nil {
		t.Fatal("expected non-nil DTO")
	}
	if dto.ID != a.ID().String() {
		t.Fatalf("expected ID %s, got %s", a.ID(), dto.ID)
	}
	if dto.ValidUntil != nil {
		t.Fatal("expected nil ValidUntil")
	}
	if len(dto.Plans) != 0 {
		t.Fatalf("expected empty plans, got %d", len(dto.Plans))
	}
}

func TestGetAgreementHandler_Success_WithValidUntil_WithPlan(t *testing.T) {
	planID := uuid.New()
	rule := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 80,
		MaxPerYear:      intPtr(10),
		AgeMin:          intPtr(18),
		AgeMax:          intPtr(65),
	}
	plan := aggregate.ReconstitutePlan(
		planID, "P001", "Plan Básico",
		valueobject.CoPayTypeNone, 0, false, intPtr(52),
		[]aggregate.ProcedureRule{rule},
		valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	a := activeAgreementWithValidUntil()
	// rebuild with plan
	email, _ := sharedvo.NewEmail("contact@test.com")
	phone, _ := sharedvo.NewPhoneNumber("+54911000001")
	validUntil := time.Now().AddDate(1, 0, 0)
	a2 := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Proveedor",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusActive,
		time.Now().AddDate(-1, 0, 0), &validUntil,
		email, phone, 5,
		[]aggregate.Plan{plan}, time.Now(), time.Now(), nil, 1,
	)
	repo := &mockAgreementRepo{findByIDResult: a2}
	h := NewGetAgreementHandler(repo)

	dto, err := h.Handle(context.Background(), GetAgreementQuery{AgreementID: a2.ID()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.ValidUntil == nil {
		t.Fatal("expected non-nil ValidUntil")
	}
	if len(dto.Plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(dto.Plans))
	}
	if dto.Plans[0].MaxAnnualVisits == nil || *dto.Plans[0].MaxAnnualVisits != 52 {
		t.Fatal("expected MaxAnnualVisits=52")
	}
	if len(dto.Plans[0].CoveredProcedures) != 1 {
		t.Fatalf("expected 1 procedure, got %d", len(dto.Plans[0].CoveredProcedures))
	}
	pr := dto.Plans[0].CoveredProcedures[0]
	if pr.ProcedureCode != "PROC001" || pr.CoveragePercent != 80 {
		t.Fatalf("unexpected procedure rule: %+v", pr)
	}
	if pr.MaxPerYear == nil || *pr.MaxPerYear != 10 {
		t.Fatal("expected MaxPerYear=10")
	}
	if pr.AgeMin == nil || *pr.AgeMin != 18 {
		t.Fatal("expected AgeMin=18")
	}
	if pr.AgeMax == nil || *pr.AgeMax != 65 {
		t.Fatal("expected AgeMax=65")
	}
	// use a for zero value check
	_ = a
}

// ── ListAgreementsHandler ─────────────────────────────────────────

func TestListAgreementsHandler_InvalidProviderType(t *testing.T) {
	h := NewListAgreementsHandler(&mockAgreementRepo{})
	_, err := h.Handle(context.Background(), ListAgreementsQuery{ProviderType: "INVALID"})
	if err == nil {
		t.Fatal("expected error for invalid provider type")
	}
}

func TestListAgreementsHandler_FindByProviderTypeError(t *testing.T) {
	repo := &mockAgreementRepo{findByProvErr: errors.New("db error")}
	h := NewListAgreementsHandler(repo)
	_, err := h.Handle(context.Background(), ListAgreementsQuery{
		ProviderType: "ObraSocial",
		Page:         sharedtypes.NewPage(10, 0),
	})
	if err == nil {
		t.Fatal("expected error from FindByProviderType")
	}
}

func TestListAgreementsHandler_FindActiveError(t *testing.T) {
	repo := &mockAgreementRepo{findActiveErr: errors.New("db error")}
	h := NewListAgreementsHandler(repo)
	_, err := h.Handle(context.Background(), ListAgreementsQuery{Page: sharedtypes.NewPage(10, 0)})
	if err == nil {
		t.Fatal("expected error from FindActive")
	}
}

func TestListAgreementsHandler_SuccessWithProviderType(t *testing.T) {
	a := activeAgreement(valueobject.ProviderTypeObraSocial)
	pagedResult := sharedtypes.NewPagedResult([]*aggregate.Agreement{a}, 1, sharedtypes.NewPage(10, 0))
	repo := &mockAgreementRepo{findByProvResult: pagedResult}
	h := NewListAgreementsHandler(repo)

	result, err := h.Handle(context.Background(), ListAgreementsQuery{
		ProviderType: "ObraSocial",
		Page:         sharedtypes.NewPage(10, 0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got total=%d items=%d", result.Total, len(result.Items))
	}
}

func TestListAgreementsHandler_SuccessWithoutProviderType(t *testing.T) {
	a := activeAgreement(valueobject.ProviderTypeObraSocial)
	pagedResult := sharedtypes.NewPagedResult([]*aggregate.Agreement{a}, 1, sharedtypes.NewPage(10, 0))
	repo := &mockAgreementRepo{findActiveResult: pagedResult}
	h := NewListAgreementsHandler(repo)

	result, err := h.Handle(context.Background(), ListAgreementsQuery{Page: sharedtypes.NewPage(10, 0)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got total=%d items=%d", result.Total, len(result.Items))
	}
}

// ── GetAuthorizationHandler ───────────────────────────────────────

func TestGetAuthorizationHandler_FindByIDError(t *testing.T) {
	repo := &mockAuthorizationRepo{findByIDErr: errors.New("not found")}
	h := NewGetAuthorizationHandler(repo)
	_, err := h.Handle(context.Background(), GetAuthorizationQuery{AuthorizationID: uuid.New()})
	if err == nil {
		t.Fatal("expected error when FindByID fails")
	}
}

func TestGetAuthorizationHandler_Success_Pending(t *testing.T) {
	ar := pendingAR()
	repo := &mockAuthorizationRepo{findByIDResult: ar}
	h := NewGetAuthorizationHandler(repo)

	dto, err := h.Handle(context.Background(), GetAuthorizationQuery{AuthorizationID: ar.ID()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto == nil {
		t.Fatal("expected non-nil DTO")
	}
	if dto.ID != ar.ID().String() {
		t.Fatalf("expected ID %s, got %s", ar.ID(), dto.ID)
	}
	if dto.Status != "Pending" {
		t.Fatalf("expected Pending, got %s", dto.Status)
	}
	if dto.AppointmentID != nil {
		t.Fatal("expected nil AppointmentID for this AR")
	}
	if dto.ResolvedAt != nil {
		t.Fatal("expected nil ResolvedAt for pending AR")
	}
	if dto.ExpiresAt == nil {
		t.Fatal("expected non-nil ExpiresAt")
	}
}

func TestGetAuthorizationHandler_Success_Approved_AllFieldsSet(t *testing.T) {
	ar := pendingAR()
	_ = ar.Approve("AUTH-CODE-001", uuid.New())

	repo := &mockAuthorizationRepo{findByIDResult: ar}
	h := NewGetAuthorizationHandler(repo)

	dto, err := h.Handle(context.Background(), GetAuthorizationQuery{AuthorizationID: ar.ID()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Status != "Approved" {
		t.Fatalf("expected Approved, got %s", dto.Status)
	}
	if dto.AuthorizationCode == nil || *dto.AuthorizationCode != "AUTH-CODE-001" {
		t.Fatalf("expected AuthorizationCode=AUTH-CODE-001, got %v", dto.AuthorizationCode)
	}
	if dto.ResolvedAt == nil {
		t.Fatal("expected non-nil ResolvedAt after approval")
	}
}

func TestGetAuthorizationHandler_Success_WithAppointmentID(t *testing.T) {
	ar := pendingAR()
	apptID := uuid.New()
	ar.AssignAppointment(apptID)

	repo := &mockAuthorizationRepo{findByIDResult: ar}
	h := NewGetAuthorizationHandler(repo)

	dto, err := h.Handle(context.Background(), GetAuthorizationQuery{AuthorizationID: ar.ID()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.AppointmentID == nil {
		t.Fatal("expected non-nil AppointmentID in DTO")
	}
	if *dto.AppointmentID != apptID.String() {
		t.Fatalf("expected AppointmentID=%s, got %s", apptID, *dto.AppointmentID)
	}
}

// ── ListPendingAuthorizationsHandler ─────────────────────────────

func TestListPendingAuthorizationsHandler_FindError(t *testing.T) {
	repo := &mockAuthorizationRepo{findPendingByAgreementErr: errors.New("db error")}
	h := NewListPendingAuthorizationsHandler(repo)
	_, err := h.Handle(context.Background(), ListPendingAuthorizationsQuery{AgreementID: uuid.New()})
	if err == nil {
		t.Fatal("expected error from FindPendingByAgreement")
	}
}

func TestListPendingAuthorizationsHandler_Success(t *testing.T) {
	ar1, ar2 := pendingAR(), pendingAR()
	repo := &mockAuthorizationRepo{
		findPendingByAgreementResult: []*aggregate.AuthorizationRequest{ar1, ar2},
	}
	h := NewListPendingAuthorizationsHandler(repo)

	dtos, err := h.Handle(context.Background(), ListPendingAuthorizationsQuery{AgreementID: uuid.New()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dtos) != 2 {
		t.Fatalf("expected 2 DTOs, got %d", len(dtos))
	}
}

func TestListPendingAuthorizationsHandler_EmptyResult(t *testing.T) {
	repo := &mockAuthorizationRepo{findPendingByAgreementResult: nil}
	h := NewListPendingAuthorizationsHandler(repo)

	dtos, err := h.Handle(context.Background(), ListPendingAuthorizationsQuery{AgreementID: uuid.New()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dtos) != 0 {
		t.Fatalf("expected empty list, got %d", len(dtos))
	}
}

// ── helpers ───────────────────────────────────────────────────────

func intPtr(n int) *int { return &n }
