package service_test

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
	findByIDResult *aggregate.Agreement
	findByIDErr    error
}

func (m *mockAgreementRepo) Save(_ context.Context, _ *aggregate.Agreement) error    { return nil }
func (m *mockAgreementRepo) Update(_ context.Context, _ *aggregate.Agreement) error  { return nil }
func (m *mockAgreementRepo) FindByCode(_ context.Context, _ string) (*aggregate.Agreement, error) {
	return m.findByIDResult, m.findByIDErr
}
func (m *mockAgreementRepo) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.Agreement, error) {
	return m.findByIDResult, m.findByIDErr
}
func (m *mockAgreementRepo) FindActive(_ context.Context, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Agreement], error) {
	return sharedtypes.PagedResult[*aggregate.Agreement]{}, nil
}
func (m *mockAgreementRepo) FindByProviderType(_ context.Context, _ valueobject.ProviderType, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Agreement], error) {
	return sharedtypes.PagedResult[*aggregate.Agreement]{}, nil
}
func (m *mockAgreementRepo) ExistsByCode(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *mockAgreementRepo) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.Agreement, error) {
	return nil, nil
}

type mockAffiliationRepo struct {
	findActiveResult *repository.PatientAffiliation
	findActiveErr    error
}

func (m *mockAffiliationRepo) Upsert(_ context.Context, _ repository.PatientAffiliation) error {
	return nil
}
func (m *mockAffiliationRepo) FindActive(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*repository.PatientAffiliation, error) {
	return m.findActiveResult, m.findActiveErr
}
func (m *mockAffiliationRepo) SuspendByPatient(_ context.Context, _ uuid.UUID) error { return nil }

// ── builders ──────────────────────────────────────────────────────

func agreeEmail() sharedvo.Email {
	e, _ := sharedvo.NewEmail("provider@clinic.com")
	return e
}

func agreePhone() sharedvo.PhoneNumber {
	p, _ := sharedvo.NewPhoneNumber("+54911000001")
	return p
}

func activeAgreement(pt valueobject.ProviderType, plans []aggregate.Plan) *aggregate.Agreement {
	return aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		pt, valueobject.AgreementStatusActive,
		time.Now().AddDate(-1, 0, 0), nil,
		agreeEmail(), agreePhone(), 0,
		plans, time.Now(), time.Now(), nil, 1,
	)
}

