package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/iam/application/command"
	"github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/iam/domain/repository"
	"github.com/juantevez/odontoagenda/context/iam/domain/service"
	iamhttp "github.com/juantevez/odontoagenda/context/iam/infrastructure/http"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── mocks (mismo patrón usado en application/command/handlers_test.go) ──

type mockUserRepo struct {
	users   map[sharedtypes.UserID]*aggregate.User
	byEmail map[string]*aggregate.User

	findByIDErr error
}

var _ repository.UserRepository = (*mockUserRepo)(nil)

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:   make(map[sharedtypes.UserID]*aggregate.User),
		byEmail: make(map[string]*aggregate.User),
	}
}

func (m *mockUserRepo) Save(_ context.Context, user *aggregate.User) error {
	m.users[user.ID()] = user
	m.byEmail[user.Email().String()] = user
	return nil
}

func (m *mockUserRepo) Update(_ context.Context, user *aggregate.User) error {
	m.users[user.ID()] = user
	m.byEmail[user.Email().String()] = user
	return nil
}

func (m *mockUserRepo) FindByID(_ context.Context, id sharedtypes.UserID) (*aggregate.User, error) {
	if m.findByIDErr != nil {
		return nil, m.findByIDErr
	}
	u, ok := m.users[id]
	if !ok {
		return nil, sharederrors.NewNotFound("User", id.String())
	}
	return u, nil
}

func (m *mockUserRepo) FindByEmail(_ context.Context, email sharedvo.Email) (*aggregate.User, error) {
	u, ok := m.byEmail[email.String()]
	if !ok {
		return nil, sharederrors.NewNotFound("User", email.String())
	}
	return u, nil
}

func (m *mockUserRepo) ExistsByEmail(_ context.Context, email sharedvo.Email) (bool, error) {
	_, ok := m.byEmail[email.String()]
	return ok, nil
}

type mockFamilyRepo struct {
	families  map[sharedtypes.FamilyID]*aggregate.FamilyAccount
	byPatient map[sharedtypes.PatientID]*aggregate.FamilyAccount
}

var _ repository.FamilyRepository = (*mockFamilyRepo)(nil)

func newMockFamilyRepo() *mockFamilyRepo {
	return &mockFamilyRepo{
		families:  make(map[sharedtypes.FamilyID]*aggregate.FamilyAccount),
		byPatient: make(map[sharedtypes.PatientID]*aggregate.FamilyAccount),
	}
}

func (m *mockFamilyRepo) Save(_ context.Context, family *aggregate.FamilyAccount) error {
	m.families[family.ID()] = family
	m.byPatient[family.PrimaryAdultID()] = family
	return nil
}

func (m *mockFamilyRepo) Update(_ context.Context, family *aggregate.FamilyAccount) error {
	m.families[family.ID()] = family
	return nil
}

func (m *mockFamilyRepo) FindByID(_ context.Context, id sharedtypes.FamilyID) (*aggregate.FamilyAccount, error) {
	f, ok := m.families[id]
	if !ok {
		return nil, sharederrors.NewNotFound("FamilyAccount", id.String())
	}
	return f, nil
}

func (m *mockFamilyRepo) FindByPatientID(_ context.Context, patientID sharedtypes.PatientID) (*aggregate.FamilyAccount, error) {
	f, ok := m.byPatient[patientID]
	if !ok {
		return nil, sharederrors.NewNotFound("FamilyAccount", patientID.String())
	}
	return f, nil
}

type noopEventBus struct{}

var _ events.Bus = (*noopEventBus)(nil)

func (noopEventBus) Publish(_ context.Context, _ events.DomainEvent) error { return nil }
func (noopEventBus) Subscribe(_ context.Context, _ events.SubscribeOptions, _ events.Handler) error {
	return nil
}
func (noopEventBus) Close() error { return nil }

// ── test harness ──────────────────────────────────────────────────

const (
	testSecret         = "test-secret-key-0123456789abcdef"
	testIssuer         = "odontoagenda-test"
	validPlainPassword = "Sup3rSecret"
)

// testServer agrupa el router montado y sus repos, para que los tests
// puedan manipular el estado del repositorio después de la construcción.
type testServer struct {
	router   chi.Router
	userRepo *mockUserRepo
}

func newTestServer() *testServer {
	userRepo := newMockUserRepo()
	familyRepo := newMockFamilyRepo()
	bus := noopEventBus{}
	tokenService := service.NewTokenService(service.DefaultTokenConfig([]byte(testSecret), testIssuer))

	registerHandler := command.NewRegisterUserHandler(userRepo, familyRepo, bus)
	loginHandler := command.NewLoginHandler(userRepo, familyRepo, tokenService)
	refreshHandler := command.NewRefreshTokensHandler(userRepo, familyRepo, tokenService)
	logoutHandler := command.NewLogoutHandler(userRepo, bus)

	jwtCfg := middleware.JWTConfig{SecretKey: []byte(testSecret), Issuer: testIssuer}

	r := chi.NewRouter()
	iamhttp.RegisterRoutes(r, jwtCfg, registerHandler, loginHandler, refreshHandler, logoutHandler)

	return &testServer{router: r, userRepo: userRepo}
}

