package aggregate_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── helpers ───────────────────────────────────────────────────────

func email() sharedvo.Email {
	e, _ := sharedvo.NewEmail("test@clinic.com")
	return e
}

func phone() sharedvo.PhoneNumber {
	p, _ := sharedvo.NewPhoneNumber("+54911000001")
	return p
}

func basicPlan() *aggregate.Plan {
	p, _ := aggregate.NewPlan("P001", "Plan Básico", valueobject.CoPayTypeNone, 0, false, nil)
	return p
}

func newActiveAgreement() *aggregate.Agreement {
	a, _ := aggregate.NewAgreement(
		"AGR001", "Proveedor Test",
		valueobject.ProviderTypeObraSocial,
		time.Now().AddDate(-1, 0, 0), nil,
		email(), phone(), 5,
		basicPlan(), nil,
	)
	a.PendingEvents() // drain creation event
	return a
}

func reconstituteSuspended(plans []aggregate.Plan) *aggregate.Agreement {
	return aggregate.Reconstitute(
		uuid.New(), "AGR001", "Proveedor",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusSuspended,
		time.Now().AddDate(-1, 0, 0), nil,
		email(), phone(), 0,
		plans, time.Now(), time.Now(), nil, 1,
	)
}

// ── ProcedureRule.Validate ────────────────────────────────────────

func TestProcedureRule_Validate_EmptyCode(t *testing.T) {
	r := aggregate.ProcedureRule{ProcedureCode: "", CoveragePercent: 80}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty procedure_code")
	}
}

func TestProcedureRule_Validate_CoveragePercentNegative(t *testing.T) {
	r := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: -1}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for negative coverage_percent")
	}
}

func TestProcedureRule_Validate_CoveragePercentOver100(t *testing.T) {
	r := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 101}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for coverage_percent > 100")
	}
}

func TestProcedureRule_Validate_NegativeWaitingPeriod(t *testing.T) {
	r := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80, WaitingPeriodDays: -1}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for negative waiting_period_days")
	}
}

func TestProcedureRule_Validate_CoPayOverridePercentExceeds100(t *testing.T) {
	override := &valueobject.CoPayOverride{CoPayType: valueobject.CoPayTypePercent, CoPayValue: 30}
	r := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 80, // 80 + 30 = 110 > 100
		CoPayOverride:   override,
	}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error: coverage + copay_percent > 100")
	}
}

