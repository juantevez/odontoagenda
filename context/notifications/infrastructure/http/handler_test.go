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
	notifhttp "github.com/juantevez/odontoagenda/context/notifications/infrastructure/http"
	"github.com/juantevez/odontoagenda/context/notifications/domain/entity"
	"github.com/juantevez/odontoagenda/context/notifications/domain/repository"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

// ── constantes de test ────────────────────────────────────────────

const (
	testSecret = "super-secret-test-key-notifications"
	testIssuer = "odontoagenda.iam"
)

// ── mockInboxRepo ─────────────────────────────────────────────────

type mockInboxRepo struct {
	items          []*entity.InboxNotification
	unreadCount    int
	findErr        error
	countErr       error
	markReadErr    error
	markAllReadErr error

	lastFindClinicID  uuid.UUID
	lastFindUnreadOnly bool
	lastFindLimit     int
	lastMarkReadID    uuid.UUID
	lastMarkAllClinic uuid.UUID
}

var _ repository.InboxRepository = (*mockInboxRepo)(nil)

func (m *mockInboxRepo) FindByClinic(_ context.Context, id uuid.UUID, unreadOnly bool, limit int) ([]*entity.InboxNotification, error) {
	m.lastFindClinicID = id
	m.lastFindUnreadOnly = unreadOnly
	m.lastFindLimit = limit
	return m.items, m.findErr
}

func (m *mockInboxRepo) CountUnread(_ context.Context, id uuid.UUID) (int, error) {
	return m.unreadCount, m.countErr
}

func (m *mockInboxRepo) MarkRead(_ context.Context, id uuid.UUID) error {
	m.lastMarkReadID = id
	return m.markReadErr
}

func (m *mockInboxRepo) MarkAllRead(_ context.Context, id uuid.UUID) error {
	m.lastMarkAllClinic = id
	return m.markAllReadErr
}

func (m *mockInboxRepo) Save(_ context.Context, _ *entity.InboxNotification) error { return nil }

// ── JWT helpers ───────────────────────────────────────────────────

func makeToken(t *testing.T, role middleware.Role) string {
	t.Helper()
	claims := &middleware.UserClaims{
		UserID: uuid.New(),
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
		SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("makeToken: %v", err)
	}
	return tok
}

// ── server ────────────────────────────────────────────────────────

func testServer(repo repository.InboxRepository) *httptest.Server {
	r := chi.NewRouter()
	cfg := middleware.JWTConfig{SecretKey: []byte(testSecret), Issuer: testIssuer}
	notifhttp.RegisterRoutes(r, cfg, repo, nil)
	return httptest.NewServer(r)
}

// ── fixture ───────────────────────────────────────────────────────

func newNotification(clinicID *uuid.UUID) *entity.InboxNotification {
	n := entity.NewInboxNotification(
		valueobject.TypeAppointmentBooked,
		clinicID,
		uuid.New().String(),
		"Turno reservado",
		"El paciente reservó un turno.",
	)
	return n
}

// ── GET /notifications ────────────────────────────────────────────

func TestList_401_SinToken(t *testing.T) {
	srv := testServer(&mockInboxRepo{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/notifications?clinic_id=" + uuid.New().String())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, quería 401", resp.StatusCode)
	}
}

func TestList_401_TokenInvalido(t *testing.T) {
	srv := testServer(&mockInboxRepo{})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/notifications?clinic_id="+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer token.malformado.xxx")
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, quería 401", resp.StatusCode)
	}
}

func TestList_400_SinClinicID(t *testing.T) {
	srv := testServer(&mockInboxRepo{})
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", resp.StatusCode)
	}
	assertErrorField(t, resp, "clinic_id requerido")
}

func TestList_400_ClinicIDInvalido(t *testing.T) {
	srv := testServer(&mockInboxRepo{})
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/notifications?clinic_id=no-es-uuid", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", resp.StatusCode)
	}
	assertErrorField(t, resp, "clinic_id inválido")
}

func TestList_500_FindByClinicFalla(t *testing.T) {
	repo := &mockInboxRepo{findErr: errors.New("db down")}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/notifications?clinic_id="+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, quería 500", resp.StatusCode)
	}
}