func (s *testServer) do(t *testing.T, method, path string, body any, bearer string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("setup: marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// registerAndLogin crea un usuario vía HTTP y devuelve su access token,
// listo para usarse en rutas protegidas.
func (s *testServer) registerAndLogin(t *testing.T, email, role string) (accessToken string) {
	t.Helper()

	rec := s.do(t, http.MethodPost, "/auth/register", map[string]string{
		"email":    email,
		"password": validPlainPassword,
		"role":     role,
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: register status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = s.do(t, http.MethodPost, "/auth/login", map[string]string{
		"email":     email,
		"password":  validPlainPassword,
		"device_id": "test-device",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: login status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var loginResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("setup: decode login response: %v", err)
	}
	return loginResp.AccessToken
}

// ── POST /auth/register ──────────────────────────────────────────

func TestRegister(t *testing.T) {
	t.Run("201 con datos válidos", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/auth/register", map[string]string{
			"email":    "nuevo@example.com",
			"password": validPlainPassword,
			"role":     "profesional",
		}, "")

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			UserID string `json:"user_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if _, err := uuid.Parse(resp.UserID); err != nil {
			t.Errorf("user_id %q no es un UUID válido", resp.UserID)
		}
	})

	t.Run("400 con body inválido", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader([]byte("{invalid json")))
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("400 con rol inválido", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/auth/register", map[string]string{
			"email":    "rolinvalido@example.com",
			"password": validPlainPassword,
			"role":     "rol-que-no-existe",
		}, "")

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body = %s, se esperaba %d", rec.Code, rec.Body.String(), http.StatusBadRequest)
		}
	})

	t.Run("409 con email duplicado", func(t *testing.T) {
		s := newTestServer()
		body := map[string]string{
			"email":    "duplicado@example.com",
			"password": validPlainPassword,
			"role":     "profesional",
		}
		first := s.do(t, http.MethodPost, "/auth/register", body, "")
		if first.Code != http.StatusCreated {
			t.Fatalf("setup: status = %d, body = %s", first.Code, first.Body.String())
		}

		second := s.do(t, http.MethodPost, "/auth/register", body, "")
		if second.Code != http.StatusConflict {
			t.Fatalf("status = %d, body = %s, se esperaba %d", second.Code, second.Body.String(), http.StatusConflict)
		}
	})
}

// ── POST /auth/login ──────────────────────────────────────────────

func TestLogin(t *testing.T) {
	t.Run("200 con credenciales válidas", func(t *testing.T) {
		s := newTestServer()
		s.registerAndLogin(t, "login-ok@example.com", "profesional") // valida que el flujo completo funcione

		rec := s.do(t, http.MethodPost, "/auth/login", map[string]string{
			"email":     "login-ok@example.com",
			"password":  validPlainPassword,
			"device_id": "device-2",
		}, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			TokenType    string `json:"token_type"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.AccessToken == "" || resp.RefreshToken == "" {
			t.Error("se esperaban tokens no vacíos")
		}
		if resp.TokenType != "Bearer" {
			t.Errorf("token_type = %q, se esperaba 'Bearer'", resp.TokenType)
		}
	})

	t.Run("400 con body inválido", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte("{invalid")))
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("401 con credenciales inválidas", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/auth/login", map[string]string{
			"email":    "no-existe@example.com",
			"password": validPlainPassword,
		}, "")

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, body = %s, se esperaba %d", rec.Code, rec.Body.String(), http.StatusUnauthorized)
		}
	})

	t.Run("usa el User-Agent como device_id cuando no se envía", func(t *testing.T) {
		s := newTestServer()
		s.do(t, http.MethodPost, "/auth/register", map[string]string{
			"email":    "noagent@example.com",
			"password": validPlainPassword,
			"role":     "profesional",
		}, "")

		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(mustJSON(t, map[string]string{
			"email":    "noagent@example.com",
			"password": validPlainPassword,
		})))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "go-test-agent")
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})
}

// ── POST /auth/refresh ────────────────────────────────────────────

