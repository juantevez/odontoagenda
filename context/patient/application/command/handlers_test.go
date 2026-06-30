package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/application/command"
	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	"github.com/juantevez/odontoagenda/context/patient/domain/repository"
	"github.com/juantevez/odontoagenda/context/patient/domain/service"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── mocks ────────────────────────────────────────────────────────

type mockPatientRepo struct {
	patients map[sharedtypes.PatientID]*aggregate.Patient

	saveErr              error
	updateErr            error
	findByIDErr          error
	archiveErr           error
	existsByNationalID   bool
	existsByNationalErr  error
	potentialDuplicates  []*aggregate.Patient
	findPotentialErr     error
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
	return nil, sharederrors.NewNotFound("Patient", "nationalID")
}

func (m *mockPatientRepo) FindByUserID(_ context.Context, _ sharedtypes.UserID) (*aggregate.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", "userID")
}

func (m *mockPatientRepo) Search(_ context.Context, _ string, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	return sharedtypes.PagedResult[*aggregate.Patient]{}, nil
}

func (m *mockPatientRepo) FindNearClinic(_ context.Context, _ sharedtypes.ClinicID, _ float64, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	return sharedtypes.PagedResult[*aggregate.Patient]{}, nil
}

func (m *mockPatientRepo) ExistsByNationalID(_ context.Context, _ sharedvo.NationalID) (bool, error) {
	return m.existsByNationalID, m.existsByNationalErr
}

func (m *mockPatientRepo) FindPotentialDuplicates(_ context.Context, _ string, _ string) ([]*aggregate.Patient, error) {
	return m.potentialDuplicates, m.findPotentialErr
}

func (m *mockPatientRepo) Archive(_ context.Context, _ sharedtypes.PatientID, _ string, _ sharedtypes.UserID) error {
	return m.archiveErr
}

type mockCoverageHistoryRepo struct {
	appendErr error
}

var _ repository.CoverageHistoryRepository = (*mockCoverageHistoryRepo)(nil)

func (m *mockCoverageHistoryRepo) Append(_ context.Context, _ coverage.CoverageHistoryEntry) error {
	return m.appendErr
}

func (m *mockCoverageHistoryRepo) FindByPatientID(_ context.Context, _ sharedtypes.PatientID, _ sharedtypes.Page) (sharedtypes.PagedResult[coverage.CoverageHistoryEntry], error) {
	return sharedtypes.PagedResult[coverage.CoverageHistoryEntry]{}, nil
}

type noopEventBus struct{}

var _ events.Bus = (*noopEventBus)(nil)

func (noopEventBus) Publish(_ context.Context, _ events.DomainEvent) error { return nil }
func (noopEventBus) Subscribe(_ context.Context, _ events.SubscribeOptions, _ events.Handler) error {
	return nil
}
func (noopEventBus) Close() error { return nil }

// failEventBus retorna error en Publish para verificar que los handlers
// loguean el warning sin abortar la operación.
type failEventBus struct{}

var _ events.Bus = (*failEventBus)(nil)

func (failEventBus) Publish(_ context.Context, _ events.DomainEvent) error {
	return errors.New("nats unavailable")
}
func (failEventBus) Subscribe(_ context.Context, _ events.SubscribeOptions, _ events.Handler) error {
	return nil
}
func (failEventBus) Close() error { return nil }

// ── helpers ──────────────────────────────────────────────────────

const (
	validPhone     = "+5491112345678"
	validPhone2    = "+5491198765432"
	validDocNumber = "12345678"
)

func validBirthDate() time.Time {
	return time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
}

func validRegisterCmd() command.RegisterPatientCommand {
	return command.RegisterPatientCommand{
		FullName:           "Juan Perez",
		BirthDate:          validBirthDate(),
		Gender:             "M",
		DocType:            "DNI",
		DocNumber:          validDocNumber,
		Phone:              validPhone,
		SkipDuplicateCheck: true,
	}
}

// newPatient crea un Patient y lo guarda en el mock repo.
func newPatient(t *testing.T, repo *mockPatientRepo) *aggregate.Patient {
	t.Helper()
	name, _ := sharedvo.NewFullName("Juan Perez")
	bd, _ := valueobject.NewBirthDate(1990, 1, 1)
	gender, _ := valueobject.ParseGender("M")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, validDocNumber)
	phone, _ := sharedvo.NewPhoneNumber(validPhone)

	p, err := aggregate.NewPatient(nil, name, bd, gender, docID, phone, nil)
	if err != nil {
		t.Fatalf("setup: NewPatient: %v", err)
	}
	p.PendingEvents()
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatalf("setup: Save: %v", err)
	}
	return p
}