func TestProcedureRule_Validate_CoPayOverridePercentOK(t *testing.T) {
	override := &valueobject.CoPayOverride{CoPayType: valueobject.CoPayTypePercent, CoPayValue: 20}
	r := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 80, // 80 + 20 = 100 → ok
		CoPayOverride:   override,
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcedureRule_Validate_CoPayOverrideFixedAmount_SkipsPercentCheck(t *testing.T) {
	override := &valueobject.CoPayOverride{CoPayType: valueobject.CoPayTypeFixedAmount, CoPayValue: 5000}
	r := aggregate.ProcedureRule{
		ProcedureCode:   "PROC001",
		CoveragePercent: 80,
		CoPayOverride:   override,
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("FixedAmount override should not trigger percent check: %v", err)
	}
}

func TestProcedureRule_Validate_Valid(t *testing.T) {
	r := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80, WaitingPeriodDays: 30}
	if err := r.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── NewPlan ───────────────────────────────────────────────────────

func TestNewPlan_EmptyCode(t *testing.T) {
	_, err := aggregate.NewPlan("", "Plan Básico", valueobject.CoPayTypeNone, 0, false, nil)
	if err == nil {
		t.Fatal("expected error for empty plan_code")
	}
}

func TestNewPlan_EmptyName(t *testing.T) {
	_, err := aggregate.NewPlan("P001", "", valueobject.CoPayTypeNone, 0, false, nil)
	if err == nil {
		t.Fatal("expected error for empty plan_name")
	}
}

func TestNewPlan_CoPayTypeNone_NonZeroValue(t *testing.T) {
	_, err := aggregate.NewPlan("P001", "Plan", valueobject.CoPayTypeNone, 10, false, nil)
	if err == nil {
		t.Fatal("expected error: CoPayTypeNone with non-zero value")
	}
}

func TestNewPlan_CoPayTypePercent_NegativeValue(t *testing.T) {
	_, err := aggregate.NewPlan("P001", "Plan", valueobject.CoPayTypePercent, -1, false, nil)
	if err == nil {
		t.Fatal("expected error: CoPayTypePercent with negative value")
	}
}

func TestNewPlan_CoPayTypePercent_Over100(t *testing.T) {
	_, err := aggregate.NewPlan("P001", "Plan", valueobject.CoPayTypePercent, 101, false, nil)
	if err == nil {
		t.Fatal("expected error: CoPayTypePercent > 100")
	}
}

func TestNewPlan_Valid_None(t *testing.T) {
	maxVisits := 52
	p, err := aggregate.NewPlan("P001", "Plan Básico", valueobject.CoPayTypeNone, 0, true, &maxVisits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PlanCode() != "P001" || p.PlanName() != "Plan Básico" {
		t.Fatal("unexpected plan fields")
	}
	if !p.RequiresPreAuthorization() {
		t.Fatal("expected RequiresPreAuthorization=true")
	}
	if p.MaxAnnualVisits() == nil || *p.MaxAnnualVisits() != 52 {
		t.Fatal("expected MaxAnnualVisits=52")
	}
	if !p.Status().IsActive() {
		t.Fatal("expected Active status on creation")
	}
}

func TestNewPlan_Valid_Percent(t *testing.T) {
	p, err := aggregate.NewPlan("P002", "Plan Premium", valueobject.CoPayTypePercent, 20, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.CoPayType() != valueobject.CoPayTypePercent || p.CoPayValue() != 20 {
		t.Fatal("unexpected copay fields")
	}
}

func TestNewPlan_Valid_FixedAmount(t *testing.T) {
	p, err := aggregate.NewPlan("P003", "Plan Fixed", valueobject.CoPayTypeFixedAmount, 5000, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.CoPayValue() != 5000 {
		t.Fatal("unexpected copay value")
	}
}

// ── Plan.UpsertProcedureRule ──────────────────────────────────────

func TestPlan_UpsertProcedureRule_Discontinued(t *testing.T) {
	p := basicPlan()
	_ = p.Discontinue()
	r := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80}
	if err := p.UpsertProcedureRule(r); err == nil {
		t.Fatal("expected error: discontinued plan cannot accept rules")
	}
}

func TestPlan_UpsertProcedureRule_InvalidRule(t *testing.T) {
	p := basicPlan()
	r := aggregate.ProcedureRule{ProcedureCode: "", CoveragePercent: 80}
	if err := p.UpsertProcedureRule(r); err == nil {
		t.Fatal("expected error for invalid rule")
	}
}

func TestPlan_UpsertProcedureRule_ExceedsCopayPercent(t *testing.T) {
	// Plan with 30% copay; rule with 80% coverage → 80+30=110 > 100
	p, _ := aggregate.NewPlan("P001", "Plan", valueobject.CoPayTypePercent, 30, false, nil)
	r := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80}
	if err := p.UpsertProcedureRule(r); err == nil {
		t.Fatal("expected error: coverage + copay > 100")
	}
}

func TestPlan_UpsertProcedureRule_AddNew(t *testing.T) {
	p := basicPlan()
	r := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80}
	if err := p.UpsertProcedureRule(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.CoveredProcedures()) != 1 {
		t.Fatalf("expected 1 procedure, got %d", len(p.CoveredProcedures()))
	}
}

func TestPlan_UpsertProcedureRule_UpdateExisting(t *testing.T) {
	p := basicPlan()
	r1 := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80}
	r2 := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 90}
	_ = p.UpsertProcedureRule(r1)
	if err := p.UpsertProcedureRule(r2); err != nil {
		t.Fatalf("unexpected error on update: %v", err)
	}
	procs := p.CoveredProcedures()
	if len(procs) != 1 {
		t.Fatalf("expected 1 procedure after upsert, got %d", len(procs))
	}
	if procs[0].CoveragePercent != 90 {
		t.Fatalf("expected updated coverage=90, got %d", procs[0].CoveragePercent)
	}
}

// ── Plan.FindProcedureRule ────────────────────────────────────────