func TestList_500_CountUnreadFalla(t *testing.T) {
	clinicID := uuid.New()
	repo := &mockInboxRepo{
		items:    []*entity.InboxNotification{newNotification(&clinicID)},
		countErr: errors.New("count fail"),
	}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/notifications?clinic_id="+clinicID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, quería 500", resp.StatusCode)
	}
}

func TestList_200_RetornaItemsYUnreadCount(t *testing.T) {
	clinicID := uuid.New()
	n := newNotification(&clinicID)
	repo := &mockInboxRepo{items: []*entity.InboxNotification{n}, unreadCount: 3}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/notifications?clinic_id="+clinicID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quería 200", resp.StatusCode)
	}

	var body struct {
		Items []struct {
			ID       string  `json:"id"`
			Type     string  `json:"type"`
			ClinicID *string `json:"clinic_id"`
			Title    string  `json:"title"`
			Body     string  `json:"body"`
			IsRead   bool    `json:"is_read"`
		} `json:"items"`
		UnreadCount int `json:"unread_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(body.Items) != 1 {
		t.Fatalf("len(items) = %d, quería 1", len(body.Items))
	}
	item := body.Items[0]
	if item.ID != n.ID.String() {
		t.Errorf("ID = %q, quería %q", item.ID, n.ID.String())
	}
	if item.Type != string(n.Type) {
		t.Errorf("Type = %q, quería %q", item.Type, n.Type)
	}
	if item.ClinicID == nil || *item.ClinicID != clinicID.String() {
		t.Errorf("ClinicID = %v, quería %q", item.ClinicID, clinicID.String())
	}
	if item.Title != n.Title {
		t.Errorf("Title = %q, quería %q", item.Title, n.Title)
	}
	if item.IsRead != false {
		t.Error("IsRead = true, quería false (notificación nueva)")
	}
	if body.UnreadCount != 3 {
		t.Errorf("unread_count = %d, quería 3", body.UnreadCount)
	}
}

func TestList_200_ClinicIDNilOmitidoEnDTO(t *testing.T) {
	n := newNotification(nil) // visible en todas las sedes
	repo := &mockInboxRepo{items: []*entity.InboxNotification{n}}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/notifications?clinic_id="+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	var body struct {
		Items []map[string]any `json:"items"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if _, ok := body.Items[0]["clinic_id"]; ok {
		t.Error("clinic_id presente en JSON, debe omitirse cuando es nil (omitempty)")
	}
}

func TestList_200_ReadAtPresenteCuandoLeida(t *testing.T) {
	n := newNotification(nil)
	readAt := time.Now().UTC().Add(-time.Hour)
	n.ReadAt = &readAt
	repo := &mockInboxRepo{items: []*entity.InboxNotification{n}}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/notifications?clinic_id="+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	var body struct {
		Items []struct {
			IsRead bool    `json:"is_read"`
			ReadAt *string `json:"read_at"`
		} `json:"items"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if !body.Items[0].IsRead {
		t.Error("IsRead = false, quería true")
	}
	if body.Items[0].ReadAt == nil {
		t.Error("read_at ausente en JSON, quería timestamp")
	}
}

func TestList_200_UnreadOnlyPropagadoAlRepo(t *testing.T) {
	clinicID := uuid.New()
	repo := &mockInboxRepo{}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet,
		srv.URL+"/notifications?clinic_id="+clinicID.String()+"&unread_only=true", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	http.DefaultClient.Do(req)

	if !repo.lastFindUnreadOnly {
		t.Error("FindByClinic llamado con unreadOnly=false, quería true")
	}
}

func TestList_200_LimitCustomPropagadoAlRepo(t *testing.T) {
	repo := &mockInboxRepo{}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet,
		srv.URL+"/notifications?clinic_id="+uuid.New().String()+"&limit=25", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	http.DefaultClient.Do(req)

	if repo.lastFindLimit != 25 {
		t.Errorf("lastFindLimit = %d, quería 25", repo.lastFindLimit)
	}
}

func TestList_200_LimitFueraDeBanda_UsaDefault50(t *testing.T) {
	repo := &mockInboxRepo{}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)

	for _, badLimit := range []string{"0", "201", "-5", "abc"} {
		req, _ := http.NewRequest(http.MethodGet,
			srv.URL+"/notifications?clinic_id="+uuid.New().String()+"&limit="+badLimit, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		http.DefaultClient.Do(req)

		if repo.lastFindLimit != 50 {
			t.Errorf("limit=%q → lastFindLimit = %d, quería default 50", badLimit, repo.lastFindLimit)
		}
	}
}

func TestList_200_ItemsVacios_RetornaArrayVacio(t *testing.T) {
	repo := &mockInboxRepo{items: []*entity.InboxNotification{}}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/notifications?clinic_id="+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quería 200", resp.StatusCode)
	}
	var body struct {
		Items []any `json:"items"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Items) != 0 {
		t.Errorf("len(items) = %d, quería 0", len(body.Items))
	}
}

