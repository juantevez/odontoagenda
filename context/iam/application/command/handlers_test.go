package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/iam/application/command"
	"github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/iam/domain/repository"
	"github.com/juantevez/odontoagenda/context/iam/domain/service"
	"github.com/juantevez/odontoagenda/context/iam/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── mocks ────────────────────────────────────────────────────────

type mockUserRepo struct {
	users   map[sharedtypes.UserID]*aggregate.User
	byEmail map[string]*aggregate.User

	saveErr        error
	updateErr      error
	findByIDErr    error
	findByEmailErr error
	existsErr      error
}

var _ repository.UserRepository = (*mockUserRepo)(nil)

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:   make(map[sharedtypes.UserID]*aggregate.User),
		byEmail: make(map[string]*aggregate.User),
	}
}

func (m *mockUserRepo) Save(_ context.Context, user *aggregate.User) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.users[user.ID()] = user
	m.byEmail[user.Email().String()] = user
	return nil
}

func (m *mockUserRepo) Update(_ context.Context, user *aggregate.User) error {
	if m.updateErr != nil {
		return m.updateErr
	}
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
	if m.findByEmailErr != nil {
		return nil, m.findByEmailErr
	}
	u, ok := m.byEmail[email.String()]
	if !ok {
		return nil, sharederrors.NewNotFound("User", email.String())
	}
	return u, nil
}

func (m *mockUserRepo) ExistsByEmail(_ context.Context, email sharedvo.Email) (bool, error) {
	if m.existsErr != nil {
		return false, m.existsErr
	}
	_, ok := m.byEmail[email.String()]
	return ok, nil
}

type mockFamilyRepo struct {
	families  map[sharedtypes.FamilyID]*aggregate.FamilyAccount
	byPatient map[sharedtypes.PatientID]*aggregate.FamilyAccount

	saveErr error
}

var _ repository.FamilyRepository = (*mockFamilyRepo)(nil)

func newMockFamilyRepo() *mockFamilyRepo {
	return &mockFamilyRepo{
		families:  make(map[sharedtypes.FamilyID]*aggregate.FamilyAccount),
		byPatient: make(map[sharedtypes.PatientID]*aggregate.FamilyAccount),
	}
}

func (m *mockFamilyRepo) Save(_ context.Context, family *aggregate.FamilyAccount) error {
	if m.saveErr != nil {
		return m.saveErr
	}
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

type mockEventBus struct {
	published  []events.DomainEvent
	publishErr error
}

var _ events.Bus = (*mockEventBus)(nil)

func (m *mockEventBus) Publish(_ context.Context, event events.DomainEvent) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.published = append(m.published, event)
	return nil
}

func (m *mockEventBus) Subscribe(_ context.Context, _ events.SubscribeOptions, _ events.Handler) error {
	return nil
}

func (m *mockEventBus) Close() error { return nil }

// ── helpers ──────────────────────────────────────────────────────

const validPlainPassword = "Sup3rSecret"

func newTestUser(t *testing.T, email string, role valueobject.Role, linkedID *uuid.UUID, linkedType string) *aggregate.User {
	t.Helper()

	em, err := sharedvo.NewEmail(email)
	if err != nil {
		t.Fatalf("setup: email inválido: %v", err)
	}
	hashed, err := valueobject.HashPassword(validPlainPassword)
	if err != nil {
		t.Fatalf("setup: hash password: %v", err)
	}
	user, err := aggregate.NewUser(em, hashed, role, linkedID, linkedType, nil)
	if err != nil {
		t.Fatalf("setup: crear user: %v", err)
	}
	user.PendingEvents() // limpiar eventos del constructor, no son relevantes en estos tests
	return user
}

func testTokenService() *service.TokenService {
	cfg := service.DefaultTokenConfig([]byte("test-secret-key-0123456789abcdef"), "odontoagenda-test")
	return service.NewTokenService(cfg)
}

func domainErrorCode(t *testing.T, err error) sharederrors.ErrorCode {
	t.Helper()
	de, ok := sharederrors.As(err)
	if !ok {
		t.Fatalf("se esperaba un *sharederrors.DomainError, se obtuvo: %T (%v)", err, err)
	}
	return de.Code
}