func newRegisterHandler(repo *mockPatientRepo) *command.RegisterPatientHandler {
	detector := service.NewDuplicateDetector(repo)
	return command.NewRegisterPatientHandler(repo, detector, noopEventBus{})
}

func domainCode(t *testing.T, err error) sharederrors.ErrorCode {
	t.Helper()
	de, ok := sharederrors.As(err)
	if !ok {
		t.Fatalf("se esperaba *DomainError, se obtuvo %T: %v", err, err)
	}
	return de.Code
}

// ── RegisterPatientHandler ────────────────────────────────────────

func TestRegisterPatientHandler(t *testing.T) {
	t.Run("201 registra paciente con campos mínimos", func(t *testing.T) {
		repo := newMockPatientRepo()
		h := newRegisterHandler(repo)

		result, err := h.Handle(context.Background(), validRegisterCmd())
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.PatientID == uuid.Nil {
			t.Error("PatientID vacío")
		}
		if len(repo.patients) != 1 {
			t.Errorf("patients count = %d, se esperaba 1", len(repo.patients))
		}
	})

	t.Run("registra paciente con email y datos de emergencia", func(t *testing.T) {
		repo := newMockPatientRepo()
		h := newRegisterHandler(repo)

		cmd := validRegisterCmd()
		cmd.Email = "juan@example.com"
		cmd.EmergencyName = "María Perez"
		cmd.EmergencyPhone = validPhone2

		result, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.PatientID == uuid.Nil {
			t.Error("PatientID vacío")
		}
	})

	t.Run("SkipDuplicateCheck=true omite la detección aunque haya duplicados", func(t *testing.T) {
		repo := newMockPatientRepo()
		repo.existsByNationalID = true // exacto duplicado — debería bloquear sin skip
		h := newRegisterHandler(repo)

		cmd := validRegisterCmd()
		cmd.SkipDuplicateCheck = true

		result, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.PatientID == uuid.Nil {
			t.Error("PatientID vacío")
		}
	})

	t.Run("rechaza nombre de paciente inválido", func(t *testing.T) {
		h := newRegisterHandler(newMockPatientRepo())

		cmd := validRegisterCmd()
		cmd.FullName = "X" // demasiado corto

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInvalidArgument)
		}
	})

	t.Run("rechaza fecha de nacimiento futura", func(t *testing.T) {
		h := newRegisterHandler(newMockPatientRepo())

		cmd := validRegisterCmd()
		cmd.BirthDate = time.Now().Add(24 * time.Hour)

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInvalidArgument)
		}
	})

	t.Run("rechaza género inválido", func(t *testing.T) {
		h := newRegisterHandler(newMockPatientRepo())

		cmd := validRegisterCmd()
		cmd.Gender = "X"

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza número de documento vacío", func(t *testing.T) {
		h := newRegisterHandler(newMockPatientRepo())

		cmd := validRegisterCmd()
		cmd.DocNumber = ""

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza teléfono inválido", func(t *testing.T) {
		h := newRegisterHandler(newMockPatientRepo())

		cmd := validRegisterCmd()
		cmd.Phone = "no-es-telefono"

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza email opcional con formato inválido", func(t *testing.T) {
		h := newRegisterHandler(newMockPatientRepo())

		cmd := validRegisterCmd()
		cmd.Email = "no-es-email"

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza teléfono de emergencia inválido", func(t *testing.T) {
		h := newRegisterHandler(newMockPatientRepo())

		cmd := validRegisterCmd()
		cmd.EmergencyPhone = "mal-formato"

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("duplicado exacto por NationalID retorna ErrAlreadyExists", func(t *testing.T) {
		repo := newMockPatientRepo()
		repo.existsByNationalID = true
		h := newRegisterHandler(repo)

		cmd := validRegisterCmd()
		cmd.SkipDuplicateCheck = false

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrAlreadyExists {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrAlreadyExists)
		}
	})

	t.Run("duplicados potenciales retornan candidatos sin error", func(t *testing.T) {
		repo := newMockPatientRepo()
		// Crear un candidato con mismo nombre → nameSimilarity = 1.0 → score > 0
		candidate := newPatient(t, newMockPatientRepo()) // en repo distinto para no contaminar
		repo.potentialDuplicates = []*aggregate.Patient{candidate}
		h := newRegisterHandler(repo)

		cmd := validRegisterCmd()
		cmd.SkipDuplicateCheck = false

		result, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v (se esperaba nil con candidatos)", err)
		}
		if len(result.DuplicateCandidates) == 0 {
			t.Error("se esperaban candidatos de duplicado en el resultado")
		}
		if result.PatientID != uuid.Nil {
			t.Error("PatientID debería ser Nil cuando se retornan candidatos")
		}
	})

	t.Run("sin duplicados continúa el registro normalmente", func(t *testing.T) {
		repo := newMockPatientRepo()
		h := newRegisterHandler(repo)

		cmd := validRegisterCmd()
		cmd.SkipDuplicateCheck = false

		result, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.PatientID == uuid.Nil {
			t.Error("se esperaba PatientID válido")
		}
	})

	t.Run("error interno si el detector falla en ExistsByNationalID", func(t *testing.T) {
		repo := newMockPatientRepo()
		repo.existsByNationalErr = errors.New("db timeout")
		h := newRegisterHandler(repo)

		cmd := validRegisterCmd()
		cmd.SkipDuplicateCheck = false

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInternal {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInternal)
		}
	})

	t.Run("propaga error si repo.Save falla", func(t *testing.T) {
		repo := newMockPatientRepo()
		sentinel := errors.New("db down")
		repo.saveErr = sentinel
		h := newRegisterHandler(repo)

		_, err := h.Handle(context.Background(), validRegisterCmd())
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, sentinel)
		}
	})

	t.Run("error en eventBus.Publish no aborta el registro (solo warning)", func(t *testing.T) {
		repo := newMockPatientRepo()
		detector := service.NewDuplicateDetector(repo)
		h := command.NewRegisterPatientHandler(repo, detector, failEventBus{})

		result, err := h.Handle(context.Background(), validRegisterCmd())
		if err != nil {
			t.Fatalf("Handle() error = %v, el publish fallido no debe abortar", err)
		}
		if result.PatientID == uuid.Nil {
			t.Error("PatientID vacío")
		}
	})
}

