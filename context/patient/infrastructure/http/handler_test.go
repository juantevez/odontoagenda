package http_test

import (
	"bytes"
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
	"github.com/juantevez/odontoagenda/context/patient/application/command"
	"github.com/juantevez/odontoagenda/context/patient/application/query"
	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	"github.com/juantevez/odontoagenda/context/patient/domain/repository"
	"github.com/juantevez/odontoagenda/context/patient/domain/service"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	patienthttp "github.com/juantevez/odontoagenda/context/patient/infrastructure/http"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── constantes de test ────────────────────────────────────────────

const (
	testSecret = "test-secret-key-0123456789abcdef"
	testIssuer = "odontoagenda-test"
)

// ── mocks ─────────────────────────────────────────────────────────

type mockPatientRepo struct {
	patients            map[sharedtypes.PatientID]*aggregate.Patient
	searchResult        []*aggregate.Patient
	potentialDuplicates []*aggregate.Patient

	saveErr      error
	updateErr    error
	findByIDErr  error
	archiveErr   error
	searchErr    error
}

var _ repository.PatientRepository = (*mockPatientRepo)(nil)

func newMockPatientRepo() *mockPatientRepo {
	return &mockPatientRepo{patients: make(map[sharedtypes.PatientID]*aggregate.Patient)}
}

func (m *mockPatientRepo) Save(_ context.Context, p *aggregate.Patient) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.patients[p.ID()] = p
	return nil
}
func (m *mockPatientRepo) Update(_ context.Context, p *aggregate.Patient) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.patients[p.ID()] = p
	return nil
}
func (m *mockPatientRepo) FindByID(_ context.Context, id sharedtypes.PatientID) (*aggregate.Patient, error) {
	if m.findByIDErr != nil {
		return nil, m.findByIDErr
	}
	p, ok := m.patients[id]
	if !ok {
		return nil, sharederrors.NewNotFound("Patient", id.String())
	}
	return p, nil
}
func (m *mockPatientRepo) FindByNationalID(_ context.Context, _ sharedvo.NationalID) (*aggregate.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", "")
}
func (m *mockPatientRepo) FindByUserID(_ context.Context, _ sharedtypes.UserID) (*aggregate.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", "")
}
func (m *mockPatientRepo) Search(_ context.Context, _ string, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	if m.searchErr != nil {
		return sharedtypes.PagedResult[*aggregate.Patient]{}, m.searchErr
	}
	return sharedtypes.NewPagedResult(m.searchResult, int64(len(m.searchResult)), page), nil
}
func (m *mockPatientRepo) FindNearClinic(_ context.Context, _ sharedtypes.ClinicID, _ float64, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	return sharedtypes.PagedResult[*aggregate.Patient]{}, nil
}
func (m *mockPatientRepo) ExistsByNationalID(_ context.Context, _ sharedvo.NationalID) (bool, error) {
	return false, nil
}
func (m *mockPatientRepo) FindPotentialDuplicates(_ context.Context, _ string, _ string) ([]*aggregate.Patient, error) {
	return m.potentialDuplicates, nil
}
func (m *mockPatientRepo) Archive(_ context.Context, _ sharedtypes.PatientID, _ string, _ sharedtypes.UserID) error {
	return m.archiveErr
}

type mockCoverageHistoryRepo struct{}

var _ repository.CoverageHistoryRepository = (*mockCoverageHistoryRepo)(nil)

func (m *mockCoverageHistoryRepo) Append(_ context.Context, _ coverage.CoverageHistoryEntry) error {
	return nil
}
func (m *mockCoverageHistoryRepo) FindByPatientID(_ context.Context, _ sharedtypes.PatientID, _ sharedtypes.Page) (sharedtypes.PagedResult[coverage.CoverageHistoryEntry], error) {
	return sharedtypes.PagedResult[coverage.CoverageHistoryEntry]{}, nil
}

type noopBus struct{}

var _ events.Bus = (*noopBus)(nil)

func (noopBus) Publish(_ context.Context, _ events.DomainEvent) error        { return nil }
func (noopBus) Subscribe(_ context.Context, _ events.SubscribeOptions, _ events.Handler) error {
	return nil
}
func (noopBus) Close() error { return nil }

// ── test server ───────────────────────────────────────────────────

type testServer struct {
	router      chi.Router
	patientRepo *mockPatientRepo
}

