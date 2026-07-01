package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	coveragecmd "github.com/juantevez/odontoagenda/context/coverage/application/command"
	coverageqry "github.com/juantevez/odontoagenda/context/coverage/application/query"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/repository"
	"github.com/juantevez/odontoagenda/context/coverage/domain/service"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	coveragehttp "github.com/juantevez/odontoagenda/context/coverage/infrastructure/http"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── constants ─────────────────────────────────────────────────────

const (
	testSecret = "test-secret-key-for-coverage-handler-tests"
	testIssuer = "odontoagenda.test"
)

// ── JWT helpers ───────────────────────────────────────────────────

func bearerToken(t *testing.T, role middleware.Role) string {
	t.Helper()
	claims := &middleware.UserClaims{
		UserID: uuid.New(),
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign test JWT: %v", err)
	}
	return "Bearer " + tok
}

func adminToken(t *testing.T) string  { return bearerToken(t, middleware.RoleClinicAdmin) }
func superToken(t *testing.T) string  { return bearerToken(t, middleware.RoleSuperAdmin) }
func patientToken(t *testing.T) string { return bearerToken(t, middleware.RolePatient) }

// ── mocks ─────────────────────────────────────────────────────────

type mockAgrRepo struct {
	existsResult bool
	existsErr    error
	saveErr      error
	updateErr    error
	findByIDRes  *aggregate.Agreement
	findByIDErr  error
	findActiveRes sharedtypes.PagedResult[*aggregate.Agreement]
	findActiveErr error
}

func (m *mockAgrRepo) Save(_ context.Context, _ *aggregate.Agreement) error { return m.saveErr }
func (m *mockAgrRepo) Update(_ context.Context, _ *aggregate.Agreement) error { return m.updateErr }
func (m *mockAgrRepo) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.Agreement, error) {
	return m.findByIDRes, m.findByIDErr
}
func (m *mockAgrRepo) FindByCode(_ context.Context, _ string) (*aggregate.Agreement, error) {
	return m.findByIDRes, m.findByIDErr
}
func (m *mockAgrRepo) FindActive(_ context.Context, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Agreement], error) {
	return m.findActiveRes, m.findActiveErr
}
func (m *mockAgrRepo) FindByProviderType(_ context.Context, _ valueobject.ProviderType, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Agreement], error) {
	return m.findActiveRes, m.findActiveErr
}
func (m *mockAgrRepo) ExistsByCode(_ context.Context, _ string) (bool, error) {
	return m.existsResult, m.existsErr
}
func (m *mockAgrRepo) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.Agreement, error) {
	return nil, nil
}

type mockAuthorizationRepo struct {
	saveErr                      error
	updateErr                    error
	findByIDRes                  *aggregate.AuthorizationRequest
	findByIDErr                  error
	findPendingByPatientRes      *aggregate.AuthorizationRequest
	findPendingByPatientErr      error
	findPendingByAgreementRes    []*aggregate.AuthorizationRequest
	findPendingByAgreementErr    error
	findExpiredRes               []*aggregate.AuthorizationRequest
	findExpiredErr               error
}

func (m *mockAuthorizationRepo) Save(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return m.saveErr
}
func (m *mockAuthorizationRepo) Update(_ context.Context, _ *aggregate.AuthorizationRequest) error {
	return m.updateErr
}
func (m *mockAuthorizationRepo) FindByID(_ context.Context, _ uuid.UUID) (*aggregate.AuthorizationRequest, error) {
	return m.findByIDRes, m.findByIDErr
}
func (m *mockAuthorizationRepo) FindPendingByAgreement(_ context.Context, _ uuid.UUID) ([]*aggregate.AuthorizationRequest, error) {
	return m.findPendingByAgreementRes, m.findPendingByAgreementErr
}
func (m *mockAuthorizationRepo) FindPendingByPatient(_ context.Context, _ uuid.UUID, _ string) (*aggregate.AuthorizationRequest, error) {
	return m.findPendingByPatientRes, m.findPendingByPatientErr
}
func (m *mockAuthorizationRepo) FindExpired(_ context.Context, _ time.Time) ([]*aggregate.AuthorizationRequest, error) {
	return m.findExpiredRes, m.findExpiredErr
}

