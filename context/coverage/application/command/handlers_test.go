package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/service"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── Mocks ─────────────────────────────────────────────────────────

type mockBus struct{ publishErr error }

func (m *mockBus) Publish(_ context.Context, _ events.DomainEvent) error { return m.publishErr }
func (m *mockBus) Subscribe(_ context.Context, _ events.SubscribeOptions, _ events.Handler) error {
	return nil
}
func (m *mockBus) Close() error { return nil }

type mockAgreementRepo struct {
	saveErr            error
	updateErr          error
	findByIDResult     *aggregate.Agreement
	findByIDErr        error
	existsByCodeResult bool
	existsByCodeErr    error
	findActiveResult   sharedtypes.PagedResult[*aggregate.Agreement]
	findActiveErr      error
	findByProvResult   sharedtypes.PagedResult[*aggregate.Agreement]
	findByProvErr      error
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
	return m.existsByCodeResult, m.existsByCodeErr
}
func (m *mockAgreementRepo) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.Agreement, error) {
	return nil, nil
}

type mockAuthorizationRepo struct {
	saveErr                    error
	updateErr                  error
	findByIDResult             *aggregate.AuthorizationRequest
	findByIDErr                error
	findPendingByPatientResult *aggregate.AuthorizationRequest
	findPendingByPatientErr    error
}

func (m *mockAuthorizationRepo) Save(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return m.saveErr
}
func (m *mockAuthorizationRepo) Update(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return m.updateErr
}
func (m *mockAuthorizationRepo) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.AuthorizationRequest, error) {
	return m.findByIDResult, m.findByIDErr
}
func (m *mockAuthorizationRepo) FindPendingByAgreement(_ context.Context, _ uuid.UUID) ([]*aggregate.AuthorizationRequest, error) {
	return nil, nil
}
func (m *mockAuthorizationRepo) FindPendingByPatient(_ context.Context, _ sharedtypes.PatientID, _ string) (*aggregate.AuthorizationRequest, error) {
	return m.findPendingByPatientResult, m.findPendingByPatientErr
}
func (m *mockAuthorizationRepo) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.AuthorizationRequest, error) {
	return nil, nil
}

type mockCoverageCache struct {
	invalidateErr error
}

func (m *mockCoverageCache) GetCoverageResult(_ context.Context, _ uuid.UUID, _ string) (*valueobject.CoverageResult, error) {
	return nil, nil
}
func (m *mockCoverageCache) SetCoverageResult(_ context.Context, _ uuid.UUID, _ string, _ valueobject.CoverageResult) error {
	return nil
}
func (m *mockCoverageCache) InvalidatePlan(_ context.Context, _ uuid.UUID) error {
	return m.invalidateErr
}

// ── Helpers ───────────────────────────────────────────────────────

func validActiveAgreement(pt valueobject.ProviderType) *aggregate.Agreement {
	email, _ := sharedvo.NewEmail("contact@test.com")
	phone, _ := sharedvo.NewPhoneNumber("+54911000001")

	plans := []aggregate.Plan{}
	if !pt.IsPrivado() {
		plan := aggregate.ReconstitutePlan(
			uuid.New(), "P001", "Plan Básico",
			valueobject.CoPayTypeNone, 0, false, nil,
			[]aggregate.ProcedureRule{}, valueobject.PlanStatusActive,
			time.Now(), time.Now(),
		)
		plans = append(plans, plan)
	}

	return aggregate.Reconstitute(
		uuid.New(), "AGR001", "Proveedor Test",
		pt, valueobject.AgreementStatusActive,
		time.Now().AddDate(-1, 0, 0), nil,
		email, phone, 0,
		plans, time.Now(), time.Now(), nil, 1,
	)
}