func newTestServer() *testServer {
	repo := newMockPatientRepo()
	histRepo := &mockCoverageHistoryRepo{}
	bus := noopBus{}
	detector := service.NewDuplicateDetector(repo)

	registerH := command.NewRegisterPatientHandler(repo, detector, bus)
	addCoverageH := command.NewAddCoverageHandler(repo, histRepo, bus)
	addAlertH := command.NewAddMedicalAlertHandler(repo, bus)
	mergeH := command.NewMergePatientsHandler(repo, bus)
	updateContactH := command.NewUpdateContactInfoHandler(repo)
	getByIDH := query.NewGetPatientByIDHandler(repo)
	searchH := query.NewSearchPatientsHandler(repo)
	forBookingH := query.NewGetPatientForBookingHandler(repo)

	jwtCfg := middleware.JWTConfig{SecretKey: []byte(testSecret), Issuer: testIssuer}
	r := chi.NewRouter()
	patienthttp.RegisterRoutes(r, jwtCfg,
		registerH, addCoverageH, addAlertH, mergeH, updateContactH,
		getByIDH, searchH, forBookingH,
	)
	return &testServer{router: r, patientRepo: repo}
}

func (s *testServer) do(t *testing.T, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		raw, _ := json.Marshal(body)
		buf = bytes.NewBuffer(raw)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// ── JWT helpers ───────────────────────────────────────────────────

func makeToken(t *testing.T, role middleware.Role, userID uuid.UUID) string {
	t.Helper()
	claims := &middleware.UserClaims{
		UserID: userID,
		Role:   role,
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

// ── helpers de dominio ────────────────────────────────────────────

func addPatientToRepo(t *testing.T, repo *mockPatientRepo) *aggregate.Patient {
	t.Helper()
	name, _ := sharedvo.NewFullName("Juan Perez")
	bd, _ := valueobject.NewBirthDate(1990, 1, 1)
	gender, _ := valueobject.ParseGender("M")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "12345678")
	phone, _ := sharedvo.NewPhoneNumber("+5491112345678")
	p, err := aggregate.NewPatient(nil, name, bd, gender, docID, phone, nil)
	if err != nil {
		t.Fatalf("setup: NewPatient: %v", err)
	}
	p.PendingEvents()
	repo.patients[p.ID()] = p
	return p
}

// ── POST /patients ────────────────────────────────────────────────

func TestRegister(t *testing.T) {
	staffToken := makeToken(t, middleware.RoleReceptionist, uuid.New())

	t.Run("201 registra paciente válido", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients", map[string]string{
			"full_name":  "Ana Lopez",
			"birth_date": "1985-06-15",
			"gender":     "F",
			"doc_type":   "DNI",
			"doc_number": "87654321",
			"phone":      "+5491112345678",
		}, staffToken)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			PatientID string `json:"patient_id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if _, err := uuid.Parse(resp.PatientID); err != nil {
			t.Errorf("patient_id inválido: %q", resp.PatientID)
		}
	})

	t.Run("400 con body inválido", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/patients", bytes.NewBufferString("{invalid"))
		req.Header.Set("Authorization", "Bearer "+staffToken)
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("400 con birth_date mal formateado", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients", map[string]string{
			"full_name":  "Ana Lopez",
			"birth_date": "15/06/1985",
			"gender":     "F",
			"doc_type":   "DNI",
			"doc_number": "87654321",
			"phone":      "+5491112345678",
		}, staffToken)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("409 con advertencia de duplicados potenciales", func(t *testing.T) {
		s := newTestServer()
		// Candidato con mismo nombre → nameSimilarity = 1.0 → score = 0.5 > 0
		name, _ := sharedvo.NewFullName("Ana Lopez")
		bd, _ := valueobject.NewBirthDate(1985, 1, 1)
		g, _ := valueobject.ParseGender("F")
		docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "11111111")
		phone, _ := sharedvo.NewPhoneNumber("+5491100000000")
		candidate, _ := aggregate.NewPatient(nil, name, bd, g, docID, phone, nil)
		candidate.PendingEvents()
		s.patientRepo.potentialDuplicates = []*aggregate.Patient{candidate}

		rec := s.do(t, http.MethodPost, "/patients", map[string]string{
			"full_name":  "Ana Lopez",
			"birth_date": "1985-06-15",
			"gender":     "F",
			"doc_type":   "DNI",
			"doc_number": "87654321",
			"phone":      "+5491112345678",
		}, staffToken)

		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, body = %s, se esperaba 409", rec.Code, rec.Body.String())
		}
		var resp struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "DUPLICATE_WARNING" {
			t.Errorf("code = %q, se esperaba DUPLICATE_WARNING", resp.Code)
		}
	})

	t.Run("401 sin token", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients", map[string]string{"full_name": "test"}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, se esperaba 401", rec.Code)
		}
	})

	t.Run("403 con rol de paciente (sin permisos de staff)", func(t *testing.T) {
		s := newTestServer()
		patientToken := makeToken(t, middleware.RolePatient, uuid.New())
		rec := s.do(t, http.MethodPost, "/patients", map[string]string{"full_name": "test"}, patientToken)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, se esperaba 403", rec.Code)
		}
	})

	t.Run("propaga error de dominio como status correcto", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients", map[string]string{
			"full_name":  "X", // nombre inválido (<2 chars)
			"birth_date": "1985-06-15",
			"gender":     "F",
			"doc_type":   "DNI",
			"doc_number": "87654321",
			"phone":      "+5491112345678",
		}, staffToken)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})
}

// ── GET /patients ─────────────────────────────────────────────────

func TestSearch(t *testing.T) {
	token := makeToken(t, middleware.RoleReceptionist, uuid.New())

	t.Run("200 con resultados paginados", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)
		s.patientRepo.searchResult = []*aggregate.Patient{p}

		rec := s.do(t, http.MethodGet, "/patients?q=Juan&limit=10&offset=0", nil, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("200 con lista vacía", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodGet, "/patients", nil, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	})

	t.Run("propaga error del repositorio", func(t *testing.T) {
		s := newTestServer()
		s.patientRepo.searchErr = errors.New("db timeout")

		rec := s.do(t, http.MethodGet, "/patients", nil, token)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, se esperaba 500", rec.Code)
		}
	})

	t.Run("401 sin token", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodGet, "/patients", nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, se esperaba 401", rec.Code)
		}
	})
}

// ── GET /patients/{patientId} ─────────────────────────────────────

func TestGetByID(t *testing.T) {
	token := makeToken(t, middleware.RoleReceptionist, uuid.New())

	t.Run("200 retorna el DTO del paciente", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)

		rec := s.do(t, http.MethodGet, "/patients/"+p.ID().String(), nil, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.ID != p.ID().String() {
			t.Errorf("ID = %q", resp.ID)
		}
	})

	t.Run("400 con patientId no válido como UUID", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodGet, "/patients/no-es-uuid", nil, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("404 si el paciente no existe", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodGet, "/patients/"+uuid.New().String(), nil, token)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, se esperaba 404", rec.Code)
		}
	})
}

// ── GET /patients/{patientId}/for-booking ─────────────────────────

func TestGetForBooking(t *testing.T) {
	token := makeToken(t, middleware.RoleProfessional, uuid.New())

	t.Run("200 retorna vista de booking", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)

		rec := s.do(t, http.MethodGet, "/patients/"+p.ID().String()+"/for-booking", nil, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("400 con patientId inválido", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodGet, "/patients/no-uuid/for-booking", nil, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("404 si el paciente no existe", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodGet, "/patients/"+uuid.New().String()+"/for-booking", nil, token)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, se esperaba 404", rec.Code)
		}
	})
}

// ── POST /patients/{patientId}/coverage ───────────────────────────

func TestAddCoverage(t *testing.T) {
	staffID := uuid.New()
	token := makeToken(t, middleware.RoleClinicAdmin, staffID)

	validCoverageBody := map[string]any{
		"coverage_type": "Privado",
		"valid_from":    "2025-01-01",
	}

	t.Run("201 agrega cobertura exitosamente", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)

		rec := s.do(t, http.MethodPost, "/patients/"+p.ID().String()+"/coverage", validCoverageBody, token)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			CoverageID string `json:"coverage_id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if _, err := uuid.Parse(resp.CoverageID); err != nil {
			t.Errorf("coverage_id inválido: %q", resp.CoverageID)
		}
	})

	t.Run("201 con agreementID y validUntil opcionales", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)
		agID := uuid.New().String()
		until := "2026-12-31"

		body := map[string]any{
			"coverage_type":     "ObraSocial",
			"provider_name":     "OSDE",
			"valid_from":        "2025-01-01",
			"agreement_id":      agID,
			"valid_until":       until,
		}
		rec := s.do(t, http.MethodPost, "/patients/"+p.ID().String()+"/coverage", body, token)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("400 con patientId inválido", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients/no-uuid/coverage", validCoverageBody, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("400 con body inválido", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)
		req := httptest.NewRequest(http.MethodPost, "/patients/"+p.ID().String()+"/coverage", bytes.NewBufferString("{invalid"))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("400 con valid_from mal formateado", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)
		rec := s.do(t, http.MethodPost, "/patients/"+p.ID().String()+"/coverage",
			map[string]any{"coverage_type": "Privado", "valid_from": "01/01/2025"}, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("propaga error de dominio", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients/"+uuid.New().String()+"/coverage",
			validCoverageBody, token)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, se esperaba 404 para paciente inexistente", rec.Code)
		}
	})

	t.Run("500 cuando el backend falla con error no-dominio (cubre writeErrorFromDomain fallback)", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)
		s.patientRepo.updateErr = errors.New("plain db error")

		rec := s.do(t, http.MethodPost, "/patients/"+p.ID().String()+"/coverage", validCoverageBody, token)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, se esperaba 500", rec.Code)
		}
		var body struct{ Code string `json:"code"` }
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != "INTERNAL" {
			t.Errorf("code = %q, se esperaba INTERNAL", body.Code)
		}
	})
}