// ── AddCoverageHandler ────────────────────────────────────────────

func newAddCoverageHandler(repo *mockPatientRepo, histRepo *mockCoverageHistoryRepo) *command.AddCoverageHandler {
	return command.NewAddCoverageHandler(repo, histRepo, noopEventBus{})
}

func validCoverageCmd(patientID sharedtypes.PatientID) command.AddCoverageCommand {
	return command.AddCoverageCommand{
		PatientID:    patientID,
		CoverageType: "Privado",
		ValidFrom:    time.Now().UTC(),
		AddedBy:      uuid.New(),
	}
}

func TestAddCoverageHandler(t *testing.T) {
	t.Run("agrega cobertura privada sin copago", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := newAddCoverageHandler(repo, &mockCoverageHistoryRepo{})

		covID, err := h.Handle(context.Background(), validCoverageCmd(p.ID()))
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if covID == uuid.Nil {
			t.Error("se esperaba un CoverageID válido")
		}
	})

	t.Run("agrega cobertura con copago en porcentaje", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := newAddCoverageHandler(repo, &mockCoverageHistoryRepo{})

		percent := 20
		cmd := validCoverageCmd(p.ID())
		cmd.CoPayPercent = &percent

		_, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("agrega cobertura con copago fijo en centavos", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := newAddCoverageHandler(repo, &mockCoverageHistoryRepo{})

		fixed := int64(5000)
		cmd := validCoverageCmd(p.ID())
		cmd.CoPayFixed = &fixed

		_, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("buildHistoryEntry incluye cobertura previa si existía", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)

		// Agregar una cobertura previa directamente al aggregate (tipo ExtPrepaid).
		prevCov, _ := coverage.NewPatientCoverage(
			p.ID(), valueobject.CoverageTypeExtPrepaid, nil, "OSDE",
			"310", "123456", time.Now(), nil, uuid.New(),
		)
		_ = p.AddCoverage(prevCov, uuid.New())
		p.PendingEvents()

		histRepo := &mockCoverageHistoryRepo{}
		h := newAddCoverageHandler(repo, histRepo)

		// Agregar una segunda cobertura de tipo distinto → hay cobertura previa
		cmd := command.AddCoverageCommand{
			PatientID:    p.ID(),
			CoverageType: "Privado",
			ValidFrom:    time.Now(),
			AddedBy:      uuid.New(),
		}

		_, err := h.Handle(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("falla si el paciente no existe", func(t *testing.T) {
		h := newAddCoverageHandler(newMockPatientRepo(), &mockCoverageHistoryRepo{})

		_, err := h.Handle(context.Background(), validCoverageCmd(uuid.New()))
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrNotFound)
		}
	})

	t.Run("falla con tipo de cobertura inválido", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := newAddCoverageHandler(repo, &mockCoverageHistoryRepo{})

		cmd := validCoverageCmd(p.ID())
		cmd.CoverageType = "TipoInexistente"

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla si validUntil es anterior a validFrom", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := newAddCoverageHandler(repo, &mockCoverageHistoryRepo{})

		pastDate := time.Now().Add(-24 * time.Hour)
		cmd := validCoverageCmd(p.ID())
		cmd.ValidUntil = &pastDate // anterior a ValidFrom (now)

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla si cobertura no-privada no tiene providerName", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := newAddCoverageHandler(repo, &mockCoverageHistoryRepo{})

		cmd := validCoverageCmd(p.ID())
		cmd.CoverageType = "PrepagaExterna" // requiere providerName
		cmd.ProviderName = ""               // faltante → error

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla si copago porcentaje está fuera de rango (>100)", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := newAddCoverageHandler(repo, &mockCoverageHistoryRepo{})

		invalidPct := 150
		cmd := validCoverageCmd(p.ID())
		cmd.CoPayPercent = &invalidPct

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla si copago fijo es negativo", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := newAddCoverageHandler(repo, &mockCoverageHistoryRepo{})

		negative := int64(-1)
		cmd := validCoverageCmd(p.ID())
		cmd.CoPayFixed = &negative

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla si ya existe cobertura activa del mismo tipo", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)

		// Agregar cobertura Privado directamente al aggregate.
		existing, _ := coverage.NewPatientCoverage(
			p.ID(), valueobject.CoverageTypePrivate, nil, "", "", "", time.Now(), nil, uuid.New(),
		)
		_ = p.AddCoverage(existing, uuid.New())
		p.PendingEvents()

		h := newAddCoverageHandler(repo, &mockCoverageHistoryRepo{})
		cmd := validCoverageCmd(p.ID()) // mismo tipo Privado

		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})

	t.Run("propaga error si repo.Update falla", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		sentinel := errors.New("db conflict")
		repo.updateErr = sentinel

		h := newAddCoverageHandler(repo, &mockCoverageHistoryRepo{})

		_, err := h.Handle(context.Background(), validCoverageCmd(p.ID()))
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, sentinel)
		}
	})

	t.Run("error en historyRepo.Append no falla la operación (solo warning)", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		histRepo := &mockCoverageHistoryRepo{appendErr: errors.New("history unavailable")}
		h := newAddCoverageHandler(repo, histRepo)

		_, err := h.Handle(context.Background(), validCoverageCmd(p.ID()))
		if err != nil {
			t.Errorf("Handle() error = %v, se esperaba nil (append error no debe abortar)", err)
		}
	})

	t.Run("error en eventBus.Publish no aborta AddCoverage (solo warning)", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewAddCoverageHandler(repo, &mockCoverageHistoryRepo{}, failEventBus{})

		_, err := h.Handle(context.Background(), validCoverageCmd(p.ID()))
		if err != nil {
			t.Errorf("Handle() error = %v, el publish fallido no debe abortar", err)
		}
	})
}

