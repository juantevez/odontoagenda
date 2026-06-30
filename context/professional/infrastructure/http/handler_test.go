package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	profqry "github.com/juantevez/odontoagenda/context/professional/application/query"
	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/repository"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	profhttp "github.com/juantevez/odontoagenda/context/professional/infrastructure/http"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

const (
	testSecret = "test-secret-key-0123456789abcdef"
	testIssuer = "odontoagenda-test"
)

// ── mock ──────────────────────────────────────────────────────────

type mockProfRepo struct {
	profs              map[sharedtypes.ProfessionalID]*aggregate.Professional
	findByIDErr        error
	findByClinicResult []*aggregate.Professional
	findByClinicErr    error
	searchResult       []*aggregate.Professional
	searchErr          error
	availableResult    []*aggregate.Professional
	availableErr       error
}

var _ repository.ProfessionalRepository = (*mockProfRepo)(nil)

func newMockProfRepo() *mockProfRepo {
	return &mockProfRepo{profs: make(map[sharedtypes.ProfessionalID]*aggregate.Professional)}
}

func (m *mockProfRepo) Save(_ context.Context, p *aggregate.Professional) error {
	m.profs[p.ID()] = p
	return nil
}
func (m *mockProfRepo) Update(_ context.Context, p *aggregate.Professional) error {
	m.profs[p.ID()] = p
	return nil
}
func (m *mockProfRepo) FindByID(_ context.Context, id sharedtypes.ProfessionalID) (*aggregate.Professional, error) {
	if m.findByIDErr != nil {
		return nil, m.findByIDErr
	}
	p, ok := m.profs[id]
	if !ok {
		return nil, sharederrors.NewNotFound("Professional", id.String())
	}
	return p, nil
}
func (m *mockProfRepo) FindByClinic(_ context.Context, _ sharedtypes.ClinicID, _ *valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	return m.findByClinicResult, m.findByClinicErr
}
func (m *mockProfRepo) FindBySpecialty(_ context.Context, _ valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	return nil, nil
}
func (m *mockProfRepo) FindAvailableAt(_ context.Context, _ sharedtypes.ClinicID, _ time.Time, _ *valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	return m.availableResult, m.availableErr
}
func (m *mockProfRepo) FindWithExpiringLicenses(_ context.Context, _ int) ([]*aggregate.Professional, error) {
	return nil, nil
}
func (m *mockProfRepo) Search(_ context.Context, _ sharedtypes.ClinicID, _ string) ([]*aggregate.Professional, error) {
	return m.searchResult, m.searchErr
}
func (m *mockProfRepo) ExistsByNationalID(_ context.Context, _ string) (bool, error) {
	return false, nil
}

// ── test server ───────────────────────────────────────────────────

type testServer struct {
	router chi.Router
	repo   *mockProfRepo
}

func newTestServer() *testServer {
	repo := newMockProfRepo()
	getByIDH := profqry.NewGetProfessionalByIDHandler(repo)
	findByClinicH := profqry.NewFindByClinicHandler(repo)
	availableAtH := profqry.NewFindAvailableAtHandler(repo)
	forSchedulingH := profqry.NewGetProfessionalForSchedulingHandler(repo)

	jwtCfg := middleware.JWTConfig{SecretKey: []byte(testSecret), Issuer: testIssuer}
	r := chi.NewRouter()
	profhttp.RegisterRoutes(r, jwtCfg,
		nil, nil, nil, nil, nil, nil, nil, // command handlers (no rutas activas en este handler)
		getByIDH, findByClinicH, availableAtH, forSchedulingH,
	)
	return &testServer{router: r, repo: repo}
}

func (s *testServer) do(t *testing.T, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// ── JWT helper ────────────────────────────────────────────────────

func makeToken(t *testing.T) string {
	t.Helper()
	userID := uuid.New()
	claims := &middleware.UserClaims{
		UserID: userID,
		Role:   middleware.RoleProfessional,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuer,
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("makeToken: %v", err)
	}
	return tok
}

// ── domain helper ─────────────────────────────────────────────────

func newTestProf(t *testing.T) *aggregate.Professional {
	t.Helper()
	name, _ := sharedvo.NewFullName("Dr. Juan Perez")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "12345678")
	email, _ := sharedvo.NewEmail("dr.perez@example.com")
	phone, _ := sharedvo.NewPhoneNumber("+5491112345678")
	p := aggregate.NewProfessional(nil, name, docID, email, phone, "Bio de prueba", nil)
	p.PendingEvents()
	return p
}

// ── GET /professionals ────────────────────────────────────────────