// ── POST /patients/{patientId}/medical-alerts ─────────────────────

func TestAddMedicalAlert(t *testing.T) {
	staffToken := makeToken(t, middleware.RoleReceptionist, uuid.New())
	patientToken := makeToken(t, middleware.RolePatient, uuid.New())

	validAlertBody := map[string]string{
		"alert_type":  "Alergia",
		"severity":    "Warning",
		"description": "Penicilina",
	}

	t.Run("201 staff agrega alerta", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)

		rec := s.do(t, http.MethodPost, "/patients/"+p.ID().String()+"/medical-alerts", validAlertBody, staffToken)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("201 paciente auto-reporta alerta", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)

		rec := s.do(t, http.MethodPost, "/patients/"+p.ID().String()+"/medical-alerts", map[string]string{
			"alert_type":  "Medicamento",
			"description": "Tomo ibuprofeno",
		}, patientToken)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("400 con patientId inválido", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients/no-uuid/medical-alerts", validAlertBody, staffToken)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("400 con body inválido", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)
		req := httptest.NewRequest(http.MethodPost, "/patients/"+p.ID().String()+"/medical-alerts", bytes.NewBufferString("{bad"))
		req.Header.Set("Authorization", "Bearer "+staffToken)
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("propaga error de dominio", func(t *testing.T) {
		s := newTestServer()
		// Paciente no existe → 404
		rec := s.do(t, http.MethodPost, "/patients/"+uuid.New().String()+"/medical-alerts", validAlertBody, staffToken)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, se esperaba 404", rec.Code)
		}
	})
}