// ── RegisterUserHandler ──────────────────────────────────────────

func TestRegisterUserHandler(t *testing.T) {
	t.Run("registra un paciente y crea su FamilyAccount", func(t *testing.T) {
		userRepo := newMockUserRepo()
		familyRepo := newMockFamilyRepo()
		bus := &mockEventBus{}
		h := command.NewRegisterUserHandler(userRepo, familyRepo, bus)

		linkedID := uuid.New()
		cmd := command.RegisterUserCommand{
			Email:         "paciente@example.com",
			PlainPassword: validPlainPassword,
			Role:          string(valueobject.RolePatient),
			LinkedID:      &linkedID,
			LinkedType:    "patient",
			FamilyName:    "Familia Pérez",
		}

		result, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v, se esperaba nil", err)
		}
		if result.UserID == (uuid.UUID{}) {
			t.Error("se esperaba un UserID válido")
		}
		if result.FamilyID == nil {
			t.Fatal("se esperaba un FamilyID, se obtuvo nil")
		}
		if len(userRepo.users) != 1 {
			t.Errorf("se esperaba 1 usuario persistido, hay %d", len(userRepo.users))
		}
		if len(familyRepo.families) != 1 {
			t.Errorf("se esperaba 1 FamilyAccount persistida, hay %d", len(familyRepo.families))
		}
		if len(bus.published) == 0 {
			t.Error("se esperaba al menos un evento publicado (UserRegistered)")
		}
	})

	t.Run("registra un profesional sin crear FamilyAccount", func(t *testing.T) {
		userRepo := newMockUserRepo()
		familyRepo := newMockFamilyRepo()
		bus := &mockEventBus{}
		h := command.NewRegisterUserHandler(userRepo, familyRepo, bus)

		cmd := command.RegisterUserCommand{
			Email:         "doctor@example.com",
			PlainPassword: validPlainPassword,
			Role:          string(valueobject.RoleProfessional),
		}

		result, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v, se esperaba nil", err)
		}
		if result.FamilyID != nil {
			t.Errorf("no se esperaba FamilyID para un profesional, se obtuvo %v", result.FamilyID)
		}
		if len(familyRepo.families) != 0 {
			t.Errorf("no se esperaba ninguna FamilyAccount, hay %d", len(familyRepo.families))
		}
	})

	t.Run("rechaza email inválido", func(t *testing.T) {
		h := command.NewRegisterUserHandler(newMockUserRepo(), newMockFamilyRepo(), &mockEventBus{})

		cmd := command.RegisterUserCommand{
			Email:         "no-es-un-email",
			PlainPassword: validPlainPassword,
			Role:          string(valueobject.RolePatient),
		}

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInvalidArgument)
		}
	})

	t.Run("rechaza rol inválido", func(t *testing.T) {
		h := command.NewRegisterUserHandler(newMockUserRepo(), newMockFamilyRepo(), &mockEventBus{})

		cmd := command.RegisterUserCommand{
			Email:         "user@example.com",
			PlainPassword: validPlainPassword,
			Role:          "rol-inexistente",
		}

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInvalidArgument)
		}
	})

	t.Run("rechaza password débil", func(t *testing.T) {
		h := command.NewRegisterUserHandler(newMockUserRepo(), newMockFamilyRepo(), &mockEventBus{})

		cmd := command.RegisterUserCommand{
			Email:         "user@example.com",
			PlainPassword: "weak",
			Role:          string(valueobject.RolePatient),
		}

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInvalidArgument)
		}
	})

	t.Run("rechaza email ya existente", func(t *testing.T) {
		userRepo := newMockUserRepo()
		existing := newTestUser(t, "dup@example.com", valueobject.RolePatient, nil, "")
		_ = userRepo.Save(context.Background(), existing)

		h := command.NewRegisterUserHandler(userRepo, newMockFamilyRepo(), &mockEventBus{})

		cmd := command.RegisterUserCommand{
			Email:         "dup@example.com",
			PlainPassword: validPlainPassword,
			Role:          string(valueobject.RolePatient),
		}

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrAlreadyExists {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrAlreadyExists)
		}
	})

	t.Run("propaga el error si falla la persistencia del usuario", func(t *testing.T) {
		userRepo := newMockUserRepo()
		sentinel := errors.New("db down")
		userRepo.saveErr = sentinel

		h := command.NewRegisterUserHandler(userRepo, newMockFamilyRepo(), &mockEventBus{})

		cmd := command.RegisterUserCommand{
			Email:         "user@example.com",
			PlainPassword: validPlainPassword,
			Role:          string(valueobject.RolePatient),
		}

		_, err := h.Handle(context.Background(), cmd)
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, sentinel)
		}
	})

	t.Run("no falla el registro si la creación de FamilyAccount falla", func(t *testing.T) {
		userRepo := newMockUserRepo()
		familyRepo := newMockFamilyRepo()
		familyRepo.saveErr = errors.New("family db down")

		h := command.NewRegisterUserHandler(userRepo, familyRepo, &mockEventBus{})

		linkedID := uuid.New()
		cmd := command.RegisterUserCommand{
			Email:         "paciente2@example.com",
			PlainPassword: validPlainPassword,
			Role:          string(valueobject.RolePatient),
			LinkedID:      &linkedID,
			LinkedType:    "patient",
		}

		result, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v, se esperaba nil (la falla de family no debe abortar el registro)", err)
		}
		if result.FamilyID != nil {
			t.Errorf("FamilyID = %v, se esperaba nil porque la persistencia de family falló", result.FamilyID)
		}
		if len(userRepo.users) != 1 {
			t.Errorf("se esperaba que el usuario quedara persistido pese al error de family")
		}
	})

	t.Run("no falla el registro si la publicación del evento falla", func(t *testing.T) {
		bus := &mockEventBus{publishErr: errors.New("nats down")}
		h := command.NewRegisterUserHandler(newMockUserRepo(), newMockFamilyRepo(), bus)

		cmd := command.RegisterUserCommand{
			Email:         "user3@example.com",
			PlainPassword: validPlainPassword,
			Role:          string(valueobject.RoleProfessional),
		}

		result, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v, se esperaba nil (fallo de publicación no debe abortar el registro)", err)
		}
		if result.UserID == (uuid.UUID{}) {
			t.Error("se esperaba un UserID válido")
		}
	})

	t.Run("propaga error interno si ExistsByEmail falla", func(t *testing.T) {
		userRepo := newMockUserRepo()
		userRepo.existsErr = errors.New("db timeout")

		h := command.NewRegisterUserHandler(userRepo, newMockFamilyRepo(), &mockEventBus{})

		cmd := command.RegisterUserCommand{
			Email:         "user4@example.com",
			PlainPassword: validPlainPassword,
			Role:          string(valueobject.RoleProfessional),
		}

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrInternal {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInternal)
		}
	})
}