type mockAffRepo struct {
	findActiveRes *repository.PatientAffiliation
	findActiveErr error
}

func (m *mockAffRepo) Upsert(_ context.Context, _ repository.PatientAffiliation) error { return nil }
func (m *mockAffRepo) FindActive(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*repository.PatientAffiliation, error) {
	return m.findActiveRes, m.findActiveErr
}
func (m *mockAffRepo) SuspendByPatient(_ context.Context, _ uuid.UUID) error { return nil }

type mockCache struct {
	getRes *valueobject.CoverageResult
	getErr error
}

func (m *mockCache) GetCoverageResult(_ context.Context, _ uuid.UUID, _ string) (*valueobject.CoverageResult, error) {
	return m.getRes, m.getErr
}
func (m *mockCache) SetCoverageResult(_ context.Context, _ uuid.UUID, _ string, _ valueobject.CoverageResult) error {
	return nil
}
func (m *mockCache) InvalidatePlan(_ context.Context, _ uuid.UUID) error { return nil }

type mockBus struct{}

func (m *mockBus) Publish(_ context.Context, _ events.DomainEvent) error   { return nil }
func (m *mockBus) Subscribe(_ context.Context, _ events.SubscribeOptions, _ events.Handler) error {
	return nil
}
func (m *mockBus) Close() error { return nil }

// ── test env ──────────────────────────────────────────────────────

type testEnv struct {
	router   http.Handler
	agrRepo  *mockAgrRepo
	authRepo *mockAuthorizationRepo
	affRepo  *mockAffRepo
	cache    *mockCache
}

func newTestEnv() *testEnv {
	e := &testEnv{
		agrRepo:  &mockAgrRepo{},
		authRepo: &mockAuthorizationRepo{},
		affRepo:  &mockAffRepo{},
		cache:    &mockCache{},
	}
	bus := &mockBus{}

	authSvc := service.NewAuthorizationService(e.authRepo)
	calc    := service.NewCoverageCalculator(e.affRepo)
	verif   := service.NewAffiliationVerifier(e.agrRepo, e.affRepo)

	r := chi.NewRouter()
	jwtCfg := middleware.JWTConfig{SecretKey: []byte(testSecret), Issuer: testIssuer}

	coveragehttp.RegisterRoutes(r, jwtCfg,
		coveragecmd.NewCreateAgreementHandler(e.agrRepo, bus),
		coveragecmd.NewAddPlanHandler(e.agrRepo, bus),
		coveragecmd.NewUpsertProcedureRuleHandler(e.agrRepo, e.cache, bus),
		coveragecmd.NewUpdateAgreementStatusHandler(e.agrRepo, bus),
		coveragecmd.NewRequestAuthorizationHandler(authSvc, bus),
		coveragecmd.NewResolveAuthorizationHandler(authSvc, bus),
		coverageqry.NewGetAgreementHandler(e.agrRepo),
		coverageqry.NewListAgreementsHandler(e.agrRepo),
		coverageqry.NewCalculateCoverageHandler(e.agrRepo, calc, e.cache),
		coverageqry.NewVerifyAffiliationHandler(verif),
		coverageqry.NewGetAuthorizationHandler(e.authRepo),
		coverageqry.NewListPendingAuthorizationsHandler(e.authRepo),
	)
	e.router = r
	return e
}

func (e *testEnv) do(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, r)
	return w
}

// ── domain data helpers ───────────────────────────────────────────

func testEmail() sharedvo.Email   { e, _ := sharedvo.NewEmail("provider@clinic.com"); return e }
func testPhone() sharedvo.PhoneNumber { p, _ := sharedvo.NewPhoneNumber("+54911000001"); return p }