// ── POST /patients/merge ──────────────────────────────────────────

func TestMergePatients(t *testing.T) {
	adminToken := makeToken(t, middleware.RoleClinicAdmin, uuid.New())

	t.Run("204 fusiona dos pacientes exitosamente", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients/merge", map[string]string{
			"target_patient_id": uuid.New().String(),
			"source_patient_id": uuid.New().String(),
		}, adminToken)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("400 con body inválido", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/patients/merge", bytes.NewBufferString("{bad"))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("400 con target_patient_id inválido", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients/merge", map[string]string{
			"target_patient_id": "no-uuid",
			"source_patient_id": uuid.New().String(),
		}, adminToken)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("400 con source_patient_id inválido", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients/merge", map[string]string{
			"target_patient_id": uuid.New().String(),
			"source_patient_id": "no-uuid",
		}, adminToken)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("401 sin token", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPost, "/patients/merge", map[string]string{
			"target_patient_id": uuid.New().String(),
			"source_patient_id": uuid.New().String(),
		}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, se esperaba 401", rec.Code)
		}
	})

	t.Run("403 con rol sin permisos (receptionist)", func(t *testing.T) {
		s := newTestServer()
		staffToken := makeToken(t, middleware.RoleReceptionist, uuid.New())
		rec := s.do(t, http.MethodPost, "/patients/merge", map[string]string{
			"target_patient_id": uuid.New().String(),
			"source_patient_id": uuid.New().String(),
		}, staffToken)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, se esperaba 403", rec.Code)
		}
	})

	t.Run("propaga error de dominio (mismos IDs)", func(t *testing.T) {
		s := newTestServer()
		sameID := uuid.New().String()
		rec := s.do(t, http.MethodPost, "/patients/merge", map[string]string{
			"target_patient_id": sameID,
			"source_patient_id": sameID,
		}, adminToken)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})
}

// ── PUT /patients/{patientId}/contact ─────────────────────────────

func TestUpdateContact(t *testing.T) {
	token := makeToken(t, middleware.RolePatient, uuid.New())

	validContactBody := map[string]string{
		"phone": "+5491112345678",
	}

	t.Run("204 actualiza contacto exitosamente", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)

		rec := s.do(t, http.MethodPut, "/patients/"+p.ID().String()+"/contact", validContactBody, token)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("400 con patientId inválido", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPut, "/patients/no-uuid/contact", validContactBody, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("400 con body inválido", func(t *testing.T) {
		s := newTestServer()
		p := addPatientToRepo(t, s.patientRepo)
		req := httptest.NewRequest(http.MethodPut, "/patients/"+p.ID().String()+"/contact", bytes.NewBufferString("{bad"))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, se esperaba 400", rec.Code)
		}
	})

	t.Run("propaga error de dominio", func(t *testing.T) {
		s := newTestServer()
		rec := s.do(t, http.MethodPut, "/patients/"+uuid.New().String()+"/contact", validContactBody, token)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, se esperaba 404", rec.Code)
		}
	})
}