func TestPlan_FindProcedureRule_Found(t *testing.T) {
	p := basicPlan()
	_ = p.UpsertProcedureRule(aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80})
	rule, ok := p.FindProcedureRule("PROC001")
	if !ok || rule == nil {
		t.Fatal("expected rule to be found")
	}
	if rule.CoveragePercent != 80 {
		t.Fatalf("expected coverage=80, got %d", rule.CoveragePercent)
	}
}

func TestPlan_FindProcedureRule_NotFound(t *testing.T) {
	p := basicPlan()
	_, ok := p.FindProcedureRule("PROC999")
	if ok {
		t.Fatal("expected rule not found")
	}
}

// ── Plan.Discontinue ─────────────────────────────────────────────

func TestPlan_Discontinue_Success(t *testing.T) {
	p := basicPlan()
	if err := p.Discontinue(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Status().IsActive() {
		t.Fatal("expected Discontinued status")
	}
}

func TestPlan_Discontinue_AlreadyDiscontinued(t *testing.T) {
	p := basicPlan()
	_ = p.Discontinue()
	if err := p.Discontinue(); err == nil {
		t.Fatal("expected error: already discontinued")
	}
}

// ── Plan getters ──────────────────────────────────────────────────

func TestPlan_Getters(t *testing.T) {
	now := time.Now()
	maxVisits := 12
	procs := []aggregate.ProcedureRule{{ProcedureCode: "P", CoveragePercent: 50}}
	p := aggregate.ReconstitutePlan(
		uuid.New(), "CODE", "NAME",
		valueobject.CoPayTypeFixedAmount, 300, true, &maxVisits,
		procs, valueobject.PlanStatusDiscontinued,
		now, now,
	)
	if p.PlanCode() != "CODE" {
		t.Fatalf("PlanCode mismatch")
	}
	if p.PlanName() != "NAME" {
		t.Fatalf("PlanName mismatch")
	}
	if p.CoPayType() != valueobject.CoPayTypeFixedAmount {
		t.Fatalf("CoPayType mismatch")
	}
	if p.CoPayValue() != 300 {
		t.Fatalf("CoPayValue mismatch")
	}
	if !p.RequiresPreAuthorization() {
		t.Fatalf("RequiresPreAuthorization mismatch")
	}
	if p.MaxAnnualVisits() == nil || *p.MaxAnnualVisits() != 12 {
		t.Fatalf("MaxAnnualVisits mismatch")
	}
	if p.Status().IsActive() {
		t.Fatalf("expected Discontinued status")
	}
	if len(p.CoveredProcedures()) != 1 {
		t.Fatalf("CoveredProcedures mismatch")
	}
	if p.CreatedAt().IsZero() || p.UpdatedAt().IsZero() {
		t.Fatalf("timestamps must not be zero")
	}
}

// ── NewAgreement ──────────────────────────────────────────────────

func TestNewAgreement_EmptyCode(t *testing.T) {
	_, err := aggregate.NewAgreement("", "Provider", valueobject.ProviderTypeObraSocial,
		time.Now(), nil, email(), phone(), 0, basicPlan(), nil)
	if err == nil {
		t.Fatal("expected error for empty agreement_code")
	}
}

func TestNewAgreement_EmptyProviderName(t *testing.T) {
	_, err := aggregate.NewAgreement("AGR001", "", valueobject.ProviderTypeObraSocial,
		time.Now(), nil, email(), phone(), 0, basicPlan(), nil)
	if err == nil {
		t.Fatal("expected error for empty provider_name")
	}
}

func TestNewAgreement_ValidUntilBeforeValidFrom(t *testing.T) {
	past := time.Now().AddDate(0, 0, -1)
	_, err := aggregate.NewAgreement("AGR001", "Provider", valueobject.ProviderTypeObraSocial,
		time.Now(), &past, email(), phone(), 0, basicPlan(), nil)
	if err == nil {
		t.Fatal("expected error: validUntil before validFrom")
	}
}

func TestNewAgreement_NegativeCancellationDays(t *testing.T) {
	_, err := aggregate.NewAgreement("AGR001", "Provider", valueobject.ProviderTypeObraSocial,
		time.Now(), nil, email(), phone(), -1, basicPlan(), nil)
	if err == nil {
		t.Fatal("expected error: negative cancellation_notice_days")
	}
}

func TestNewAgreement_NonPrivado_NoPlan(t *testing.T) {
	_, err := aggregate.NewAgreement("AGR001", "Provider", valueobject.ProviderTypeObraSocial,
		time.Now(), nil, email(), phone(), 0, nil, nil)
	if err == nil {
		t.Fatal("expected error: non-Privado requires first plan")
	}
}

func TestNewAgreement_Privado_NoPlan(t *testing.T) {
	a, err := aggregate.NewAgreement("AGR001", "Provider", valueobject.ProviderTypePrivado,
		time.Now(), nil, email(), phone(), 0, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.Plans()) != 0 {
		t.Fatal("Privado should have no plans")
	}
}

func TestNewAgreement_NonPrivado_WithPlan(t *testing.T) {
	createdBy := uuid.New()
	validUntil := time.Now().AddDate(1, 0, 0)
	a, err := aggregate.NewAgreement(
		"AGR001", "Provider ObraSocial",
		valueobject.ProviderTypeObraSocial,
		time.Now().AddDate(-1, 0, 0), &validUntil,
		email(), phone(), 5,
		basicPlan(), &createdBy,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !a.Status().IsActive() {
		t.Fatal("expected Active status")
	}
	if len(a.Plans()) != 1 {
		t.Fatal("expected 1 plan")
	}
	if a.CancellationNoticeDays() != 5 {
		t.Fatal("expected CancellationNoticeDays=5")
	}
	if a.ValidUntil() == nil {
		t.Fatal("expected non-nil ValidUntil")
	}
	evts := a.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "agreement.created" {
		t.Fatalf("expected AgreementCreated event, got %v", evts)
	}
	if len(a.PendingEvents()) != 0 {
		t.Fatal("PendingEvents should be cleared after first call")
	}
}

// ── Agreement.AddPlan ─────────────────────────────────────────────

func TestAgreement_AddPlan_NotActive(t *testing.T) {
	a := reconstituteSuspended([]aggregate.Plan{})
	plan := basicPlan()
	if err := a.AddPlan(*plan); err == nil {
		t.Fatal("expected error: agreement not active")
	}
}

func TestAgreement_AddPlan_Privado(t *testing.T) {
	a := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		valueobject.ProviderTypePrivado, valueobject.AgreementStatusActive,
		time.Now(), nil, email(), phone(), 0,
		[]aggregate.Plan{}, time.Now(), time.Now(), nil, 1,
	)
	plan := basicPlan()
	if err := a.AddPlan(*plan); err == nil {
		t.Fatal("expected error: Privado agreements cannot have plans")
	}
}

func TestAgreement_AddPlan_DuplicateCode(t *testing.T) {
	a := newActiveAgreement() // already has P001
	plan := basicPlan()       // also P001
	if err := a.AddPlan(*plan); err == nil {
		t.Fatal("expected error: duplicate plan code")
	}
}

func TestAgreement_AddPlan_Success(t *testing.T) {
	a := newActiveAgreement()
	plan, _ := aggregate.NewPlan("P002", "Plan Premium", valueobject.CoPayTypeNone, 0, false, nil)
	if err := a.AddPlan(*plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.Plans()) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(a.Plans()))
	}
	evts := a.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "agreement.plan_added" {
		t.Fatalf("expected AgreementPlanAdded event, got %v", evts)
	}
}