func privadoAgreement() *aggregate.Agreement {
	return aggregate.Reconstitute(
		uuid.New(), "AGR001", "Provider Privado",
		valueobject.ProviderTypePrivado, valueobject.AgreementStatusActive,
		time.Now().AddDate(-1, 0, 0), nil,
		testEmail(), testPhone(), 0, nil,
		time.Now(), time.Now(), nil, 1,
	)
}

func activeObraSocialAgreement(planID uuid.UUID) *aggregate.Agreement {
	plan := aggregate.ReconstitutePlan(
		planID, "P001", "Plan Básico",
		valueobject.CoPayTypeNone, 0, false, nil,
		[]aggregate.ProcedureRule{},
		valueobject.PlanStatusActive,
		time.Now(), time.Now(),
	)
	return aggregate.Reconstitute(
		uuid.New(), "AGR002", "Obra Social Test",
		valueobject.ProviderTypeObraSocial, valueobject.AgreementStatusActive,
		time.Now().AddDate(-1, 0, 0), nil,
		testEmail(), testPhone(), 0,
		[]aggregate.Plan{plan},
		time.Now(), time.Now(), nil, 1,
	)
}

func pendingAR() *aggregate.AuthorizationRequest {
	deadline := time.Now().Add(48 * time.Hour)
	ar, _ := aggregate.NewAuthorizationRequest(
		uuid.New(), uuid.New(), uuid.New(),
		"MEM001", "PROC001", nil, &deadline,
	)
	ar.PendingEvents()
	return ar
}

// ── request helpers ───────────────────────────────────────────────