func TestRefreshTokens(t *testing.T) {
	t.Run("200 con refresh token válido", func(t *testing.T) {
		s := newTestServer()
		s.do(t, http.MethodPost, "/auth/register", map[string]string{
			"email":    "refresh@example.com",
			"password": validPlainPassword,
			"role":     "profesional",
		}, "")
		loginRec := s.do(t, http.MethodPost, "/auth/login", map[string]string{
			"email":     "refresh@example.com",
			"password":  validPlainPassword,
			"device_id": "device-1",
		}, "")

		var loginResp struct {
			RefreshToken string `json:"refresh_token"`
		}
		_ = json.Unmarshal(loginRec.Body.Bytes(), &loginResp)

		var registerResp struct {
			UserID string `json:"user_id"`
		}
		regRec := s.do(t, http.MethodPost, "/auth/register", map[string]string{
			"email":    "refresh2@example.com",
			"password": validPlainPassword,
			"role":     "profesional",
		}, "")
		_ = json.Unmarshal(regRec.Body.Bytes(), &registerResp)

		// Necesitamos el user_id del usuario logueado, no el del segundo registro de control.
		meRec := s.do(t, http.MethodPost, "/auth/login", map[string]string{
			"email":     "refresh@example.com",
			"password":  validPlainPassword,
			"device_id": "device-1",
		}, "")
		var secondLogin struct {
			RefreshToken string `json:"refresh_token"`
		}
		_ = json.Unmarshal(meRec.Body.Bytes(), &secondLogin)

		rec := s.do(t, http.MethodPost, "/auth/refresh", map[string]string{
			"refresh_token": secondLogin.RefreshToken,
			"device_id":     "device-1",
			"user_id":       userIDFromLogin(t, s, "refresh@example.com"),
		}, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("400 con body inválido", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte("{invalid")))
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("400 con user_id mal formado", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/auth/refresh", map[string]string{
			"refresh_token": "cualquiera",
			"device_id":     "device-1",
			"user_id":       "no-es-un-uuid",
		}, "")

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body = %s, se esperaba %d", rec.Code, rec.Body.String(), http.StatusBadRequest)
		}
	})

	t.Run("401 con refresh token desconocido", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/auth/refresh", map[string]string{
			"refresh_token": "token-inexistente",
			"device_id":     "device-1",
			"user_id":       uuid.New().String(),
		}, "")

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, body = %s, se esperaba %d", rec.Code, rec.Body.String(), http.StatusUnauthorized)
		}
	})
}

// ── POST /auth/logout ─────────────────────────────────────────────

func TestLogout(t *testing.T) {
	t.Run("204 con token válido", func(t *testing.T) {
		s := newTestServer()
		token := s.registerAndLogin(t, "logout-ok@example.com", "profesional")

		rec := s.do(t, http.MethodPost, "/auth/logout", map[string]bool{"global_logout": true}, token)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, body = %s, se esperaba %d", rec.Code, rec.Body.String(), http.StatusNoContent)
		}
	})

	t.Run("401 sin Authorization header", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/auth/logout", map[string]bool{"global_logout": true}, "")

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("401 con token inválido", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/auth/logout", map[string]bool{"global_logout": true}, "token-basura")

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("500 cuando el backend falla con error no-dominio (cubre writeErrorFromDomain fallback)", func(t *testing.T) {
		s := newTestServer()
		token := s.registerAndLogin(t, "logout-fail@example.com", "profesional")

		// FindByID devolverá un error plano (no *DomainError) al intentar el logout,
		// provocando que writeErrorFromDomain tome el camino de fallback → 500.
		s.userRepo.findByIDErr = errors.New("connection reset by peer")

		rec := s.do(t, http.MethodPost, "/auth/logout", map[string]bool{"global_logout": true}, token)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, body = %s, se esperaba %d", rec.Code, rec.Body.String(), http.StatusInternalServerError)
		}

		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != "INTERNAL" {
			t.Errorf("code = %q, se esperaba 'INTERNAL'", body.Code)
		}
	})
}

// ── GET /auth/me ──────────────────────────────────────────────────

func TestMe(t *testing.T) {
	t.Run("200 con token válido devuelve los claims del usuario", func(t *testing.T) {
		s := newTestServer()
		token := s.registerAndLogin(t, "me-ok@example.com", "profesional")

		rec := s.do(t, http.MethodGet, "/auth/me", nil, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			UserID string `json:"user_id"`
			Role   string `json:"role"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Role != "profesional" {
			t.Errorf("role = %q, se esperaba 'profesional'", resp.Role)
		}
		if _, err := uuid.Parse(resp.UserID); err != nil {
			t.Errorf("user_id %q no es un UUID válido", resp.UserID)
		}
	})

	t.Run("401 sin Authorization header", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodGet, "/auth/me", nil, "")

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

// ── helpers ──────────────────────────────────────────────────────

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// userIDFromLogin obtiene el user_id decodificando el claim 'sub' del access token,
// ya que /auth/login no expone el user_id en su respuesta.
func userIDFromLogin(t *testing.T, s *testServer, email string) string {
	t.Helper()
	rec := s.do(t, http.MethodGet, "/auth/me", nil, s.loginToken(t, email))
	var resp struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode /auth/me response: %v", err)
	}
	return resp.UserID
}

func (s *testServer) loginToken(t *testing.T, email string) string {
	t.Helper()
	rec := s.do(t, http.MethodPost, "/auth/login", map[string]string{
		"email":     email,
		"password":  validPlainPassword,
		"device_id": "helper-device",
	}, "")
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return resp.AccessToken
}