// ── Agreement.UpsertProcedureRule ─────────────────────────────────

func TestAgreement_UpsertProcedureRule_PlanNotFound(t *testing.T) {
	a := newActiveAgreement()
	r := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80}
	if err := a.UpsertProcedureRule(uuid.New(), r); err == nil {
		t.Fatal("expected error: plan not found")
	}
}

func TestAgreement_UpsertProcedureRule_PlanFoundRuleInvalid(t *testing.T) {
	a := newActiveAgreement()
	planID := a.Plans()[0].ID()
	r := aggregate.ProcedureRule{ProcedureCode: "", CoveragePercent: 80} // empty code → invalid
	if err := a.UpsertProcedureRule(planID, r); err == nil {
		t.Fatal("expected error: invalid rule")
	}
}

func TestAgreement_UpsertProcedureRule_Success(t *testing.T) {
	a := newActiveAgreement()
	planID := a.Plans()[0].ID()
	r := aggregate.ProcedureRule{ProcedureCode: "PROC001", CoveragePercent: 80}
	if err := a.UpsertProcedureRule(planID, r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	evts := a.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "agreement.procedure_rule_updated" {
		t.Fatalf("expected AgreementProcedureRuleUpdated event, got %v", evts)
	}
}

// ── Agreement.Suspend ────────────────────────────────────────────

func TestAgreement_Suspend_AlreadySuspended(t *testing.T) {
	a := reconstituteSuspended([]aggregate.Plan{})
	if err := a.Suspend("reason", uuid.New()); err == nil {
		t.Fatal("expected error: already suspended")
	}
}

func TestAgreement_Suspend_Expired(t *testing.T) {
	a := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusExpired,
		time.Now(), nil, email(), phone(), 0,
		[]aggregate.Plan{}, time.Now(), time.Now(), nil, 1,
	)
	if err := a.Suspend("reason", uuid.New()); err == nil {
		t.Fatal("expected error: cannot suspend expired agreement")
	}
}