func jsonBody(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

// ── POST /agreements ──────────────────────────────────────────────

func TestCreateAgreement_NoJWT(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("POST", "/agreements", nil)
	w := e.do(r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestCreateAgreement_ForbiddenRole(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("POST", "/agreements", strings.NewReader("{}"))
	r.Header.Set("Authorization", patientToken(t))
	w := e.do(r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestCreateAgreement_BadBody(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("POST", "/agreements", strings.NewReader("not-json"))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateAgreement_InvalidValidFrom(t *testing.T) {
	e := newTestEnv()
	body := jsonBody(map[string]string{"valid_from": "not-a-date"})
	r := httptest.NewRequest("POST", "/agreements", body)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateAgreement_InvalidValidUntil(t *testing.T) {
	e := newTestEnv()
	body := jsonBody(map[string]any{
		"valid_from":  "2025-01-01",
		"valid_until": "bad-date",
	})
	r := httptest.NewRequest("POST", "/agreements", body)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateAgreement_HandlerDomainError(t *testing.T) {
	e := newTestEnv()
	e.agrRepo.existsErr = errors.New("db error") // triggers internal error
	body := jsonBody(map[string]any{
		"agreement_code": "X001",
		"provider_name":  "Test",
		"provider_type":  "Privado",
		"valid_from":     "2025-01-01",
		"contact_email":  "x@x.com",
		"contact_phone":  "+54911000001",
	})
	r := httptest.NewRequest("POST", "/agreements", body)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	// generic error → 500
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreateAgreement_Success(t *testing.T) {
	e := newTestEnv()
	// existsByCode → false, save → nil
	body := jsonBody(map[string]any{
		"agreement_code": "P001",
		"provider_name":  "Provider",
		"provider_type":  "Privado",
		"valid_from":     "2025-01-01",
		"valid_until":    "2027-12-31",
		"contact_email":  "prov@prov.com",
		"contact_phone":  "+54911000001",
	})
	r := httptest.NewRequest("POST", "/agreements", body)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["agreement_id"] == "" {
		t.Fatal("expected agreement_id in response")
	}
}

// ── GET /agreements ───────────────────────────────────────────────

func TestListAgreements_HandlerError(t *testing.T) {
	e := newTestEnv()
	e.agrRepo.findActiveErr = sharederrors.NewNotFound("Agreement", "list")
	r := httptest.NewRequest("GET", "/agreements", nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestListAgreements_Success(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("GET", "/agreements?limit=10&offset=0", nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── GET /agreements/{agreementId} ─────────────────────────────────

func TestGetAgreement_BadUUID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("GET", "/agreements/not-a-uuid", nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetAgreement_DomainError(t *testing.T) {
	e := newTestEnv()
	e.agrRepo.findByIDErr = sharederrors.NewNotFound("Agreement", "x")
	id := uuid.New()
	r := httptest.NewRequest("GET", "/agreements/"+id.String(), nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetAgreement_GenericError_Returns500(t *testing.T) {
	e := newTestEnv()
	e.agrRepo.findByIDErr = errors.New("db connection lost")
	id := uuid.New()
	r := httptest.NewRequest("GET", "/agreements/"+id.String(), nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetAgreement_Success(t *testing.T) {
	e := newTestEnv()
	a := privadoAgreement()
	e.agrRepo.findByIDRes = a
	r := httptest.NewRequest("GET", "/agreements/"+a.ID().String(), nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── PATCH /agreements/{agreementId}/status ────────────────────────

func TestUpdateAgreementStatus_ForbiddenRole(t *testing.T) {
	e := newTestEnv()
	id := uuid.New()
	r := httptest.NewRequest("PATCH", "/agreements/"+id.String()+"/status",
		jsonBody(map[string]string{"status": "Suspended"}))
	r.Header.Set("Authorization", adminToken(t)) // clinic admin, not superadmin
	w := e.do(r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestUpdateAgreementStatus_BadUUID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("PATCH", "/agreements/not-uuid/status",
		jsonBody(map[string]string{"status": "Suspended"}))
	r.Header.Set("Authorization", superToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateAgreementStatus_BadBody(t *testing.T) {
	e := newTestEnv()
	id := uuid.New()
	r := httptest.NewRequest("PATCH", "/agreements/"+id.String()+"/status",
		strings.NewReader("not-json"))
	r.Header.Set("Authorization", superToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateAgreementStatus_HandlerError(t *testing.T) {
	e := newTestEnv()
	e.agrRepo.findByIDErr = sharederrors.NewNotFound("Agreement", "x")
	id := uuid.New()
	r := httptest.NewRequest("PATCH", "/agreements/"+id.String()+"/status",
		jsonBody(map[string]string{"status": "Suspended", "reason": "cierre"}))
	r.Header.Set("Authorization", superToken(t))
	w := e.do(r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateAgreementStatus_Success(t *testing.T) {
	e := newTestEnv()
	a := activeObraSocialAgreement(uuid.New())
	e.agrRepo.findByIDRes = a
	r := httptest.NewRequest("PATCH", "/agreements/"+a.ID().String()+"/status",
		jsonBody(map[string]string{"status": "Suspended", "reason": "test suspension"}))
	r.Header.Set("Authorization", superToken(t))
	w := e.do(r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// ── POST /agreements/{agreementId}/plans ──────────────────────────

func TestAddPlan_BadAgreementID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("POST", "/agreements/not-uuid/plans",
		jsonBody(map[string]string{"plan_code": "P1", "plan_name": "Plan 1", "co_pay_type": "None"}))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAddPlan_BadBody(t *testing.T) {
	e := newTestEnv()
	id := uuid.New()
	r := httptest.NewRequest("POST", "/agreements/"+id.String()+"/plans",
		strings.NewReader("not-json"))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAddPlan_HandlerError(t *testing.T) {
	e := newTestEnv()
	e.agrRepo.findByIDErr = sharederrors.NewNotFound("Agreement", "x")
	id := uuid.New()
	r := httptest.NewRequest("POST", "/agreements/"+id.String()+"/plans",
		jsonBody(map[string]any{"plan_code": "P1", "plan_name": "Plan", "co_pay_type": "None"}))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAddPlan_Success(t *testing.T) {
	e := newTestEnv()
	a := activeObraSocialAgreement(uuid.New())
	e.agrRepo.findByIDRes = a
	r := httptest.NewRequest("POST", "/agreements/"+a.ID().String()+"/plans",
		jsonBody(map[string]any{
			"plan_code":   "P-NEW",
			"plan_name":   "Nuevo Plan",
			"co_pay_type": "None",
		}))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["plan_id"] == "" {
		t.Fatal("expected plan_id in response")
	}
}

// ── PUT /agreements/{agreementId}/plans/{planId}/procedures ────────

func TestUpsertProcedureRule_BadAgreementID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("PUT", "/agreements/bad/plans/"+uuid.New().String()+"/procedures",
		jsonBody(map[string]any{"procedure_code": "P1", "coverage_percent": 80}))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpsertProcedureRule_BadPlanID(t *testing.T) {
	e := newTestEnv()
	id := uuid.New()
	r := httptest.NewRequest("PUT", "/agreements/"+id.String()+"/plans/bad-uuid/procedures",
		jsonBody(map[string]any{"procedure_code": "P1", "coverage_percent": 80}))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpsertProcedureRule_BadBody(t *testing.T) {
	e := newTestEnv()
	id := uuid.New()
	pid := uuid.New()
	r := httptest.NewRequest("PUT", "/agreements/"+id.String()+"/plans/"+pid.String()+"/procedures",
		strings.NewReader("not-json"))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpsertProcedureRule_HandlerError(t *testing.T) {
	e := newTestEnv()
	e.agrRepo.findByIDErr = sharederrors.NewNotFound("Agreement", "x")
	id, pid := uuid.New(), uuid.New()
	r := httptest.NewRequest("PUT", "/agreements/"+id.String()+"/plans/"+pid.String()+"/procedures",
		jsonBody(map[string]any{"procedure_code": "PROC001", "coverage_percent": 80}))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpsertProcedureRule_Success(t *testing.T) {
	e := newTestEnv()
	planID := uuid.New()
	a := activeObraSocialAgreement(planID)
	e.agrRepo.findByIDRes = a
	id := a.ID()
	r := httptest.NewRequest("PUT", "/agreements/"+id.String()+"/plans/"+planID.String()+"/procedures",
		jsonBody(map[string]any{
			"procedure_code":   "PROC001",
			"coverage_percent": 80,
		}))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// ── GET /coverage/calculate ───────────────────────────────────────

func TestCalculateCoverage_BadAgreementID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("GET", "/coverage/calculate?agreement_id=bad", nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCalculateCoverage_BadPlanID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("GET", "/coverage/calculate?agreement_id="+uuid.New().String()+"&plan_id=bad", nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCalculateCoverage_BadPatientID(t *testing.T) {
	e := newTestEnv()
	q := "/coverage/calculate?agreement_id=" + uuid.New().String() +
		"&plan_id=" + uuid.New().String() + "&patient_id=bad"
	r := httptest.NewRequest("GET", q, nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCalculateCoverage_HandlerError(t *testing.T) {
	e := newTestEnv()
	e.agrRepo.findByIDErr = sharederrors.NewNotFound("Agreement", "x")
	q := "/coverage/calculate?agreement_id=" + uuid.New().String() +
		"&plan_id=" + uuid.New().String() + "&patient_id=" + uuid.New().String()
	r := httptest.NewRequest("GET", q, nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCalculateCoverage_Success_WithAppointmentDate(t *testing.T) {
	e := newTestEnv()
	a := privadoAgreement()
	e.agrRepo.findByIDRes = a
	// appointment_date provided and valid → covers the inner date-parse success branch
	q := "/coverage/calculate?agreement_id=" + a.ID().String() +
		"&plan_id=" + uuid.New().String() +
		"&patient_id=" + uuid.New().String() +
		"&appointment_date=2025-06-15"
	r := httptest.NewRequest("GET", q, nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCalculateCoverage_Success_NoAppointmentDate(t *testing.T) {
	e := newTestEnv()
	a := privadoAgreement()
	e.agrRepo.findByIDRes = a
	q := "/coverage/calculate?agreement_id=" + a.ID().String() +
		"&plan_id=" + uuid.New().String() +
		"&patient_id=" + uuid.New().String()
	r := httptest.NewRequest("GET", q, nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── GET /coverage/verify-affiliation ─────────────────────────────

func TestVerifyAffiliation_BadAgreementID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("GET", "/coverage/verify-affiliation?agreement_id=bad", nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestVerifyAffiliation_BadPlanID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("GET", "/coverage/verify-affiliation?agreement_id="+uuid.New().String()+"&plan_id=bad", nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestVerifyAffiliation_BadPatientID(t *testing.T) {
	e := newTestEnv()
	q := "/coverage/verify-affiliation?agreement_id=" + uuid.New().String() +
		"&plan_id=" + uuid.New().String() + "&patient_id=bad"
	r := httptest.NewRequest("GET", q, nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestVerifyAffiliation_Success_WithAppointmentDate(t *testing.T) {
	e := newTestEnv()
	// Verifier uses agrRepo.FindByID; error → Cancelled → still HTTP 200
	e.agrRepo.findByIDErr = errors.New("not found")
	q := "/coverage/verify-affiliation?agreement_id=" + uuid.New().String() +
		"&plan_id=" + uuid.New().String() +
		"&patient_id=" + uuid.New().String() +
		"&appointment_date=2025-06-15"
	r := httptest.NewRequest("GET", q, nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyAffiliation_Success_NoAppointmentDate(t *testing.T) {
	e := newTestEnv()
	e.agrRepo.findByIDErr = errors.New("not found")
	q := "/coverage/verify-affiliation?agreement_id=" + uuid.New().String() +
		"&plan_id=" + uuid.New().String() +
		"&patient_id=" + uuid.New().String()
	r := httptest.NewRequest("GET", q, nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── POST /authorizations ──────────────────────────────────────────

func TestRequestAuthorization_BadBody(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("POST", "/authorizations", strings.NewReader("bad"))
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRequestAuthorization_BadAgreementID(t *testing.T) {
	e := newTestEnv()
	body := jsonBody(map[string]string{"agreement_id": "bad", "plan_id": uuid.New().String(), "patient_id": uuid.New().String()})
	r := httptest.NewRequest("POST", "/authorizations", body)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRequestAuthorization_BadPlanID(t *testing.T) {
	e := newTestEnv()
	body := jsonBody(map[string]string{"agreement_id": uuid.New().String(), "plan_id": "bad", "patient_id": uuid.New().String()})
	r := httptest.NewRequest("POST", "/authorizations", body)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRequestAuthorization_BadPatientID(t *testing.T) {
	e := newTestEnv()
	body := jsonBody(map[string]string{
		"agreement_id": uuid.New().String(),
		"plan_id":      uuid.New().String(),
		"patient_id":   "bad",
	})
	r := httptest.NewRequest("POST", "/authorizations", body)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRequestAuthorization_InvalidAppointmentID_Ignored(t *testing.T) {
	e := newTestEnv()
	// Invalid appointment_id is silently ignored; handler proceeds normally
	apptID := "not-a-uuid"
	body := jsonBody(map[string]any{
		"agreement_id":      uuid.New().String(),
		"plan_id":           uuid.New().String(),
		"patient_id":        uuid.New().String(),
		"membership_number": "MEM001",
		"procedure_code":    "PROC001",
		"appointment_id":    apptID,
	})
	r := httptest.NewRequest("POST", "/authorizations", body)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	// handler continues with nil AppointmentID → success
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 (invalid appt_id silently ignored), got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequestAuthorization_WithValidAppointmentID(t *testing.T) {
	e := newTestEnv()
	apptID := uuid.New().String()
	body := jsonBody(map[string]any{
		"agreement_id":      uuid.New().String(),
		"plan_id":           uuid.New().String(),
		"patient_id":        uuid.New().String(),
		"membership_number": "MEM001",
		"procedure_code":    "PROC001",
		"appointment_id":    apptID,
	})
	r := httptest.NewRequest("POST", "/authorizations", body)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["authorization_id"] == "" {
		t.Fatal("expected authorization_id in response")
	}
}

func TestRequestAuthorization_HandlerError(t *testing.T) {
	e := newTestEnv()
	e.authRepo.saveErr = sharederrors.NewConflict("duplicate authorization", nil)
	body := jsonBody(map[string]any{
		"agreement_id":      uuid.New().String(),
		"plan_id":           uuid.New().String(),
		"patient_id":        uuid.New().String(),
		"membership_number": "MEM001",
		"procedure_code":    "PROC001",
	})
	r := httptest.NewRequest("POST", "/authorizations", body)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

// ── GET /authorizations/{authorizationId} ─────────────────────────

func TestGetAuthorization_BadUUID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("GET", "/authorizations/not-a-uuid", nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetAuthorization_HandlerError(t *testing.T) {
	e := newTestEnv()
	e.authRepo.findByIDErr = sharederrors.NewNotFound("Authorization", "x")
	id := uuid.New()
	r := httptest.NewRequest("GET", "/authorizations/"+id.String(), nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetAuthorization_Success(t *testing.T) {
	e := newTestEnv()
	ar := pendingAR()
	e.authRepo.findByIDRes = ar
	r := httptest.NewRequest("GET", "/authorizations/"+ar.ID().String(), nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── GET /authorizations/pending ───────────────────────────────────

func TestListPendingAuthorizations_BadAgreementID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("GET", "/authorizations/pending?agreement_id=bad", nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListPendingAuthorizations_HandlerError(t *testing.T) {
	e := newTestEnv()
	e.authRepo.findPendingByAgreementErr = sharederrors.NewNotFound("Agreement", "x")
	r := httptest.NewRequest("GET", "/authorizations/pending?agreement_id="+uuid.New().String(), nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestListPendingAuthorizations_Success(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("GET", "/authorizations/pending?agreement_id="+uuid.New().String(), nil)
	r.Header.Set("Authorization", adminToken(t))
	w := e.do(r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── PATCH /authorizations/{authorizationId}/resolve ───────────────

func TestResolveAuthorization_NoJWT(t *testing.T) {
	e := newTestEnv()
	id := uuid.New()
	r := httptest.NewRequest("PATCH", "/authorizations/"+id.String()+"/resolve", nil)
	// No auth header — JWT middleware returns 401
	w := e.do(r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestResolveAuthorization_BadUUID(t *testing.T) {
	e := newTestEnv()
	r := httptest.NewRequest("PATCH", "/authorizations/bad-uuid/resolve",
		jsonBody(map[string]string{"status": "Approved", "authorization_code": "CODE"}))
	r.Header.Set("Authorization", superToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestResolveAuthorization_BadBody(t *testing.T) {
	e := newTestEnv()
	id := uuid.New()
	r := httptest.NewRequest("PATCH", "/authorizations/"+id.String()+"/resolve",
		strings.NewReader("bad"))
	r.Header.Set("Authorization", superToken(t))
	w := e.do(r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestResolveAuthorization_HandlerError(t *testing.T) {
	e := newTestEnv()
	e.authRepo.findByIDErr = sharederrors.NewNotFound("Authorization", "x")
	id := uuid.New()
	r := httptest.NewRequest("PATCH", "/authorizations/"+id.String()+"/resolve",
		jsonBody(map[string]string{"status": "Approved", "authorization_code": "CODE"}))
	r.Header.Set("Authorization", superToken(t))
	w := e.do(r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestResolveAuthorization_Success(t *testing.T) {
	e := newTestEnv()
	ar := pendingAR()
	e.authRepo.findByIDRes = ar
	r := httptest.NewRequest("PATCH", "/authorizations/"+ar.ID().String()+"/resolve",
		jsonBody(map[string]string{"status": "Approved", "authorization_code": "AUTH-9999"}))
	r.Header.Set("Authorization", superToken(t))
	w := e.do(r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}