// ── LoginHandler ──────────────────────────────────────────────────

func TestLoginHandler(t *testing.T) {
	t.Run("login exitoso emite par de tokens y persiste el refresh token", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "login@example.com", valueobject.RoleProfessional, nil, "")
		_ = userRepo.Save(context.Background(), user)

		h := command.NewLoginHandler(userRepo, newMockFamilyRepo(), testTokenService())

		result, err := h.Handle(context.Background(), command.LoginCommand{
			Email:         "login@example.com",
			PlainPassword: validPlainPassword,
			DeviceID:      "device-1",
		})
		if err != nil {
			t.Fatalf("Handle() error = %v, se esperaba nil", err)
		}
		if result.AccessToken == "" || result.RefreshToken == "" {
			t.Error("se esperaban tokens no vacíos")
		}
		if result.AccessTokenExpiry <= time.Now().Unix() {
			t.Error("AccessTokenExpiry debería estar en el futuro")
		}

		persisted, err := userRepo.FindByID(context.Background(), user.ID())
		if err != nil {
			t.Fatalf("FindByID() error = %v", err)
		}
		if len(persisted.RefreshTokens()) != 1 {
			t.Errorf("se esperaba 1 refresh token persistido, hay %d", len(persisted.RefreshTokens()))
		}
	})

	t.Run("rechaza usuario inexistente sin revelar el motivo", func(t *testing.T) {
		h := command.NewLoginHandler(newMockUserRepo(), newMockFamilyRepo(), testTokenService())

		_, err := h.Handle(context.Background(), command.LoginCommand{
			Email:         "no-existe@example.com",
			PlainPassword: validPlainPassword,
			DeviceID:      "device-1",
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("rechaza password incorrecto", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "wrongpw@example.com", valueobject.RoleProfessional, nil, "")
		_ = userRepo.Save(context.Background(), user)

		h := command.NewLoginHandler(userRepo, newMockFamilyRepo(), testTokenService())

		_, err := h.Handle(context.Background(), command.LoginCommand{
			Email:         "wrongpw@example.com",
			PlainPassword: "OtraClave1",
			DeviceID:      "device-1",
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("rechaza usuario suspendido", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "suspended@example.com", valueobject.RoleProfessional, nil, "")
		if err := user.Suspend("incumplimiento", uuid.New()); err != nil {
			t.Fatalf("setup: Suspend() error = %v", err)
		}
		_ = userRepo.Save(context.Background(), user)

		h := command.NewLoginHandler(userRepo, newMockFamilyRepo(), testTokenService())

		_, err := h.Handle(context.Background(), command.LoginCommand{
			Email:         "suspended@example.com",
			PlainPassword: validPlainPassword,
			DeviceID:      "device-1",
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})

	t.Run("rechaza email con formato inválido", func(t *testing.T) {
		h := command.NewLoginHandler(newMockUserRepo(), newMockFamilyRepo(), testTokenService())

		_, err := h.Handle(context.Background(), command.LoginCommand{
			Email:         "formato-invalido",
			PlainPassword: validPlainPassword,
			DeviceID:      "device-1",
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("propaga error interno si falla la persistencia tras el login", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "updatefail@example.com", valueobject.RoleProfessional, nil, "")
		_ = userRepo.Save(context.Background(), user)
		userRepo.updateErr = errors.New("db down")

		h := command.NewLoginHandler(userRepo, newMockFamilyRepo(), testTokenService())

		_, err := h.Handle(context.Background(), command.LoginCommand{
			Email:         "updatefail@example.com",
			PlainPassword: validPlainPassword,
			DeviceID:      "device-1",
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrInternal {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInternal)
		}
	})

	t.Run("propaga error interno si FindByEmail falla (no es NotFound)", func(t *testing.T) {
		userRepo := newMockUserRepo()
		userRepo.findByEmailErr = errors.New("connection reset by peer")

		h := command.NewLoginHandler(userRepo, newMockFamilyRepo(), testTokenService())

		_, err := h.Handle(context.Background(), command.LoginCommand{
			Email:         "cualquiera@example.com",
			PlainPassword: validPlainPassword,
			DeviceID:      "device-1",
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrInternal {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInternal)
		}
	})

	t.Run("login exitoso como paciente resuelve FamilyAccount e identifica guardian", func(t *testing.T) {
		userRepo := newMockUserRepo()
		familyRepo := newMockFamilyRepo()

		linkedID := uuid.New()
		patientID := sharedtypes.PatientID(linkedID)
		user := newTestUser(t, "guardian@example.com", valueobject.RolePatient, &linkedID, "patient")
		_ = userRepo.Save(context.Background(), user)

		// Crear FamilyAccount donde el paciente es el adulto primario.
		family := aggregate.NewFamilyAccount(patientID, "Familia Test", nil)

		// Agregar un menor con el paciente como guardian para activar isGuardian=true.
		minorID := sharedtypes.PatientID(uuid.New())
		if err := family.AddMinor(minorID, aggregate.RelationshipChild, []sharedtypes.PatientID{patientID}, uuid.New()); err != nil {
			t.Fatalf("setup: AddMinor() error = %v", err)
		}
		_ = familyRepo.Save(context.Background(), family)

		h := command.NewLoginHandler(userRepo, familyRepo, testTokenService())

		result, err := h.Handle(context.Background(), command.LoginCommand{
			Email:         "guardian@example.com",
			PlainPassword: validPlainPassword,
			DeviceID:      "device-1",
		})
		if err != nil {
			t.Fatalf("Handle() error = %v, se esperaba nil", err)
		}
		if result.AccessToken == "" {
			t.Error("se esperaba un access token no vacío")
		}
	})
}

// ── RefreshTokensHandler ──────────────────────────────────────────

func TestRefreshTokensHandler(t *testing.T) {
	t.Run("rota el par de tokens con un refresh token válido", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "refresh@example.com", valueobject.RoleProfessional, nil, "")
		tokenService := testTokenService()

		pair, err := tokenService.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("setup: IssueTokenPair() error = %v", err)
		}
		_ = userRepo.Save(context.Background(), user)

		h := command.NewRefreshTokensHandler(userRepo, newMockFamilyRepo(), tokenService)

		result, err := h.Handle(context.Background(), command.RefreshTokensCommand{
			RefreshToken: pair.RefreshToken,
			DeviceID:     "device-1",
			UserID:       user.ID(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v, se esperaba nil", err)
		}
		if result.RefreshToken == pair.RefreshToken {
			t.Error("se esperaba un nuevo refresh token (rotation), se obtuvo el mismo")
		}
		if result.AccessToken == "" {
			t.Error("se esperaba un access token no vacío")
		}
	})

	t.Run("rechaza si el usuario no existe", func(t *testing.T) {
		h := command.NewRefreshTokensHandler(newMockUserRepo(), newMockFamilyRepo(), testTokenService())

		_, err := h.Handle(context.Background(), command.RefreshTokensCommand{
			RefreshToken: "cualquier-token",
			DeviceID:     "device-1",
			UserID:       uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("rechaza un refresh token desconocido", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "badtoken@example.com", valueobject.RoleProfessional, nil, "")
		_ = userRepo.Save(context.Background(), user)

		h := command.NewRefreshTokensHandler(userRepo, newMockFamilyRepo(), testTokenService())

		_, err := h.Handle(context.Background(), command.RefreshTokensCommand{
			RefreshToken: "token-que-no-existe",
			DeviceID:     "device-1",
			UserID:       user.ID(),
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("rechaza un refresh token expirado", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "expired@example.com", valueobject.RoleProfessional, nil, "")

		shortLivedCfg := service.DefaultTokenConfig([]byte("test-secret-key-0123456789abcdef"), "odontoagenda-test")
		shortLivedCfg.RefreshTokenTTL = 10 * time.Millisecond
		tokenService := service.NewTokenService(shortLivedCfg)

		pair, err := tokenService.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("setup: IssueTokenPair() error = %v", err)
		}
		_ = userRepo.Save(context.Background(), user)

		time.Sleep(20 * time.Millisecond)

		h := command.NewRefreshTokensHandler(userRepo, newMockFamilyRepo(), tokenService)

		_, err = h.Handle(context.Background(), command.RefreshTokensCommand{
			RefreshToken: pair.RefreshToken,
			DeviceID:     "device-1",
			UserID:       user.ID(),
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("propaga error interno si falla la persistencia", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "refreshupdatefail@example.com", valueobject.RoleProfessional, nil, "")
		tokenService := testTokenService()

		pair, err := tokenService.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("setup: IssueTokenPair() error = %v", err)
		}
		_ = userRepo.Save(context.Background(), user)
		userRepo.updateErr = errors.New("db down")

		h := command.NewRefreshTokensHandler(userRepo, newMockFamilyRepo(), tokenService)

		_, err = h.Handle(context.Background(), command.RefreshTokensCommand{
			RefreshToken: pair.RefreshToken,
			DeviceID:     "device-1",
			UserID:       user.ID(),
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrInternal {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInternal)
		}
	})
}

// ── LogoutHandler ─────────────────────────────────────────────────

func TestLogoutHandler(t *testing.T) {
	t.Run("logout global revoca todos los refresh tokens", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "logoutall@example.com", valueobject.RoleProfessional, nil, "")
		tokenService := testTokenService()
		if _, err := tokenService.IssueTokenPair(user, "device-1", nil, nil, false); err != nil {
			t.Fatalf("setup: IssueTokenPair() error = %v", err)
		}
		if _, err := tokenService.IssueTokenPair(user, "device-2", nil, nil, false); err != nil {
			t.Fatalf("setup: IssueTokenPair() error = %v", err)
		}
		_ = userRepo.Save(context.Background(), user)

		bus := &mockEventBus{}
		h := command.NewLogoutHandler(userRepo, bus)

		err := h.Handle(context.Background(), command.LogoutCommand{
			UserID:       user.ID(),
			GlobalLogout: true,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v, se esperaba nil", err)
		}

		persisted, _ := userRepo.FindByID(context.Background(), user.ID())
		for _, rt := range persisted.RefreshTokens() {
			if rt.RevokedAt == nil {
				t.Errorf("se esperaba que el token del dispositivo %q quedara revocado", rt.DeviceID)
			}
		}
		if len(bus.published) == 0 {
			t.Error("se esperaba que se publicara el evento UserLoggedOut")
		}
	})

	t.Run("logout parcial revoca solo el refresh token indicado", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "logoutpartial@example.com", valueobject.RoleProfessional, nil, "")
		tokenService := testTokenService()

		pair1, err := tokenService.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("setup: IssueTokenPair() error = %v", err)
		}
		if _, err := tokenService.IssueTokenPair(user, "device-2", nil, nil, false); err != nil {
			t.Fatalf("setup: IssueTokenPair() error = %v", err)
		}
		_ = userRepo.Save(context.Background(), user)

		h := command.NewLogoutHandler(userRepo, &mockEventBus{})

		err = h.Handle(context.Background(), command.LogoutCommand{
			UserID:       user.ID(),
			RefreshToken: pair1.RefreshToken,
			GlobalLogout: false,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v, se esperaba nil", err)
		}

		persisted, _ := userRepo.FindByID(context.Background(), user.ID())
		var revoked, active int
		for _, rt := range persisted.RefreshTokens() {
			if rt.RevokedAt != nil {
				revoked++
			} else {
				active++
			}
		}
		if revoked != 1 || active != 1 {
			t.Errorf("revoked = %d, active = %d; se esperaba 1 y 1", revoked, active)
		}
	})

	t.Run("rechaza si el usuario no existe", func(t *testing.T) {
		h := command.NewLogoutHandler(newMockUserRepo(), &mockEventBus{})

		err := h.Handle(context.Background(), command.LogoutCommand{
			UserID:       uuid.New(),
			GlobalLogout: true,
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrNotFound)
		}
	})

	t.Run("rechaza un refresh token desconocido en logout parcial", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "logoutbadtoken@example.com", valueobject.RoleProfessional, nil, "")
		_ = userRepo.Save(context.Background(), user)

		h := command.NewLogoutHandler(userRepo, &mockEventBus{})

		err := h.Handle(context.Background(), command.LogoutCommand{
			UserID:       user.ID(),
			RefreshToken: "token-inexistente",
			GlobalLogout: false,
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrNotFound)
		}
	})

	t.Run("propaga error interno si falla la persistencia", func(t *testing.T) {
		userRepo := newMockUserRepo()
		user := newTestUser(t, "logoutupdatefail@example.com", valueobject.RoleProfessional, nil, "")
		_ = userRepo.Save(context.Background(), user)
		userRepo.updateErr = errors.New("db down")

		h := command.NewLogoutHandler(userRepo, &mockEventBus{})

		err := h.Handle(context.Background(), command.LogoutCommand{
			UserID:       user.ID(),
			GlobalLogout: true,
		})
		if err == nil {
			t.Fatal("se esperaba un error, se obtuvo nil")
		}
		if code := domainErrorCode(t, err); code != sharederrors.ErrInternal {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInternal)
		}
	})
}