func TestAgreement_Suspend_Success(t *testing.T) {
	a := newActiveAgreement()
	if err := a.Suspend("maintenance", uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Status() != valueobject.AgreementStatusSuspended {
		t.Fatal("expected Suspended status")
	}
	evts := a.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "agreement.suspended" {
		t.Fatalf("expected AgreementSuspended event, got %v", evts)
	}
}

// ── Agreement.Activate ────────────────────────────────────────────

func TestAgreement_Activate_AlreadyActive(t *testing.T) {
	a := newActiveAgreement()
	if err := a.Activate(uuid.New()); err == nil {
		t.Fatal("expected error: already active")
	}
}

func TestAgreement_Activate_Expired(t *testing.T) {
	a := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusExpired,
		time.Now(), nil, email(), phone(), 0,
		[]aggregate.Plan{}, time.Now(), time.Now(), nil, 1,
	)
	if err := a.Activate(uuid.New()); err == nil {
		t.Fatal("expected error: cannot activate expired agreement")
	}
}

func TestAgreement_Activate_NonPrivado_NoActivePlan(t *testing.T) {
	// Suspended agreement with no plans
	a := reconstituteSuspended([]aggregate.Plan{})
	if err := a.Activate(uuid.New()); err == nil {
		t.Fatal("expected error: no active plan")
	}
}

func TestAgreement_Activate_Success_NonPrivado(t *testing.T) {
	plan := aggregate.ReconstitutePlan(
		uuid.New(), "P001", "Plan",
		valueobject.CoPayTypeNone, 0, false, nil,
		[]aggregate.ProcedureRule{}, valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	a := reconstituteSuspended([]aggregate.Plan{plan})
	if err := a.Activate(uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !a.Status().IsActive() {
		t.Fatal("expected Active status")
	}
	evts := a.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "agreement.activated" {
		t.Fatalf("expected AgreementActivated event, got %v", evts)
	}
}

func TestAgreement_Activate_Success_Privado(t *testing.T) {
	a := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		valueobject.ProviderTypePrivado, valueobject.AgreementStatusSuspended,
		time.Now(), nil, email(), phone(), 0,
		[]aggregate.Plan{}, time.Now(), time.Now(), nil, 1,
	)
	if err := a.Activate(uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !a.Status().IsActive() {
		t.Fatal("expected Active status after activate")
	}
}

// ── Agreement.Expire ─────────────────────────────────────────────

func TestAgreement_Expire(t *testing.T) {
	a := newActiveAgreement()
	a.Expire()
	if a.Status() != valueobject.AgreementStatusExpired {
		t.Fatal("expected Expired status")
	}
	evts := a.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "agreement.expired" {
		t.Fatalf("expected AgreementExpired event, got %v", evts)
	}
}

// ── Agreement.FindPlan ────────────────────────────────────────────

func TestAgreement_FindPlan_Found(t *testing.T) {
	a := newActiveAgreement()
	planID := a.Plans()[0].ID()
	p, ok := a.FindPlan(planID)
	if !ok || p == nil {
		t.Fatal("expected plan to be found")
	}
}

func TestAgreement_FindPlan_NotFound(t *testing.T) {
	a := newActiveAgreement()
	_, ok := a.FindPlan(uuid.New())
	if ok {
		t.Fatal("expected plan not found")
	}
}

// ── Agreement.IsValidAt ───────────────────────────────────────────

func TestAgreement_IsValidAt_NotActive(t *testing.T) {
	a := reconstituteSuspended([]aggregate.Plan{})
	if a.IsValidAt(time.Now()) {
		t.Fatal("suspended agreement should not be valid")
	}
}

func TestAgreement_IsValidAt_BeforeValidFrom(t *testing.T) {
	a := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusActive,
		time.Now().AddDate(0, 0, 1), nil, // validFrom = tomorrow
		email(), phone(), 0,
		[]aggregate.Plan{}, time.Now(), time.Now(), nil, 1,
	)
	if a.IsValidAt(time.Now()) {
		t.Fatal("agreement not valid before validFrom")
	}
}

func TestAgreement_IsValidAt_AfterValidUntil(t *testing.T) {
	pastUntil := time.Now().AddDate(-1, 0, 0)
	a := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusActive,
		time.Now().AddDate(-2, 0, 0), &pastUntil,
		email(), phone(), 0,
		[]aggregate.Plan{}, time.Now(), time.Now(), nil, 1,
	)
	if a.IsValidAt(time.Now()) {
		t.Fatal("agreement not valid after validUntil")
	}
}