func TestList_200_ClinicIDPropagadoAlRepo(t *testing.T) {
	clinicID := uuid.New()
	repo := &mockInboxRepo{}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/notifications?clinic_id="+clinicID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	http.DefaultClient.Do(req)

	if repo.lastFindClinicID != clinicID {
		t.Errorf("lastFindClinicID = %v, quería %v", repo.lastFindClinicID, clinicID)
	}
}

// ── PATCH /notifications/{id}/read ───────────────────────────────

func TestMarkRead_401_SinToken(t *testing.T) {
	srv := testServer(&mockInboxRepo{})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/notifications/"+uuid.New().String()+"/read", nil)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, quería 401", resp.StatusCode)
	}
}

func TestMarkRead_400_IDInvalido(t *testing.T) {
	srv := testServer(&mockInboxRepo{})
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/notifications/no-es-uuid/read", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", resp.StatusCode)
	}
	assertErrorField(t, resp, "id inválido")
}

func TestMarkRead_500_RepoFalla(t *testing.T) {
	repo := &mockInboxRepo{markReadErr: errors.New("db error")}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/notifications/"+uuid.New().String()+"/read", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, quería 500", resp.StatusCode)
	}
}

func TestMarkRead_204_Exitoso(t *testing.T) {
	notifID := uuid.New()
	repo := &mockInboxRepo{}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/notifications/"+notifID.String()+"/read", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, quería 204", resp.StatusCode)
	}
	if repo.lastMarkReadID != notifID {
		t.Errorf("MarkRead llamado con %v, quería %v", repo.lastMarkReadID, notifID)
	}
}

// ── POST /notifications/read-all ─────────────────────────────────

func TestMarkAllRead_401_SinToken(t *testing.T) {
	srv := testServer(&mockInboxRepo{})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/notifications/read-all?clinic_id="+uuid.New().String(), nil)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, quería 401", resp.StatusCode)
	}
}

func TestMarkAllRead_400_SinClinicID(t *testing.T) {
	srv := testServer(&mockInboxRepo{})
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/notifications/read-all", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", resp.StatusCode)
	}
	assertErrorField(t, resp, "clinic_id requerido")
}

func TestMarkAllRead_400_ClinicIDInvalido(t *testing.T) {
	srv := testServer(&mockInboxRepo{})
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/notifications/read-all?clinic_id=xyz", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, quería 400", resp.StatusCode)
	}
	assertErrorField(t, resp, "clinic_id inválido")
}

func TestMarkAllRead_500_RepoFalla(t *testing.T) {
	repo := &mockInboxRepo{markAllReadErr: errors.New("db error")}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/notifications/read-all?clinic_id="+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, quería 500", resp.StatusCode)
	}
}

func TestMarkAllRead_204_Exitoso(t *testing.T) {
	clinicID := uuid.New()
	repo := &mockInboxRepo{}
	srv := testServer(repo)
	defer srv.Close()

	tok := makeToken(t, middleware.RoleClinicAdmin)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/notifications/read-all?clinic_id="+clinicID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, quería 204", resp.StatusCode)
	}
	if repo.lastMarkAllClinic != clinicID {
		t.Errorf("MarkAllRead llamado con %v, quería %v", repo.lastMarkAllClinic, clinicID)
	}
}

// ── helper de aserción ────────────────────────────────────────────

func assertErrorField(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got := body["error"]; got != want {
		t.Errorf("body[\"error\"] = %q, quería %q", got, want)
	}
}