func suspendedAgreement() *aggregate.Agreement {
	email, _ := sharedvo.NewEmail("contact@test.com")
	phone, _ := sharedvo.NewPhoneNumber("+54911000001")
	plan := aggregate.ReconstitutePlan(
		uuid.New(), "P001", "Plan Básico",
		valueobject.CoPayTypeNone, 0, false, nil,
		[]aggregate.ProcedureRule{}, valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	return aggregate.Reconstitute(
		uuid.New(), "AGR001", "Proveedor Test",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusSuspended,
		time.Now().AddDate(-1, 0, 0), nil,
		email, phone, 0,
		[]aggregate.Plan{plan}, time.Now(), time.Now(), nil, 1,
	)
}

func pendingAR() *aggregate.AuthorizationRequest {
	deadline := time.Now().Add(48 * time.Hour)
	ar, _ := aggregate.NewAuthorizationRequest(
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", nil, &deadline,
	)
	return ar
}

func baseCreateCmd() CreateAgreementCommand {
	validUntil := time.Now().AddDate(1, 0, 0)
	return CreateAgreementCommand{
		AgreementCode:          "AGR001",
		ProviderName:           "Proveedor Test",
		ProviderType:           "ObraSocial",
		ValidFrom:              time.Now().AddDate(-1, 0, 0),
		ValidUntil:             &validUntil,
		ContactEmail:           "contact@test.com",
		ContactPhone:           "+54911000001",
		CancellationNoticeDays: 5,
		FirstPlanCode:          "P001",
		FirstPlanName:          "Plan Básico",
		FirstPlanCoPayType:     "None",
		FirstPlanCoPayValue:    0,
	}
}

// ── CreateAgreementHandler ────────────────────────────────────────

func TestCreateAgreementHandler_InvalidProviderType(t *testing.T) {
	cmd := baseCreateCmd()
	cmd.ProviderType = "INVALID"
	h := NewCreateAgreementHandler(&mockAgreementRepo{}, &mockBus{})
	_, err := h.Handle(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error for invalid provider type")
	}
}

func TestCreateAgreementHandler_InvalidEmail(t *testing.T) {
	cmd := baseCreateCmd()
	cmd.ContactEmail = "not-an-email"
	h := NewCreateAgreementHandler(&mockAgreementRepo{}, &mockBus{})
	_, err := h.Handle(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error for invalid email")
	}
}

func TestCreateAgreementHandler_InvalidPhone(t *testing.T) {
	cmd := baseCreateCmd()
	cmd.ContactPhone = ""
	h := NewCreateAgreementHandler(&mockAgreementRepo{}, &mockBus{})
	_, err := h.Handle(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error for invalid phone")
	}
}

func TestCreateAgreementHandler_ExistsByCodeError(t *testing.T) {
	cmd := baseCreateCmd()
	repo := &mockAgreementRepo{existsByCodeErr: errors.New("db error")}
	h := NewCreateAgreementHandler(repo, &mockBus{})
	_, err := h.Handle(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error when ExistsByCode fails")
	}
}

func TestCreateAgreementHandler_AlreadyExists(t *testing.T) {
	cmd := baseCreateCmd()
	repo := &mockAgreementRepo{existsByCodeResult: true}
	h := NewCreateAgreementHandler(repo, &mockBus{})
	_, err := h.Handle(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error: agreement already exists")
	}
}

func TestCreateAgreementHandler_InvalidFirstPlanCoPayType(t *testing.T) {
	cmd := baseCreateCmd()
	cmd.FirstPlanCoPayType = "INVALID"
	h := NewCreateAgreementHandler(&mockAgreementRepo{}, &mockBus{})
	_, err := h.Handle(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error for invalid co_pay_type")
	}
}

func TestCreateAgreementHandler_FirstPlanValidationFails(t *testing.T) {
	cmd := baseCreateCmd()
	cmd.FirstPlanCode = ""
	h := NewCreateAgreementHandler(&mockAgreementRepo{}, &mockBus{})
	_, err := h.Handle(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error: empty plan code")
	}
}

func TestCreateAgreementHandler_NewAgreementFails(t *testing.T) {
	cmd := baseCreateCmd()
	cmd.ProviderType = "Privado"
	past := time.Now().AddDate(0, 0, -1)
	cmd.ValidFrom = time.Now()
	cmd.ValidUntil = &past // validUntil before validFrom
	h := NewCreateAgreementHandler(&mockAgreementRepo{}, &mockBus{})
	_, err := h.Handle(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error: validUntil before validFrom")
	}
}

func TestCreateAgreementHandler_SaveFails(t *testing.T) {
	cmd := baseCreateCmd()
	repo := &mockAgreementRepo{saveErr: errors.New("save error")}
	h := NewCreateAgreementHandler(repo, &mockBus{})
	_, err := h.Handle(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error when Save fails")
	}
}

func TestCreateAgreementHandler_SuccessPrivado(t *testing.T) {
	cmd := baseCreateCmd()
	cmd.ProviderType = "Privado"
	h := NewCreateAgreementHandler(&mockAgreementRepo{}, &mockBus{})
	id, err := h.Handle(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("expected valid agreement ID")
	}
}

func TestCreateAgreementHandler_SuccessNonPrivado(t *testing.T) {
	cmd := baseCreateCmd()
	h := NewCreateAgreementHandler(&mockAgreementRepo{}, &mockBus{})
	id, err := h.Handle(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("expected valid agreement ID")
	}
}

func TestCreateAgreementHandler_PublishError_DoesNotFail(t *testing.T) {
	cmd := baseCreateCmd()
	bus := &mockBus{publishErr: errors.New("bus error")}
	h := NewCreateAgreementHandler(&mockAgreementRepo{}, bus)
	_, err := h.Handle(context.Background(), cmd)
	if err != nil {
		t.Fatalf("publish error should not fail handler: %v", err)
	}
}

// ── AddPlanHandler ────────────────────────────────────────────────

func TestAddPlanHandler_FindByIDError(t *testing.T) {
	repo := &mockAgreementRepo{findByIDErr: errors.New("not found")}
	h := NewAddPlanHandler(repo, &mockBus{})
	_, err := h.Handle(context.Background(), AddPlanCommand{
		AgreementID: uuid.New(), PlanCode: "P002", PlanName: "Premium", CoPayType: "None",
	})
	if err == nil {
		t.Fatal("expected error when FindByID fails")
	}
}

func TestAddPlanHandler_InvalidCoPayType(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewAddPlanHandler(repo, &mockBus{})
	_, err := h.Handle(context.Background(), AddPlanCommand{
		AgreementID: a.ID(), PlanCode: "P002", PlanName: "Premium", CoPayType: "INVALID",
	})
	if err == nil {
		t.Fatal("expected error for invalid co_pay_type")
	}
}

func TestAddPlanHandler_NewPlanFails(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewAddPlanHandler(repo, &mockBus{})
	_, err := h.Handle(context.Background(), AddPlanCommand{
		AgreementID: a.ID(), PlanCode: "", PlanName: "Premium", CoPayType: "None",
	})
	if err == nil {
		t.Fatal("expected error for empty plan code")
	}
}

func TestAddPlanHandler_AddPlanFails_DuplicateCode(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewAddPlanHandler(repo, &mockBus{})
	_, err := h.Handle(context.Background(), AddPlanCommand{
		AgreementID: a.ID(), PlanCode: "P001", PlanName: "Dup", CoPayType: "None",
	})
	if err == nil {
		t.Fatal("expected error: duplicate plan code")
	}
}

func TestAddPlanHandler_UpdateFails(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a, updateErr: errors.New("update error")}
	h := NewAddPlanHandler(repo, &mockBus{})
	_, err := h.Handle(context.Background(), AddPlanCommand{
		AgreementID: a.ID(), PlanCode: "P002", PlanName: "Premium", CoPayType: "None",
	})
	if err == nil {
		t.Fatal("expected error when Update fails")
	}
}

