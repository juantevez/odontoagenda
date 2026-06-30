// White-box test: package nats para poder llamar handleUserRegistered
// y extractNameFromEmail que son funciones/métodos unexported.
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	iamaggregate "github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
	iamrepo "github.com/juantevez/odontoagenda/context/iam/domain/repository"
	iamvo "github.com/juantevez/odontoagenda/context/iam/domain/valueobject"
	patientcmd "github.com/juantevez/odontoagenda/context/patient/application/command"
	patientagg "github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	patientrepo "github.com/juantevez/odontoagenda/context/patient/domain/repository"
	patientservice "github.com/juantevez/odontoagenda/context/patient/domain/service"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── mocks ────────────────────────────────────────────────────────

// mockIAMUserRepo implementa iamrepo.UserRepository en memoria.
type mockIAMUserRepo struct {
	users      map[sharedtypes.UserID]*iamaggregate.User
	findByIDErr error
	updateErr   error
}

var _ iamrepo.UserRepository = (*mockIAMUserRepo)(nil)

func newMockIAMUserRepo() *mockIAMUserRepo {
	return &mockIAMUserRepo{users: make(map[sharedtypes.UserID]*iamaggregate.User)}
}

func (m *mockIAMUserRepo) Save(_ context.Context, u *iamaggregate.User) error {
	m.users[u.ID()] = u
	return nil
}
func (m *mockIAMUserRepo) Update(_ context.Context, u *iamaggregate.User) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.users[u.ID()] = u
	return nil
}
func (m *mockIAMUserRepo) FindByID(_ context.Context, id sharedtypes.UserID) (*iamaggregate.User, error) {
	if m.findByIDErr != nil {
		return nil, m.findByIDErr
	}
	u, ok := m.users[id]
	if !ok {
		return nil, sharederrors.NewNotFound("User", id.String())
	}
	return u, nil
}
func (m *mockIAMUserRepo) FindByEmail(_ context.Context, _ sharedvo.Email) (*iamaggregate.User, error) {
	return nil, sharederrors.NewNotFound("User", "email")
}
func (m *mockIAMUserRepo) ExistsByEmail(_ context.Context, _ sharedvo.Email) (bool, error) {
	return false, nil
}

// mockPatientRepo implementa patientrepo.PatientRepository en memoria.
type mockPatientRepo struct {
	patients map[sharedtypes.PatientID]*patientagg.Patient
	saveErr  error
}

var _ patientrepo.PatientRepository = (*mockPatientRepo)(nil)

func newMockPatientRepo() *mockPatientRepo {
	return &mockPatientRepo{patients: make(map[sharedtypes.PatientID]*patientagg.Patient)}
}

func (m *mockPatientRepo) Save(_ context.Context, p *patientagg.Patient) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.patients[p.ID()] = p
	return nil
}
func (m *mockPatientRepo) Update(_ context.Context, _ *patientagg.Patient) error { return nil }
func (m *mockPatientRepo) FindByID(_ context.Context, id sharedtypes.PatientID) (*patientagg.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", id.String())
}
func (m *mockPatientRepo) FindByNationalID(_ context.Context, _ sharedvo.NationalID) (*patientagg.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", "nationalID")
}
func (m *mockPatientRepo) FindByUserID(_ context.Context, _ sharedtypes.UserID) (*patientagg.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", "userID")
}
func (m *mockPatientRepo) Search(_ context.Context, _ string, _ sharedtypes.Page) (sharedtypes.PagedResult[*patientagg.Patient], error) {
	return sharedtypes.PagedResult[*patientagg.Patient]{}, nil
}
func (m *mockPatientRepo) FindNearClinic(_ context.Context, _ sharedtypes.ClinicID, _ float64, _ sharedtypes.Page) (sharedtypes.PagedResult[*patientagg.Patient], error) {
	return sharedtypes.PagedResult[*patientagg.Patient]{}, nil
}
func (m *mockPatientRepo) ExistsByNationalID(_ context.Context, _ sharedvo.NationalID) (bool, error) {
	return false, nil
}
func (m *mockPatientRepo) FindPotentialDuplicates(_ context.Context, _ string, _ string) ([]*patientagg.Patient, error) {
	return nil, nil
}
func (m *mockPatientRepo) Archive(_ context.Context, _ sharedtypes.PatientID, _ string, _ sharedtypes.UserID) error {
	return nil
}

// noopBus implementa pkgevents.Bus sin hacer nada.
type noopBus struct{ subscribeErr error }

func (b *noopBus) Publish(_ context.Context, _ pkgevents.DomainEvent) error { return nil }
func (b *noopBus) Subscribe(_ context.Context, _ pkgevents.SubscribeOptions, _ pkgevents.Handler) error {
	return b.subscribeErr
}
func (b *noopBus) Close() error { return nil }

// ── helpers ──────────────────────────────────────────────────────