// ── AddMedicalAlertHandler ────────────────────────────────────────

func TestAddMedicalAlertHandler(t *testing.T) {
	t.Run("paciente reporta su propia alerta", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewAddMedicalAlertHandler(repo, noopEventBus{})

		alertID, err := h.Handle(context.Background(), command.AddMedicalAlertCommand{
			PatientID:     p.ID(),
			AlertType:     "Alergia",
			Description:   "Alergia a penicilina",
			IsSelfReported: true,
			RequestedBy:   uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if alertID == uuid.Nil {
			t.Error("se esperaba un AlertID válido")
		}
	})

	t.Run("staff agrega alerta con severidad crítica", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewAddMedicalAlertHandler(repo, noopEventBus{})

		alertID, err := h.Handle(context.Background(), command.AddMedicalAlertCommand{
			PatientID:   p.ID(),
			AlertType:   "RiesgoSangrado",
			Severity:    "Critical",
			Description: "Paciente anticoagulado con warfarina",
			RequestedBy: uuid.New(),
			RequestedRole: "recepcionista",
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if alertID == uuid.Nil {
			t.Error("se esperaba un AlertID válido")
		}
	})

	t.Run("falla si el paciente no existe", func(t *testing.T) {
		h := command.NewAddMedicalAlertHandler(newMockPatientRepo(), noopEventBus{})

		_, err := h.Handle(context.Background(), command.AddMedicalAlertCommand{
			PatientID:   uuid.New(),
			AlertType:   "Alergia",
			Description: "test",
			RequestedBy: uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con tipo de alerta inválido", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewAddMedicalAlertHandler(repo, noopEventBus{})

		_, err := h.Handle(context.Background(), command.AddMedicalAlertCommand{
			PatientID:   p.ID(),
			AlertType:   "TipoDesconocido",
			Description: "test",
			RequestedBy: uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("staff: falla con severidad inválida", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewAddMedicalAlertHandler(repo, noopEventBus{})

		_, err := h.Handle(context.Background(), command.AddMedicalAlertCommand{
			PatientID:     p.ID(),
			AlertType:     "Alergia",
			Severity:      "Extremo", // no válido
			Description:   "test",
			RequestedBy:   uuid.New(),
			RequestedRole: "profesional",
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("paciente: falla con descripción vacía", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewAddMedicalAlertHandler(repo, noopEventBus{})

		_, err := h.Handle(context.Background(), command.AddMedicalAlertCommand{
			PatientID:      p.ID(),
			AlertType:      "Alergia",
			Description:    "", // vacía
			IsSelfReported: true,
			RequestedBy:    uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("staff: falla con descripción vacía", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewAddMedicalAlertHandler(repo, noopEventBus{})

		_, err := h.Handle(context.Background(), command.AddMedicalAlertCommand{
			PatientID:     p.ID(),
			AlertType:     "Alergia",
			Severity:      "Warning",
			Description:   "", // vacía
			RequestedBy:   uuid.New(),
			RequestedRole: "profesional",
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("propaga error si repo.Update falla", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		sentinel := errors.New("db down")
		repo.updateErr = sentinel

		h := command.NewAddMedicalAlertHandler(repo, noopEventBus{})

		_, err := h.Handle(context.Background(), command.AddMedicalAlertCommand{
			PatientID:      p.ID(),
			AlertType:      "Alergia",
			Description:    "test",
			IsSelfReported: true,
			RequestedBy:    uuid.New(),
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, sentinel)
		}
	})

	t.Run("error en eventBus.Publish no aborta AddMedicalAlert (solo warning)", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewAddMedicalAlertHandler(repo, failEventBus{})

		_, err := h.Handle(context.Background(), command.AddMedicalAlertCommand{
			PatientID:      p.ID(),
			AlertType:      "Alergia",
			Description:    "Alergia a penicilina",
			IsSelfReported: true,
			RequestedBy:    uuid.New(),
		})
		if err != nil {
			t.Errorf("Handle() error = %v, el publish fallido no debe abortar", err)
		}
	})
}

// ── RecordCompletedVisitHandler ───────────────────────────────────

func TestRecordCompletedVisitHandler(t *testing.T) {
	t.Run("registra visita completada en el historial dental", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewRecordCompletedVisitHandler(repo)

		err := h.Handle(context.Background(), command.RecordCompletedVisitCommand{
			PatientID:      p.ID(),
			ProcedureCode:  "D1110",
			Description:    "Limpieza dental",
			PerformedAt:    time.Now(),
			ClinicID:       uuid.New(),
			ProfessionalID: uuid.New(),
			SourceEventID:  uuid.New().String(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("falla si el paciente no existe", func(t *testing.T) {
		h := command.NewRecordCompletedVisitHandler(newMockPatientRepo())

		err := h.Handle(context.Background(), command.RecordCompletedVisitCommand{
			PatientID:     uuid.New(),
			ProcedureCode: "D1110",
			PerformedAt:   time.Now(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("propaga error si repo.Update falla", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		sentinel := errors.New("db error")
		repo.updateErr = sentinel

		h := command.NewRecordCompletedVisitHandler(repo)

		err := h.Handle(context.Background(), command.RecordCompletedVisitCommand{
			PatientID:     p.ID(),
			ProcedureCode: "D1110",
			PerformedAt:   time.Now(),
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, sentinel)
		}
	})
}

// ── MergePatientsHandler ──────────────────────────────────────────

func TestMergePatientsHandler(t *testing.T) {
	t.Run("archiva el paciente fuente exitosamente", func(t *testing.T) {
		repo := newMockPatientRepo()
		h := command.NewMergePatientsHandler(repo, noopEventBus{})

		err := h.Handle(context.Background(), command.MergePatientsCommand{
			TargetPatientID: uuid.New(),
			SourcePatientID: uuid.New(),
			MergedBy:        uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("falla si target y source son el mismo paciente", func(t *testing.T) {
		h := command.NewMergePatientsHandler(newMockPatientRepo(), noopEventBus{})
		sameID := uuid.New()

		err := h.Handle(context.Background(), command.MergePatientsCommand{
			TargetPatientID: sameID,
			SourcePatientID: sameID,
			MergedBy:        uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInvalidArgument)
		}
	})

	t.Run("propaga error si repo.Archive falla", func(t *testing.T) {
		repo := newMockPatientRepo()
		sentinel := errors.New("archive failed")
		repo.archiveErr = sentinel

		h := command.NewMergePatientsHandler(repo, noopEventBus{})

		err := h.Handle(context.Background(), command.MergePatientsCommand{
			TargetPatientID: uuid.New(),
			SourcePatientID: uuid.New(),
			MergedBy:        uuid.New(),
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, sentinel)
		}
	})
}

// ── UpdateContactInfoHandler ──────────────────────────────────────

func TestUpdateContactInfoHandler(t *testing.T) {
	t.Run("actualiza todos los campos de contacto", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewUpdateContactInfoHandler(repo)

		err := h.Handle(context.Background(), command.UpdateContactInfoCommand{
			PatientID:      p.ID(),
			Phone:          validPhone,
			Email:          "nuevo@example.com",
			WhatsApp:       validPhone2,
			EmergencyName:  "María García",
			EmergencyPhone: validPhone2,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("actualiza solo el teléfono cuando los campos opcionales están vacíos", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewUpdateContactInfoHandler(repo)

		err := h.Handle(context.Background(), command.UpdateContactInfoCommand{
			PatientID: p.ID(),
			Phone:     validPhone,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("falla si el paciente no existe", func(t *testing.T) {
		h := command.NewUpdateContactInfoHandler(newMockPatientRepo())

		err := h.Handle(context.Background(), command.UpdateContactInfoCommand{
			PatientID: uuid.New(),
			Phone:     validPhone,
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con teléfono inválido", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewUpdateContactInfoHandler(repo)

		err := h.Handle(context.Background(), command.UpdateContactInfoCommand{
			PatientID: p.ID(),
			Phone:     "abc",
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con email inválido", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewUpdateContactInfoHandler(repo)

		err := h.Handle(context.Background(), command.UpdateContactInfoCommand{
			PatientID: p.ID(),
			Phone:     validPhone,
			Email:     "no-es-email",
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con WhatsApp inválido", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewUpdateContactInfoHandler(repo)

		err := h.Handle(context.Background(), command.UpdateContactInfoCommand{
			PatientID: p.ID(),
			Phone:     validPhone,
			WhatsApp:  "no-telefono",
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con teléfono de emergencia inválido", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		h := command.NewUpdateContactInfoHandler(repo)

		err := h.Handle(context.Background(), command.UpdateContactInfoCommand{
			PatientID:      p.ID(),
			Phone:          validPhone,
			EmergencyPhone: "mal-formato",
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("propaga error si repo.Update falla", func(t *testing.T) {
		repo := newMockPatientRepo()
		p := newPatient(t, repo)
		sentinel := errors.New("db down")
		repo.updateErr = sentinel

		h := command.NewUpdateContactInfoHandler(repo)

		err := h.Handle(context.Background(), command.UpdateContactInfoCommand{
			PatientID: p.ID(),
			Phone:     validPhone,
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, sentinel)
		}
	})
}