func TestAgreement_IsValidAt_InRange(t *testing.T) {
	a := newActiveAgreement()
	if !a.IsValidAt(time.Now()) {
		t.Fatal("expected agreement to be valid now")
	}
}

// ── Agreement.BumpVersion ─────────────────────────────────────────

func TestAgreement_BumpVersion(t *testing.T) {
	a := newActiveAgreement()
	v1 := a.Version()
	a.BumpVersion()
	if a.Version() != v1+1 {
		t.Fatalf("expected version %d, got %d", v1+1, a.Version())
	}
}

// ── Agreement getters ─────────────────────────────────────────────

func TestAgreement_Getters(t *testing.T) {
	createdBy := uuid.New()
	a, _ := aggregate.NewAgreement(
		"  AGR001  ", "  Provider  ",
		valueobject.ProviderTypeObraSocial,
		time.Now().AddDate(-1, 0, 0), nil,
		email(), phone(), 3,
		basicPlan(), &createdBy,
	)
	if a.AgreementCode() != "AGR001" {
		t.Fatalf("expected trimmed code, got %q", a.AgreementCode())
	}
	if a.ProviderName() != "Provider" {
		t.Fatalf("expected trimmed name, got %q", a.ProviderName())
	}
	if a.ProviderType() != valueobject.ProviderTypeObraSocial {
		t.Fatal("ProviderType mismatch")
	}
	if a.ContactEmail().String() != "test@clinic.com" {
		t.Fatal("ContactEmail mismatch")
	}
	if a.ContactPhone().String() != "+54911000001" {
		t.Fatalf("ContactPhone mismatch: got %s", a.ContactPhone().String())
	}
	if a.CancellationNoticeDays() != 3 {
		t.Fatal("CancellationNoticeDays mismatch")
	}
	if a.CreatedBy() == nil || *a.CreatedBy() != createdBy {
		t.Fatal("CreatedBy mismatch")
	}
	if a.ID() == uuid.Nil {
		t.Fatal("ID must not be nil")
	}
	if a.CreatedAt().IsZero() || a.UpdatedAt().IsZero() {
		t.Fatal("timestamps must not be zero")
	}
	if a.ValidFrom().IsZero() {
		t.Fatal("ValidFrom must not be zero")
	}
	if a.Version() != 1 {
		t.Fatal("expected initial version=1")
	}
}

// ── Reconstitute ─────────────────────────────────────────────────

func TestAgreement_Reconstitute_NoPendingEvents(t *testing.T) {
	a := aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusActive,
		time.Now(), nil, email(), phone(), 0,
		[]aggregate.Plan{}, time.Now(), time.Now(), nil, 5,
	)
	if len(a.PendingEvents()) != 0 {
		t.Fatal("Reconstitute should not generate events")
	}
	if a.Version() != 5 {
		t.Fatal("expected version=5")
	}
}