func TestFindByClinic(t *testing.T) {
	t.Run("200 con lista de profesionales", func(t *testing.T) {
		s := newTestServer()
		prof := newTestProf(t)
		s.repo.findByClinicResult = []*aggregate.Professional{prof}
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals?clinic_id="+uuid.New().String(), token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var result []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(result) != 1 {
			t.Errorf("len = %d, se esperaba 1", len(result))
		}
	})

	t.Run("200 con specialty param pasa el filtro al handler", func(t *testing.T) {
		s := newTestServer()
		prof := newTestProf(t)
		s.repo.findByClinicResult = []*aggregate.Professional{prof}
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals?clinic_id="+uuid.New().String()+"&specialty=GENERAL_DENTISTRY", token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("200 con q param usa Search en lugar de FindByClinic", func(t *testing.T) {
		s := newTestServer()
		prof := newTestProf(t)
		s.repo.searchResult = []*aggregate.Professional{prof}
		// findByClinicResult queda vacío; si el handler usara FindByClinic devolvería 0 resultados.
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals?clinic_id="+uuid.New().String()+"&q=Perez", token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var result []map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &result)
		if len(result) != 1 {
			t.Errorf("len = %d, se esperaba 1 (desde searchResult)", len(result))
		}
	})

	t.Run("200 sin clinic_id devuelve lista (clinic_id es uuid.Nil internamente)", func(t *testing.T) {
		s := newTestServer()
		s.repo.findByClinicResult = []*aggregate.Professional{}
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals", token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("400 con clinic_id inválido", func(t *testing.T) {
		s := newTestServer()
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals?clinic_id=no-es-uuid", token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != "INVALID_ARGUMENT" {
			t.Errorf("code = %q, se esperaba INVALID_ARGUMENT", body.Code)
		}
	})

	t.Run("401 sin Authorization header", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodGet, "/professionals?clinic_id="+uuid.New().String(), "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, se esperaba 401", rec.Code)
		}
	})

	t.Run("500 cuando FindByClinic falla con error genérico", func(t *testing.T) {
		s := newTestServer()
		s.repo.findByClinicErr = errors.New("db down")
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals?clinic_id="+uuid.New().String(), token)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, se esperaba 500", rec.Code)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != "INTERNAL" {
			t.Errorf("code = %q, se esperaba INTERNAL", body.Code)
		}
	})
}

// ── GET /professionals/{professionalId} ───────────────────────────

func TestGetByID(t *testing.T) {
	t.Run("200 con profesional existente", func(t *testing.T) {
		s := newTestServer()
		prof := newTestProf(t)
		_ = s.repo.Save(context.Background(), prof)
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals/"+prof.ID().String(), token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var body struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.ID != prof.ID().String() {
			t.Errorf("id = %q, se esperaba %q", body.ID, prof.ID().String())
		}
	})

	t.Run("400 con UUID inválido en el path", func(t *testing.T) {
		s := newTestServer()
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals/no-es-uuid", token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != "INVALID_ARGUMENT" {
			t.Errorf("code = %q, se esperaba INVALID_ARGUMENT", body.Code)
		}
	})

	t.Run("401 sin Authorization header", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodGet, "/professionals/"+uuid.New().String(), "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, se esperaba 401", rec.Code)
		}
	})

	t.Run("404 cuando el profesional no existe", func(t *testing.T) {
		s := newTestServer()
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals/"+uuid.New().String(), token)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, se esperaba 404", rec.Code)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != "NOT_FOUND" {
			t.Errorf("code = %q, se esperaba NOT_FOUND", body.Code)
		}
	})

	t.Run("500 cuando el repositorio falla con error genérico (cubre fallback de writeErrorFromDomain)", func(t *testing.T) {
		s := newTestServer()
		s.repo.findByIDErr = errors.New("connection reset by peer")
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals/"+uuid.New().String(), token)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, se esperaba 500", rec.Code)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != "INTERNAL" {
			t.Errorf("code = %q, se esperaba INTERNAL", body.Code)
		}
	})

	t.Run("respuesta incluye email y status del profesional", func(t *testing.T) {
		s := newTestServer()
		prof := newTestProf(t)
		_ = s.repo.Save(context.Background(), prof)
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals/"+prof.ID().String(), token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Email  string `json:"email"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Email != "dr.perez@example.com" {
			t.Errorf("email = %q", body.Email)
		}
		if body.Status == "" {
			t.Error("status no debería ser vacío")
		}
	})
}

// ── GET /professionals/{professionalId}/for-scheduling ────────────

func TestGetForScheduling(t *testing.T) {
	t.Run("200 con profesional existente", func(t *testing.T) {
		s := newTestServer()
		prof := newTestProf(t)
		_ = s.repo.Save(context.Background(), prof)
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals/"+prof.ID().String()+"/for-scheduling", token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var body struct {
			ProfessionalID string `json:"professional_id"`
			IsActive       bool   `json:"is_active"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.ProfessionalID != prof.ID().String() {
			t.Errorf("professional_id = %q, se esperaba %q", body.ProfessionalID, prof.ID().String())
		}
		if !body.IsActive {
			t.Error("is_active debería ser true")
		}
	})

	t.Run("400 con UUID inválido en el path", func(t *testing.T) {
		s := newTestServer()
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals/no-es-uuid/for-scheduling", token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != "INVALID_ARGUMENT" {
			t.Errorf("code = %q, se esperaba INVALID_ARGUMENT", body.Code)
		}
	})

	t.Run("401 sin Authorization header", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodGet, "/professionals/"+uuid.New().String()+"/for-scheduling", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, se esperaba 401", rec.Code)
		}
	})

	t.Run("404 cuando el profesional no existe", func(t *testing.T) {
		s := newTestServer()
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals/"+uuid.New().String()+"/for-scheduling", token)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, se esperaba 404", rec.Code)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != "NOT_FOUND" {
			t.Errorf("code = %q, se esperaba NOT_FOUND", body.Code)
		}
	})

	t.Run("500 cuando el repositorio falla con error genérico", func(t *testing.T) {
		s := newTestServer()
		s.repo.findByIDErr = errors.New("db timeout")
		token := makeToken(t)

		rec := s.do(t, http.MethodGet, "/professionals/"+uuid.New().String()+"/for-scheduling", token)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, se esperaba 500", rec.Code)
		}
	})
}