func activePlanWithRule(rule aggregate.ProcedureRule, coPayType valueobject.CoPayType, coPayValue int, requiresPreAuth bool) (aggregate.Plan, uuid.UUID) {
	id := uuid.New()
	p := aggregate.ReconstitutePlan(
		id, "P001", "Plan Básico",
		coPayType, coPayValue, requiresPreAuth, nil,
		[]aggregate.ProcedureRule{rule},
		valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	return p, id
}

func activeAffiliation(patientID, planID uuid.UUID, affiliatedSince time.Time) *repository.PatientAffiliation {
	return &repository.PatientAffiliation{
		ID:               uuid.New(),
		PatientID:        patientID,
		PlanID:           planID,
		MembershipNumber: "MEM001",
		AffiliatedSince:  affiliatedSince,
		Status:           valueobject.AffiliationStatusActive,
	}
}

// baseInput builds a CalculateInput for a given agreement, planID and patientID.
func baseInput(a *aggregate.Agreement, planID, patientID uuid.UUID) service.CalculateInput {
	return service.CalculateInput{
		Agreement:       a,
		PlanID:          planID,
		ProcedureCode:   "PROC001",
		PatientID:       patientID,
		PatientAge:      30,
		AppointmentDate: time.Now(),
		VisitsThisYear:  0,
	}
}

// ── CoverageCalculator.Calculate ──────────────────────────────────

func TestCalculate_Privado_FastPath(t *testing.T) {
	a := activeAgreement(valueobject.ProviderTypePrivado, nil)
	calc := service.NewCoverageCalculator(&mockAffiliationRepo{})

	result, err := calc.Calculate(context.Background(), service.CalculateInput{
		Agreement: a, PlanID: uuid.New(), ProcedureCode: "PROC001",
		AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsCovered {
		t.Fatal("Privado should return IsCovered=true")
	}
	if result.CoPayValue != 100 || result.CoPayType != valueobject.CoPayTypePercent {
		t.Fatalf("Privado should return 100%% co-pay, got %+v", result)
	}
	if result.RequiresAuthorization {
		t.Fatal("Privado should not require authorization")
	}
}

func TestCalculate_AgreementNotValid_Suspended(t *testing.T) {
	a := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusSuspended,
		time.Now().AddDate(-1, 0, 0), nil,
		agreeEmail(), agreePhone(), 0,
		nil, time.Now(), time.Now(), nil, 1,
	)
	calc := service.NewCoverageCalculator(&mockAffiliationRepo{})

	result, err := calc.Calculate(context.Background(), service.CalculateInput{
		Agreement: a, PlanID: uuid.New(), AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("suspended agreement should not be covered")
	}
}

func TestCalculate_AgreementNotValid_ExpiredValidUntil(t *testing.T) {
	past := time.Now().AddDate(-1, 0, 0)
	a := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusActive,
		time.Now().AddDate(-2, 0, 0), &past, // validUntil = 1 year ago
		agreeEmail(), agreePhone(), 0,
		nil, time.Now(), time.Now(), nil, 1,
	)
	calc := service.NewCoverageCalculator(&mockAffiliationRepo{})

	result, err := calc.Calculate(context.Background(), service.CalculateInput{
		Agreement: a, PlanID: uuid.New(), AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("agreement past validUntil should not be covered")
	}
}

func TestCalculate_PlanNotFound(t *testing.T) {
	rule := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80}
	plan, _ := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	calc := service.NewCoverageCalculator(&mockAffiliationRepo{})

	result, err := calc.Calculate(context.Background(), service.CalculateInput{
		Agreement: a, PlanID: uuid.New(), // wrong planID
		ProcedureCode: "PROC001", AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("unknown plan should not be covered")
	}
}

func TestCalculate_PlanDiscontinued(t *testing.T) {
	planID := uuid.New()
	rule := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80}
	discontinuedPlan := aggregate.ReconstitutePlan(
		planID, "P001", "Plan",
		valueobject.CoPayTypeNone, 0, false, nil,
		[]aggregate.ProcedureRule{rule},
		valueobject.PlanStatusDiscontinued,
		time.Now(), time.Now(),
	)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{discontinuedPlan})
	calc := service.NewCoverageCalculator(&mockAffiliationRepo{})

	result, err := calc.Calculate(context.Background(), service.CalculateInput{
		Agreement: a, PlanID: planID, ProcedureCode: "PROC001",
		AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("discontinued plan should not be covered")
	}
}

func TestCalculate_ProcedureRuleNotFound(t *testing.T) {
	planID := uuid.New()
	plan := aggregate.ReconstitutePlan(
		planID, "P001", "Plan",
		valueobject.CoPayTypeNone, 0, false, nil,
		[]aggregate.ProcedureRule{}, // no procedures
		valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	calc := service.NewCoverageCalculator(&mockAffiliationRepo{})

	result, err := calc.Calculate(context.Background(), service.CalculateInput{
		Agreement: a, PlanID: planID, ProcedureCode: "PROC001",
		AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("missing procedure rule should not be covered")
	}
}

func TestCalculate_NoAffiliation_NilResult(t *testing.T) {
	rule := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80}
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	affRepo := &mockAffiliationRepo{findActiveResult: nil} // nil = no affiliation
	calc := service.NewCoverageCalculator(affRepo)

	result, err := calc.Calculate(context.Background(), baseInput(a, planID, uuid.New()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("no affiliation should not be covered")
	}
}

func TestCalculate_NoAffiliation_RepoError(t *testing.T) {
	rule := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80}
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	affRepo := &mockAffiliationRepo{findActiveErr: errors.New("db error")}
	calc := service.NewCoverageCalculator(affRepo)

	result, err := calc.Calculate(context.Background(), baseInput(a, planID, uuid.New()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("affiliation error should not be covered")
	}
}

func TestCalculate_WaitingPeriodActive(t *testing.T) {
	rule := aggregate.ProcedureRule{
		ProcedureCode:     "PROC001",
		CoveragePercent:   80,
		WaitingPeriodDays: 90,
	}
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	patientID := uuid.New()
	// Affiliated yesterday → still within 90-day waiting period
	aff := activeAffiliation(patientID, planID, time.Now().AddDate(0, 0, -1))
	affRepo := &mockAffiliationRepo{findActiveResult: aff}
	calc := service.NewCoverageCalculator(affRepo)

	result, err := calc.Calculate(context.Background(), baseInput(a, planID, patientID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("patient in waiting period should not be covered")
	}
}

func TestCalculate_AgeBelowMinimum(t *testing.T) {
	ageMin := 18
	rule := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 80,
		AgeMin:          &ageMin,
	}
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	patientID := uuid.New()
	aff := activeAffiliation(patientID, planID, time.Now().AddDate(-1, 0, 0))
	affRepo := &mockAffiliationRepo{findActiveResult: aff}
	calc := service.NewCoverageCalculator(affRepo)

	input := baseInput(a, planID, patientID)
	input.PatientAge = 17 // below minimum
	result, err := calc.Calculate(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("patient below age minimum should not be covered")
	}
}

func TestCalculate_AgeAboveMaximum(t *testing.T) {
	ageMax := 65
	rule := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 80,
		AgeMax:          &ageMax,
	}
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	patientID := uuid.New()
	aff := activeAffiliation(patientID, planID, time.Now().AddDate(-1, 0, 0))
	affRepo := &mockAffiliationRepo{findActiveResult: aff}
	calc := service.NewCoverageCalculator(affRepo)

	input := baseInput(a, planID, patientID)
	input.PatientAge = 66 // above maximum
	result, err := calc.Calculate(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("patient above age maximum should not be covered")
	}
}

func TestCalculate_MaxPerYearExceeded(t *testing.T) {
	maxPerYear := 5
	rule := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 80,
		MaxPerYear:      &maxPerYear,
	}
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	patientID := uuid.New()
	aff := activeAffiliation(patientID, planID, time.Now().AddDate(-1, 0, 0))
	affRepo := &mockAffiliationRepo{findActiveResult: aff}
	calc := service.NewCoverageCalculator(affRepo)

	input := baseInput(a, planID, patientID)
	input.VisitsThisYear = 5 // equals limit → exceeded (>= maxPerYear)
	result, err := calc.Calculate(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCovered {
		t.Fatal("max annual visits exceeded should not be covered")
	}
}

func TestCalculate_MaxPerYear_NotExceeded(t *testing.T) {
	maxPerYear := 5
	rule := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 80,
		MaxPerYear:      &maxPerYear,
	}
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	patientID := uuid.New()
	aff := activeAffiliation(patientID, planID, time.Now().AddDate(-1, 0, 0))
	affRepo := &mockAffiliationRepo{findActiveResult: aff}
	calc := service.NewCoverageCalculator(affRepo)

	input := baseInput(a, planID, patientID)
	input.VisitsThisYear = 4 // under limit
	result, err := calc.Calculate(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsCovered {
		t.Fatalf("should be covered: %s", result.RejectionReason)
	}
}

func TestCalculate_Success_PlanCopay(t *testing.T) {
	rule := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 80,
	}
	// Plan with 20% percent copay and no CoPayOverride on rule → uses plan's copay
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypePercent, 20, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	patientID := uuid.New()
	aff := activeAffiliation(patientID, planID, time.Now().AddDate(-1, 0, 0))
	affRepo := &mockAffiliationRepo{findActiveResult: aff}
	calc := service.NewCoverageCalculator(affRepo)

	result, err := calc.Calculate(context.Background(), baseInput(a, planID, patientID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsCovered {
		t.Fatalf("expected covered, reason: %s", result.RejectionReason)
	}
	if result.CoveragePercent != 80 {
		t.Fatalf("expected CoveragePercent=80, got %d", result.CoveragePercent)
	}
	if result.CoPayType != valueobject.CoPayTypePercent || result.CoPayValue != 20 {
		t.Fatalf("expected plan copay (Percent/20), got %s/%d", result.CoPayType, result.CoPayValue)
	}
	if result.RequiresAuthorization {
		t.Fatal("should not require authorization")
	}
}

func TestCalculate_Success_CoPayOverride(t *testing.T) {
	override := &valueobject.CoPayOverride{
		CoPayType:  valueobject.CoPayTypeFixedAmount,
		CoPayValue: 1500,
	}
	rule := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 70,
		CoPayOverride:   override,
	}
	// Plan has percent copay but rule overrides with fixed amount
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypePercent, 20, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	patientID := uuid.New()
	aff := activeAffiliation(patientID, planID, time.Now().AddDate(-1, 0, 0))
	affRepo := &mockAffiliationRepo{findActiveResult: aff}
	calc := service.NewCoverageCalculator(affRepo)

	result, err := calc.Calculate(context.Background(), baseInput(a, planID, patientID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsCovered {
		t.Fatalf("expected covered, reason: %s", result.RejectionReason)
	}
	if result.CoPayType != valueobject.CoPayTypeFixedAmount || result.CoPayValue != 1500 {
		t.Fatalf("expected override copay (FixedAmount/1500), got %s/%d", result.CoPayType, result.CoPayValue)
	}
}

func TestCalculate_RequiresAuthorization_PlanPreAuth(t *testing.T) {
	rule := aggregate.ProcedureRule{
		ProcedureCode:         "PROC001",
		CoveragePercent:       80,
		RequiresAuthorization: false, // rule doesn't require it
	}
	// Plan requires pre-auth → result must require authorization
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, true)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	patientID := uuid.New()
	aff := activeAffiliation(patientID, planID, time.Now().AddDate(-1, 0, 0))
	affRepo := &mockAffiliationRepo{findActiveResult: aff}
	calc := service.NewCoverageCalculator(affRepo)

	result, err := calc.Calculate(context.Background(), baseInput(a, planID, patientID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.RequiresAuthorization {
		t.Fatal("plan pre-auth should propagate to RequiresAuthorization")
	}
}

func TestCalculate_RequiresAuthorization_RuleRequiresAuth(t *testing.T) {
	rule := aggregate.ProcedureRule{
		ProcedureCode:         "PROC001",
		CoveragePercent:       80,
		RequiresAuthorization: true, // rule requires it
	}
	// Plan does NOT require pre-auth → rule flag drives RequiresAuthorization
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	patientID := uuid.New()
	aff := activeAffiliation(patientID, planID, time.Now().AddDate(-1, 0, 0))
	affRepo := &mockAffiliationRepo{findActiveResult: aff}
	calc := service.NewCoverageCalculator(affRepo)

	result, err := calc.Calculate(context.Background(), baseInput(a, planID, patientID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.RequiresAuthorization {
		t.Fatal("rule RequiresAuthorization should propagate to result")
	}
}

func TestCalculate_WaitingPeriodZero_NoRestriction(t *testing.T) {
	rule := aggregate.ProcedureRule{
		ProcedureCode:     "PROC001",
		CoveragePercent:   80,
		WaitingPeriodDays: 0, // no waiting period
	}
	plan, planID := activePlanWithRule(rule, valueobject.CoPayTypeNone, 0, false)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	patientID := uuid.New()
	// Affiliated just 1 day ago — no waiting period so should be fine
	aff := activeAffiliation(patientID, planID, time.Now().AddDate(0, 0, -1))
	affRepo := &mockAffiliationRepo{findActiveResult: aff}
	calc := service.NewCoverageCalculator(affRepo)

	result, err := calc.Calculate(context.Background(), baseInput(a, planID, patientID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsCovered {
		t.Fatalf("zero waiting period should not block coverage: %s", result.RejectionReason)
	}
}

// ── AffiliationVerifier.Verify ────────────────────────────────────

func TestVerify_AgreementNotFound(t *testing.T) {
	agrRepo := &mockAgreementRepo{findByIDErr: errors.New("not found")}
	verifier := service.NewAffiliationVerifier(agrRepo, &mockAffiliationRepo{})

	result, err := verifier.Verify(context.Background(), service.VerifyInput{
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

func TestVerify_AgreementNotValid(t *testing.T) {
	suspended := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusSuspended,
		time.Now().AddDate(-1, 0, 0), nil,
		agreeEmail(), agreePhone(), 0,
		nil, time.Now(), time.Now(), nil, 1,
	)
	agrRepo := &mockAgreementRepo{findByIDResult: suspended}
	verifier := service.NewAffiliationVerifier(agrRepo, &mockAffiliationRepo{})

	result, err := verifier.Verify(context.Background(), service.VerifyInput{
		AgreementID: suspended.ID(), PlanID: uuid.New(),
		PatientID: uuid.New(), AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobject.AffiliationStatusSuspended {
		t.Fatalf("expected Suspended, got %s", result.Status)
	}
}

func TestVerify_PlanNotFound(t *testing.T) {
	a := activeAgreement(valueobject.ProviderTypeObraSocial, nil) // no plans
	agrRepo := &mockAgreementRepo{findByIDResult: a}
	verifier := service.NewAffiliationVerifier(agrRepo, &mockAffiliationRepo{})

	result, err := verifier.Verify(context.Background(), service.VerifyInput{
		AgreementID: a.ID(), PlanID: uuid.New(), // unknown plan
		PatientID: uuid.New(), AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobject.AffiliationStatusCancelled {
		t.Fatalf("expected Cancelled, got %s", result.Status)
	}
}

func TestVerify_PlanDiscontinued(t *testing.T) {
	planID := uuid.New()
	discontinued := aggregate.ReconstitutePlan(
		planID, "P001", "Plan",
		valueobject.CoPayTypeNone, 0, false, nil,
		nil, valueobject.PlanStatusDiscontinued,
		time.Now(), time.Now(),
	)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{discontinued})
	agrRepo := &mockAgreementRepo{findByIDResult: a}
	verifier := service.NewAffiliationVerifier(agrRepo, &mockAffiliationRepo{})

	result, err := verifier.Verify(context.Background(), service.VerifyInput{
		AgreementID: a.ID(), PlanID: planID,
		PatientID: uuid.New(), AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobject.AffiliationStatusCancelled {
		t.Fatalf("expected Cancelled for discontinued plan, got %s", result.Status)
	}
}

func TestVerify_AffiliationNil(t *testing.T) {
	planID := uuid.New()
	plan := aggregate.ReconstitutePlan(
		planID, "P001", "Plan",
		valueobject.CoPayTypeNone, 0, false, nil,
		nil, valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	agrRepo := &mockAgreementRepo{findByIDResult: a}
	affRepo := &mockAffiliationRepo{findActiveResult: nil} // no affiliation
	verifier := service.NewAffiliationVerifier(agrRepo, affRepo)

	result, err := verifier.Verify(context.Background(), service.VerifyInput{
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

func TestVerify_AffiliationError(t *testing.T) {
	planID := uuid.New()
	plan := aggregate.ReconstitutePlan(
		planID, "P001", "Plan",
		valueobject.CoPayTypeNone, 0, false, nil,
		nil, valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	agrRepo := &mockAgreementRepo{findByIDResult: a}
	affRepo := &mockAffiliationRepo{findActiveErr: errors.New("db error")}
	verifier := service.NewAffiliationVerifier(agrRepo, affRepo)

	result, err := verifier.Verify(context.Background(), service.VerifyInput{
		AgreementID: a.ID(), PlanID: planID,
		PatientID: uuid.New(), AppointmentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobject.AffiliationStatusCancelled {
		t.Fatalf("expected Cancelled on affiliation error, got %s", result.Status)
	}
}

func TestVerify_Success(t *testing.T) {
	planID := uuid.New()
	patientID := uuid.New()
	plan := aggregate.ReconstitutePlan(
		planID, "P001", "Plan",
		valueobject.CoPayTypeNone, 0, false, nil,
		nil, valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	a := activeAgreement(valueobject.ProviderTypeObraSocial, []aggregate.Plan{plan})
	agrRepo := &mockAgreementRepo{findByIDResult: a}
	affRepo := &mockAffiliationRepo{
		findActiveResult: activeAffiliation(patientID, planID, time.Now().AddDate(-1, 0, 0)),
	}
	verifier := service.NewAffiliationVerifier(agrRepo, affRepo)

	result, err := verifier.Verify(context.Background(), service.VerifyInput{
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