// buildEnvelope construye un Envelope con el payload dado serializado como JSON.
func buildEnvelope(t *testing.T, payload userRegisteredPayload) pkgevents.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("setup: marshal payload: %v", err)
	}
	return pkgevents.Envelope{
		EventID:   uuid.New().String(),
		EventType: "user.registered",
		Payload:   json.RawMessage(raw),
	}
}

// newTestIAMUser crea un User en el mock repo usando Reconstitute
// (evita el costo de bcrypt al no necesitar un password real en estos tests).
func newTestIAMUser(t *testing.T, repo *mockIAMUserRepo) *iamaggregate.User {
	t.Helper()
	em, err := sharedvo.NewEmail("test@example.com")
	if err != nil {
		t.Fatalf("setup: email: %v", err)
	}
	user := iamaggregate.Reconstitute(
		uuid.New(), em,
		iamvo.LoadHash([]byte("fakehash")),
		iamvo.RoleProfessional, iamvo.StatusActive,
		nil, "",
		nil,
		sharedtypes.NewAuditInfo(nil), 1,
	)
	_ = repo.Save(context.Background(), user)
	return user
}

// newTestSubscriber construye un PatientProvisionSubscriber con dependencias
// reales (RegisterPatientHandler sobre mock repos) e IAM user repo.
func newTestSubscriber(
	patientRepo *mockPatientRepo,
	iamUserRepo *mockIAMUserRepo,
) *PatientProvisionSubscriber {
	detector := patientservice.NewDuplicateDetector(patientRepo)
	registerHandler := patientcmd.NewRegisterPatientHandler(patientRepo, detector, &noopBus{})
	return NewPatientProvisionSubscriber(&noopBus{}, registerHandler, iamUserRepo)
}

// ── RegisterAll ───────────────────────────────────────────────────

func TestRegisterAll(t *testing.T) {
	t.Run("subscribe exitoso registra el consumer y retorna nil", func(t *testing.T) {
		sub := newTestSubscriber(newMockPatientRepo(), newMockIAMUserRepo())
		if err := sub.RegisterAll(context.Background()); err != nil {
			t.Errorf("RegisterAll() error = %v", err)
		}
	})

	t.Run("propaga el error si Subscribe falla", func(t *testing.T) {
		failBus := &noopBus{subscribeErr: errors.New("nats down")}
		registerHandler := patientcmd.NewRegisterPatientHandler(
			newMockPatientRepo(),
			patientservice.NewDuplicateDetector(newMockPatientRepo()),
			&noopBus{},
		)
		sub := NewPatientProvisionSubscriber(failBus, registerHandler, newMockIAMUserRepo())

		err := sub.RegisterAll(context.Background())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if !errors.Is(err, failBus.subscribeErr) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, failBus.subscribeErr)
		}
	})
}

// ── handleUserRegistered ──────────────────────────────────────────