func TestAddPlanHandler_Success(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewAddPlanHandler(repo, &mockBus{})
	id, err := h.Handle(context.Background(), AddPlanCommand{
		AgreementID: a.ID(), PlanCode: "P002", PlanName: "Premium", CoPayType: "None",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("expected valid plan ID")
	}
}

// ── UpsertProcedureRuleHandler ────────────────────────────────────

func TestUpsertProcedureRuleHandler_FindByIDError(t *testing.T) {
	repo := &mockAgreementRepo{findByIDErr: errors.New("not found")}
	h := NewUpsertProcedureRuleHandler(repo, &mockCoverageCache{}, &mockBus{})
	err := h.Handle(context.Background(), UpsertProcedureRuleCommand{AgreementID: uuid.New(), ProcedureCode: "PROC001"})
	if err == nil {
		t.Fatal("expected error when FindByID fails")
	}
}

func TestUpsertProcedureRuleHandler_PlanNotFound(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewUpsertProcedureRuleHandler(repo, &mockCoverageCache{}, &mockBus{})
	err := h.Handle(context.Background(), UpsertProcedureRuleCommand{
		AgreementID: a.ID(), PlanID: uuid.New(), ProcedureCode: "PROC001", CoveragePercent: 80,
	})
	if err == nil {
		t.Fatal("expected error: plan not found")
	}
}

func TestUpsertProcedureRuleHandler_UpdateFails(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	planID := a.Plans()[0].ID()
	repo := &mockAgreementRepo{findByIDResult: a, updateErr: errors.New("update error")}
	h := NewUpsertProcedureRuleHandler(repo, &mockCoverageCache{}, &mockBus{})
	err := h.Handle(context.Background(), UpsertProcedureRuleCommand{
		AgreementID: a.ID(), PlanID: planID, ProcedureCode: "PROC001", CoveragePercent: 80,
	})
	if err == nil {
		t.Fatal("expected error when Update fails")
	}
}

func TestUpsertProcedureRuleHandler_CacheInvalidateError_StillSucceeds(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	planID := a.Plans()[0].ID()
	repo := &mockAgreementRepo{findByIDResult: a}
	cache := &mockCoverageCache{invalidateErr: errors.New("cache error")}
	h := NewUpsertProcedureRuleHandler(repo, cache, &mockBus{})
	err := h.Handle(context.Background(), UpsertProcedureRuleCommand{
		AgreementID: a.ID(), PlanID: planID, ProcedureCode: "PROC001", CoveragePercent: 80,
	})
	if err != nil {
		t.Fatalf("cache invalidation error should not fail: %v", err)
	}
}

func TestUpsertProcedureRuleHandler_Success(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	planID := a.Plans()[0].ID()
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewUpsertProcedureRuleHandler(repo, &mockCoverageCache{}, &mockBus{})
	err := h.Handle(context.Background(), UpsertProcedureRuleCommand{
		AgreementID: a.ID(), PlanID: planID, ProcedureCode: "PROC001", CoveragePercent: 80,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── UpdateAgreementStatusHandler ──────────────────────────────────

func TestUpdateAgreementStatusHandler_FindByIDError(t *testing.T) {
	repo := &mockAgreementRepo{findByIDErr: errors.New("not found")}
	h := NewUpdateAgreementStatusHandler(repo, &mockBus{})
	err := h.Handle(context.Background(), UpdateAgreementStatusCommand{
		AgreementID: uuid.New(), NewStatus: "Suspended",
	})
	if err == nil {
		t.Fatal("expected error when FindByID fails")
	}
}

func TestUpdateAgreementStatusHandler_InvalidStatus(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewUpdateAgreementStatusHandler(repo, &mockBus{})
	err := h.Handle(context.Background(), UpdateAgreementStatusCommand{
		AgreementID: a.ID(), NewStatus: "INVALID",
	})
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestUpdateAgreementStatusHandler_SuspendFails_AlreadySuspended(t *testing.T) {
	a := suspendedAgreement()
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewUpdateAgreementStatusHandler(repo, &mockBus{})
	err := h.Handle(context.Background(), UpdateAgreementStatusCommand{
		AgreementID: a.ID(), NewStatus: "Suspended", Reason: "maintenance", UpdatedBy: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected error: already suspended")
	}
}

func TestUpdateAgreementStatusHandler_ActivateFails_AlreadyActive(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewUpdateAgreementStatusHandler(repo, &mockBus{})
	err := h.Handle(context.Background(), UpdateAgreementStatusCommand{
		AgreementID: a.ID(), NewStatus: "Active", UpdatedBy: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected error: already active")
	}
}

func TestUpdateAgreementStatusHandler_DefaultStatus_Expired(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewUpdateAgreementStatusHandler(repo, &mockBus{})
	err := h.Handle(context.Background(), UpdateAgreementStatusCommand{
		AgreementID: a.ID(), NewStatus: "Expired",
	})
	if err == nil {
		t.Fatal("expected error: Expired not allowed via this command")
	}
}

func TestUpdateAgreementStatusHandler_UpdateFails(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a, updateErr: errors.New("update error")}
	h := NewUpdateAgreementStatusHandler(repo, &mockBus{})
	err := h.Handle(context.Background(), UpdateAgreementStatusCommand{
		AgreementID: a.ID(), NewStatus: "Suspended", Reason: "maintenance", UpdatedBy: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected error when Update fails")
	}
}

func TestUpdateAgreementStatusHandler_SuccessSuspend(t *testing.T) {
	a := validActiveAgreement(valueobject.ProviderTypeObraSocial)
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewUpdateAgreementStatusHandler(repo, &mockBus{})
	err := h.Handle(context.Background(), UpdateAgreementStatusCommand{
		AgreementID: a.ID(), NewStatus: "Suspended", Reason: "maintenance", UpdatedBy: uuid.New(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateAgreementStatusHandler_SuccessActivate(t *testing.T) {
	a := suspendedAgreement()
	repo := &mockAgreementRepo{findByIDResult: a}
	h := NewUpdateAgreementStatusHandler(repo, &mockBus{})
	err := h.Handle(context.Background(), UpdateAgreementStatusCommand{
		AgreementID: a.ID(), NewStatus: "Active", UpdatedBy: uuid.New(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── RequestAuthorizationHandler ───────────────────────────────────

func TestRequestAuthorizationHandler_ServiceFails_Duplicate(t *testing.T) {
	existing := pendingAR()
	authRepo := &mockAuthorizationRepo{findPendingByPatientResult: existing}
	authSvc := service.NewAuthorizationService(authRepo)
	h := NewRequestAuthorizationHandler(authSvc, &mockBus{})
	_, err := h.Handle(context.Background(), RequestAuthorizationCommand{
		AgreementID:      uuid.New(),
		PlanID:           uuid.New(),
		PatientID:        uuid.New(),
		MembershipNumber: "MEM001",
		ProcedureCode:    "PROC001",
	})
	if err == nil {
		t.Fatal("expected error: duplicate authorization")
	}
}

func TestRequestAuthorizationHandler_Success(t *testing.T) {
	authRepo := &mockAuthorizationRepo{}
	authSvc := service.NewAuthorizationService(authRepo)
	h := NewRequestAuthorizationHandler(authSvc, &mockBus{})
	id, err := h.Handle(context.Background(), RequestAuthorizationCommand{
		AgreementID:      uuid.New(),
		PlanID:           uuid.New(),
		PatientID:        uuid.New(),
		MembershipNumber: "MEM001",
		ProcedureCode:    "PROC001",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("expected valid authorization ID")
	}
}

// ── ResolveAuthorizationHandler ───────────────────────────────────

func TestResolveAuthorizationHandler_InvalidStatus(t *testing.T) {
	authSvc := service.NewAuthorizationService(&mockAuthorizationRepo{})
	h := NewResolveAuthorizationHandler(authSvc, &mockBus{})
	err := h.Handle(context.Background(), ResolveAuthorizationCommand{
		AuthorizationID: uuid.New(),
		Status:          "INVALID",
	})
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestResolveAuthorizationHandler_ServiceFails(t *testing.T) {
	authRepo := &mockAuthorizationRepo{findByIDErr: errors.New("not found")}
	authSvc := service.NewAuthorizationService(authRepo)
	h := NewResolveAuthorizationHandler(authSvc, &mockBus{})
	err := h.Handle(context.Background(), ResolveAuthorizationCommand{
		AuthorizationID:   uuid.New(),
		Status:            "Approved",
		AuthorizationCode: "AUTH001",
		ResolvedBy:        uuid.New(),
	})
	if err == nil {
		t.Fatal("expected error when FindByID fails")
	}
}

func TestResolveAuthorizationHandler_SuccessApprove(t *testing.T) {
	ar := pendingAR()
	authRepo := &mockAuthorizationRepo{findByIDResult: ar}
	authSvc := service.NewAuthorizationService(authRepo)
	h := NewResolveAuthorizationHandler(authSvc, &mockBus{})
	err := h.Handle(context.Background(), ResolveAuthorizationCommand{
		AuthorizationID:   ar.ID(),
		Status:            "Approved",
		AuthorizationCode: "AUTH001",
		ResolvedBy:        uuid.New(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveAuthorizationHandler_SuccessReject(t *testing.T) {
	ar := pendingAR()
	authRepo := &mockAuthorizationRepo{findByIDResult: ar}
	authSvc := service.NewAuthorizationService(authRepo)
	h := NewResolveAuthorizationHandler(authSvc, &mockBus{})
	err := h.Handle(context.Background(), ResolveAuthorizationCommand{
		AuthorizationID: ar.ID(),
		Status:          "Rejected",
		RejectionReason: "No autorizado",
		ResolvedBy:      uuid.New(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