func TestHandleUserRegistered(t *testing.T) {
	t.Run("crea Patient y vincula User para rol paciente", func(t *testing.T) {
		patientRepo := newMockPatientRepo()
		iamUserRepo := newMockIAMUserRepo()
		user := newTestIAMUser(t, iamUserRepo)
		sub := newTestSubscriber(patientRepo, iamUserRepo)

		env := buildEnvelope(t, userRegisteredPayload{
			UserID:     user.ID().String(),
			Email:      "test@example.com",
			Role:       "paciente",
			OccurredAt: time.Now(),
		})

		err := sub.handleUserRegistered(context.Background(), env)
		if err != nil {
			t.Fatalf("handleUserRegistered() error = %v", err)
		}
		if len(patientRepo.patients) != 1 {
			t.Errorf("patients count = %d, se esperaba 1", len(patientRepo.patients))
		}
		updatedUser, _ := iamUserRepo.FindByID(context.Background(), user.ID())
		if updatedUser.LinkedID() == nil {
			t.Error("LinkedID del User debería estar seteado tras la provisión")
		}
		if updatedUser.LinkedType() != "patient" {
			t.Errorf("LinkedType = %q, se esperaba 'patient'", updatedUser.LinkedType())
		}
	})

	t.Run("no-op para rol no-paciente", func(t *testing.T) {
		patientRepo := newMockPatientRepo()
		iamUserRepo := newMockIAMUserRepo()
		sub := newTestSubscriber(patientRepo, iamUserRepo)

		env := buildEnvelope(t, userRegisteredPayload{
			UserID:     uuid.New().String(),
			Email:      "prof@example.com",
			Role:       "profesional",
			OccurredAt: time.Now(),
		})

		if err := sub.handleUserRegistered(context.Background(), env); err != nil {
			t.Errorf("handleUserRegistered() error = %v, se esperaba nil para non-paciente", err)
		}
		if len(patientRepo.patients) != 0 {
			t.Error("no debería haberse creado ningún Patient para rol profesional")
		}
	})

	t.Run("omite provisión si el User ya tiene linked_id (idempotencia)", func(t *testing.T) {
		patientRepo := newMockPatientRepo()
		iamUserRepo := newMockIAMUserRepo()
		sub := newTestSubscriber(patientRepo, iamUserRepo)

		existingLinkedID := uuid.New().String()
		env := buildEnvelope(t, userRegisteredPayload{
			UserID:     uuid.New().String(),
			Email:      "paciente@example.com",
			Role:       "paciente",
			LinkedID:   &existingLinkedID,
			OccurredAt: time.Now(),
		})

		if err := sub.handleUserRegistered(context.Background(), env); err != nil {
			t.Errorf("handleUserRegistered() error = %v", err)
		}
		if len(patientRepo.patients) != 0 {
			t.Error("no debería crear Patient si ya existe linked_id")
		}
	})

	t.Run("retorna ErrSkipRetry con payload JSON inválido", func(t *testing.T) {
		sub := newTestSubscriber(newMockPatientRepo(), newMockIAMUserRepo())
		env := pkgevents.Envelope{
			EventID:   uuid.New().String(),
			EventType: "user.registered",
			Payload:   json.RawMessage(`{invalid json`),
		}

		err := sub.handleUserRegistered(context.Background(), env)
		if !errors.Is(err, pkgevents.ErrSkipRetry) {
			t.Errorf("err = %v, se esperaba ErrSkipRetry", err)
		}
	})

	t.Run("retorna ErrSkipRetry con user_id no válido como UUID", func(t *testing.T) {
		sub := newTestSubscriber(newMockPatientRepo(), newMockIAMUserRepo())

		env := buildEnvelope(t, userRegisteredPayload{
			UserID:     "not-a-uuid",
			Email:      "paciente@example.com",
			Role:       "paciente",
			OccurredAt: time.Now(),
		})

		err := sub.handleUserRegistered(context.Background(), env)
		if !errors.Is(err, pkgevents.ErrSkipRetry) {
			t.Errorf("err = %v, se esperaba ErrSkipRetry", err)
		}
	})

	t.Run("retorna error para retry si RegisterPatient falla", func(t *testing.T) {
		patientRepo := newMockPatientRepo()
		iamUserRepo := newMockIAMUserRepo()
		user := newTestIAMUser(t, iamUserRepo)

		patientRepo.saveErr = errors.New("db timeout")
		sub := newTestSubscriber(patientRepo, iamUserRepo)

		env := buildEnvelope(t, userRegisteredPayload{
			UserID:     user.ID().String(),
			Email:      "paciente@example.com",
			Role:       "paciente",
			OccurredAt: time.Now(),
		})

		err := sub.handleUserRegistered(context.Background(), env)
		if err == nil {
			t.Fatal("se esperaba error para triggear el retry de NATS")
		}
		if errors.Is(err, pkgevents.ErrSkipRetry) {
			t.Error("el error debería permitir retry, no ser ErrSkipRetry")
		}
	})

	t.Run("retorna error para retry si linkUserToPatient falla en FindByID", func(t *testing.T) {
		patientRepo := newMockPatientRepo()
		iamUserRepo := newMockIAMUserRepo()
		user := newTestIAMUser(t, iamUserRepo)

		iamUserRepo.findByIDErr = errors.New("db connection lost")
		sub := newTestSubscriber(patientRepo, iamUserRepo)

		env := buildEnvelope(t, userRegisteredPayload{
			UserID:     user.ID().String(),
			Email:      "test@example.com",
			Role:       "paciente",
			OccurredAt: time.Now(),
		})

		err := sub.handleUserRegistered(context.Background(), env)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		// El Patient ya fue creado pero el link falló — el mensaje debe reintentarse.
		if len(patientRepo.patients) != 1 {
			t.Errorf("el Patient debería haberse creado antes de fallar el link, count = %d", len(patientRepo.patients))
		}
	})

	t.Run("retorna error para retry si linkUserToPatient falla en Update", func(t *testing.T) {
		patientRepo := newMockPatientRepo()
		iamUserRepo := newMockIAMUserRepo()
		user := newTestIAMUser(t, iamUserRepo)

		iamUserRepo.updateErr = errors.New("optimistic lock conflict")
		sub := newTestSubscriber(patientRepo, iamUserRepo)

		env := buildEnvelope(t, userRegisteredPayload{
			UserID:     user.ID().String(),
			Email:      "test@example.com",
			Role:       "paciente",
			OccurredAt: time.Now(),
		})

		err := sub.handleUserRegistered(context.Background(), env)
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
}

// ── extractNameFromEmail ──────────────────────────────────────────

func TestExtractNameFromEmail(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"juan.perez@gmail.com", "juan.perez"},
		{"test@example.com", "test"},
		{"ab@example.com", "ab"},      // local de exactamente 2 chars → válido
		{"a@example.com", "Paciente"}, // local de 1 char → fallback
		{"@example.com", "Paciente"},  // @ en posición 0 → fallback
		{"notanemail", "Paciente"},    // sin '@' → fallback
		{"", "Paciente"},              // vacío → fallback
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := extractNameFromEmail(tc.input)
			if got != tc.want {
				t.Errorf("extractNameFromEmail(%q) = %q, se esperaba %q", tc.input, got, tc.want)
			}
		})
	}
}
