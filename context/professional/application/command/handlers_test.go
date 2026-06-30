package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/professional/application/command"
	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/repository"
	"github.com/juantevez/odontoagenda/context/professional/domain/service"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── mocks ─────────────────────────────────────────────────────────

type mockProfRepo struct {
	profs             map[sharedtypes.ProfessionalID]*aggregate.Professional
	saveErr           error
	updateErr         error
	findByIDErr       error
	existsByNationalID bool
	existsErr         error
}

var _ repository.ProfessionalRepository = (*mockProfRepo)(nil)

func newMockProfRepo() *mockProfRepo {
	return &mockProfRepo{profs: make(map[sharedtypes.ProfessionalID]*aggregate.Professional)}
}

func (m *mockProfRepo) Save(_ context.Context, p *aggregate.Professional) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.profs[p.ID()] = p
	return nil
}
func (m *mockProfRepo) Update(_ context.Context, p *aggregate.Professional) error {
	if m.updateErr != nil {
		return m.updateErr
	}
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
	return nil, nil
}
func (m *mockProfRepo) FindBySpecialty(_ context.Context, _ valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	return nil, nil
}
func (m *mockProfRepo) FindAvailableAt(_ context.Context, _ sharedtypes.ClinicID, _ time.Time, _ *valueobject.SpecialtyCode) ([]*aggregate.Professional, error) {
	return nil, nil
}
func (m *mockProfRepo) FindWithExpiringLicenses(_ context.Context, _ int) ([]*aggregate.Professional, error) {
	return nil, nil
}
func (m *mockProfRepo) Search(_ context.Context, _ sharedtypes.ClinicID, _ string) ([]*aggregate.Professional, error) {
	return nil, nil
}
func (m *mockProfRepo) ExistsByNationalID(_ context.Context, _ string) (bool, error) {
	return m.existsByNationalID, m.existsErr
}

type noopBus struct{}

var _ pkgevents.Bus = (*noopBus)(nil)

func (noopBus) Publish(_ context.Context, _ pkgevents.DomainEvent) error { return nil }
func (noopBus) Subscribe(_ context.Context, _ pkgevents.SubscribeOptions, _ pkgevents.Handler) error {
	return nil
}
func (noopBus) Close() error { return nil }

type failBus struct{}

var _ pkgevents.Bus = (*failBus)(nil)

func (failBus) Publish(_ context.Context, _ pkgevents.DomainEvent) error {
	return errors.New("nats down")
}
func (failBus) Subscribe(_ context.Context, _ pkgevents.SubscribeOptions, _ pkgevents.Handler) error {
	return nil
}
func (failBus) Close() error { return nil }

// ── helpers ──────────────────────────────────────────────────────

func newTestProf(t *testing.T) *aggregate.Professional {
	t.Helper()
	name, _ := sharedvo.NewFullName("Dr. Juan Perez")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "12345678")
	email, _ := sharedvo.NewEmail("dr.perez@example.com")
	phone, _ := sharedvo.NewPhoneNumber("+5491112345678")
	p := aggregate.NewProfessional(nil, name, docID, email, phone, "Bio", nil)
	p.PendingEvents()
	return p
}

func saveProfInRepo(t *testing.T, repo *mockProfRepo, p *aggregate.Professional) {
	t.Helper()
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatalf("setup: Save: %v", err)
	}
}

// newTestProfWithLicense crea un profesional con una matrícula activa de odontología general.
// Necesario para poder llamar a AssignToClinic, que requiere matrícula activa.
func newTestProfWithLicense(t *testing.T) *aggregate.Professional {
	t.Helper()
	prof := newTestProf(t)
	sp, err := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Odontología General")
	if err != nil {
		t.Fatalf("setup: NewSpecialty: %v", err)
	}
	if err := prof.AddLicense(sp, "MP12345", "Colegio Odontológico", time.Now().Add(-365*24*time.Hour), nil, ""); err != nil {
		t.Fatalf("setup: AddLicense: %v", err)
	}
	prof.PendingEvents()
	return prof
}

// addClinicAssignment agrega una licencia si el prof no tiene ninguna, luego
// asigna a la sede con un horario dado y lo guarda en el repo.
func addClinicAssignment(t *testing.T, repo *mockProfRepo, prof *aggregate.Professional, clinicID sharedtypes.ClinicID, startH, endH int) uuid.UUID {
	t.Helper()
	// AssignToClinic requiere matrícula activa para la especialidad asignada.
	if len(prof.Licenses()) == 0 {
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Odontología General")
		_ = prof.AddLicense(sp, "MP12345", "Colegio", time.Now().Add(-365*24*time.Hour), nil, "")
		prof.PendingEvents()
	}
	ds, err := valueobject.NewDaySchedule(valueobject.Weekday(1), startH, 0, endH, 0)
	if err != nil {
		t.Fatalf("setup: NewDaySchedule: %v", err)
	}
	if err := prof.AssignToClinic(clinicID,
		[]valueobject.SpecialtyCode{valueobject.SpecialtyGeneralDentistry},
		[]valueobject.DaySchedule{ds},
		time.Now(), uuid.New(),
	); err != nil {
		t.Fatalf("setup: AssignToClinic: %v", err)
	}
	prof.PendingEvents()
	repo.profs[prof.ID()] = prof
	assignments := prof.ClinicAssignments()
	return assignments[len(assignments)-1].ID()
}

func domainCode(t *testing.T, err error) sharederrors.ErrorCode {
	t.Helper()
	de, ok := sharederrors.As(err)
	if !ok {
		t.Fatalf("se esperaba *DomainError, se obtuvo %T: %v", err, err)
	}
	return de.Code
}

func validRegisterCmd() command.RegisterProfessionalCommand {
	return command.RegisterProfessionalCommand{
		FullName:  "Dr. Ana Lopez",
		DocType:   "DNI",
		DocNumber: "87654321",
		Email:     "dra.lopez@example.com",
		Phone:     "+5491112345679",
		Bio:       "Odontóloga general",
	}
}

// ── RegisterProfessionalHandler ───────────────────────────────────

func TestRegisterProfessionalHandler(t *testing.T) {
	t.Run("registra un profesional válido", func(t *testing.T) {
		repo := newMockProfRepo()
		h := command.NewRegisterProfessionalHandler(repo, noopBus{})

		id, err := h.Handle(context.Background(), validRegisterCmd())
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if id == uuid.Nil {
			t.Error("se esperaba un ProfessionalID válido")
		}
	})

	t.Run("rechaza full_name inválido", func(t *testing.T) {
		h := command.NewRegisterProfessionalHandler(newMockProfRepo(), noopBus{})
		cmd := validRegisterCmd()
		cmd.FullName = "X"
		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza national_id inválido (número vacío)", func(t *testing.T) {
		h := command.NewRegisterProfessionalHandler(newMockProfRepo(), noopBus{})
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

	t.Run("rechaza email inválido", func(t *testing.T) {
		h := command.NewRegisterProfessionalHandler(newMockProfRepo(), noopBus{})
		cmd := validRegisterCmd()
		cmd.Email = "no-email"
		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza teléfono inválido", func(t *testing.T) {
		h := command.NewRegisterProfessionalHandler(newMockProfRepo(), noopBus{})
		cmd := validRegisterCmd()
		cmd.Phone = "abc"
		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza DNI ya existente", func(t *testing.T) {
		repo := newMockProfRepo()
		repo.existsByNationalID = true
		h := command.NewRegisterProfessionalHandler(repo, noopBus{})

		_, err := h.Handle(context.Background(), validRegisterCmd())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrAlreadyExists {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrAlreadyExists)
		}
	})

	t.Run("error interno si ExistsByNationalID falla", func(t *testing.T) {
		repo := newMockProfRepo()
		repo.existsErr = errors.New("db timeout")
		h := command.NewRegisterProfessionalHandler(repo, noopBus{})

		_, err := h.Handle(context.Background(), validRegisterCmd())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInternal {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("propaga error si repo.Save falla", func(t *testing.T) {
		repo := newMockProfRepo()
		sentinel := errors.New("db down")
		repo.saveErr = sentinel
		h := command.NewRegisterProfessionalHandler(repo, noopBus{})

		_, err := h.Handle(context.Background(), validRegisterCmd())
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, sentinel)
		}
	})

	t.Run("error en Publish no aborta el registro", func(t *testing.T) {
		h := command.NewRegisterProfessionalHandler(newMockProfRepo(), failBus{})
		id, err := h.Handle(context.Background(), validRegisterCmd())
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if id == uuid.Nil {
			t.Error("id vacío")
		}
	})
}

// ── AddLicenseHandler ─────────────────────────────────────────────

func TestAddLicenseHandler(t *testing.T) {
	t.Run("agrega una matrícula válida", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewAddLicenseHandler(repo, noopBus{})

		licID, err := h.Handle(context.Background(), command.AddLicenseCommand{
			ProfessionalID: prof.ID(),
			SpecialtyCode:  string(valueobject.SpecialtyGeneralDentistry),
			SpecialtyName:  "Odontología General",
			LicenseNumber:  "MP12345",
			IssuingBody:    "Colegio Médico",
			IssuedAt:       time.Now().Add(-365 * 24 * time.Hour),
			AddedBy:        uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if licID == uuid.Nil {
			t.Error("se esperaba un LicenseID válido")
		}
	})

	t.Run("falla si el profesional no existe", func(t *testing.T) {
		h := command.NewAddLicenseHandler(newMockProfRepo(), noopBus{})
		_, err := h.Handle(context.Background(), command.AddLicenseCommand{
			ProfessionalID: uuid.New(),
			SpecialtyCode:  string(valueobject.SpecialtyGeneralDentistry),
			AddedBy:        uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con specialty code inválido", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewAddLicenseHandler(repo, noopBus{})

		_, err := h.Handle(context.Background(), command.AddLicenseCommand{
			ProfessionalID: prof.ID(),
			SpecialtyCode:  "ESPECIALIDAD_INVALIDA",
			SpecialtyName:  "test",
			AddedBy:        uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla si ya existe matrícula activa para la misma especialidad", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		// Agregar una primera matrícula directamente.
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Odontología General")
		_ = prof.AddLicense(sp, "MP11111", "Colegio", time.Now().Add(-time.Hour), nil, "")
		prof.PendingEvents()
		saveProfInRepo(t, repo, prof)
		h := command.NewAddLicenseHandler(repo, noopBus{})

		_, err := h.Handle(context.Background(), command.AddLicenseCommand{
			ProfessionalID: prof.ID(),
			SpecialtyCode:  string(valueobject.SpecialtyGeneralDentistry),
			SpecialtyName:  "Odontología General",
			LicenseNumber:  "MP22222",
			IssuingBody:    "Colegio",
			IssuedAt:       time.Now(),
			AddedBy:        uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error por especialidad duplicada")
		}
	})

	t.Run("propaga error si repo.Update falla", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		sentinel := errors.New("db error")
		repo.updateErr = sentinel
		h := command.NewAddLicenseHandler(repo, noopBus{})

		_, err := h.Handle(context.Background(), command.AddLicenseCommand{
			ProfessionalID: prof.ID(),
			SpecialtyCode:  string(valueobject.SpecialtyGeneralDentistry),
			SpecialtyName:  "Odontología General",
			LicenseNumber:  "MP12345",
			IssuingBody:    "Colegio",
			IssuedAt:       time.Now(),
			AddedBy:        uuid.New(),
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("error en Publish no aborta la operación", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewAddLicenseHandler(repo, failBus{})

		_, err := h.Handle(context.Background(), command.AddLicenseCommand{
			ProfessionalID: prof.ID(),
			SpecialtyCode:  string(valueobject.SpecialtyGeneralDentistry),
			SpecialtyName:  "Odontología General",
			LicenseNumber:  "MP12345",
			IssuingBody:    "Colegio",
			IssuedAt:       time.Now(),
			AddedBy:        uuid.New(),
		})
		if err != nil {
			t.Errorf("Handle() error = %v, publish fallido no debe abortar", err)
		}
	})
}

// ── AssignToClinicHandler ─────────────────────────────────────────

func TestAssignToClinicHandler(t *testing.T) {
	checker := service.NewScheduleConflictChecker()

	validAssignCmd := func(profID sharedtypes.ProfessionalID) command.AssignToClinicCommand {
		return command.AssignToClinicCommand{
			ProfessionalID: profID,
			ClinicID:       sharedtypes.ClinicID(uuid.New()),
			Specialties:    []string{string(valueobject.SpecialtyGeneralDentistry)},
			WeeklySchedule: []command.DayScheduleInput{
				{Weekday: 1, StartHour: 9, StartMin: 0, EndHour: 13, EndMin: 0},
			},
			AssignedFrom: time.Now(),
			AssignedBy:   uuid.New(),
		}
	}

	t.Run("asigna a una sede exitosamente", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t) // necesita matrícula activa
		saveProfInRepo(t, repo, prof)
		h := command.NewAssignToClinicHandler(repo, checker, noopBus{})

		assignID, err := h.Handle(context.Background(), validAssignCmd(prof.ID()))
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if assignID == uuid.Nil {
			t.Error("se esperaba un AssignmentID válido")
		}
	})

	t.Run("falla si el profesional no existe", func(t *testing.T) {
		h := command.NewAssignToClinicHandler(newMockProfRepo(), checker, noopBus{})
		_, err := h.Handle(context.Background(), validAssignCmd(uuid.New()))
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con horario inválido", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewAssignToClinicHandler(repo, checker, noopBus{})

		cmd := validAssignCmd(prof.ID())
		cmd.WeeklySchedule = []command.DayScheduleInput{
			{Weekday: 1, StartHour: 17, StartMin: 0, EndHour: 9, EndMin: 0},
		}
		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con specialty code inválido", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewAssignToClinicHandler(repo, checker, noopBus{})

		cmd := validAssignCmd(prof.ID())
		cmd.Specialties = []string{"INVALIDA"}
		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con conflicto de horario entre sedes", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic1 := sharedtypes.ClinicID(uuid.New())
		// Pre-asignar a clinic1 con Lunes 09:00-17:00.
		addClinicAssignment(t, repo, prof, clinic1, 9, 17)
		h := command.NewAssignToClinicHandler(repo, checker, noopBus{})

		// Intentar asignar a clinic2 con Lunes 10:00-15:00 (solapa).
		cmd := command.AssignToClinicCommand{
			ProfessionalID: prof.ID(),
			ClinicID:       sharedtypes.ClinicID(uuid.New()),
			Specialties:    []string{string(valueobject.SpecialtyOrthodontics)},
			WeeklySchedule: []command.DayScheduleInput{
				{Weekday: 1, StartHour: 10, StartMin: 0, EndHour: 15, EndMin: 0},
			},
			AssignedFrom: time.Now(),
			AssignedBy:   uuid.New(),
		}
		_, err := h.Handle(context.Background(), cmd)
		if err == nil {
			t.Fatal("se esperaba error por conflicto de horario")
		}
		if code := domainCode(t, err); code != sharederrors.ErrConflict {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrConflict)
		}
	})

	t.Run("descripción resume varios conflictos cuando hay más de uno", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		clinic1 := sharedtypes.ClinicID(uuid.New())
		clinic2 := sharedtypes.ClinicID(uuid.New())
		// Asignar directamente (sin checker) a 2 sedes con el mismo horario Lunes 09-17.
		addClinicAssignment(t, repo, prof, clinic1, 9, 17)
		addClinicAssignment(t, repo, prof, clinic2, 9, 17)
		h := command.NewAssignToClinicHandler(repo, checker, noopBus{})

		// Proponer clinic3 Lunes 10-16 → solapa con clinic1 Y clinic2 → 2 conflictos.
		_, err := h.Handle(context.Background(), command.AssignToClinicCommand{
			ProfessionalID: prof.ID(),
			ClinicID:       sharedtypes.ClinicID(uuid.New()),
			Specialties:    []string{string(valueobject.SpecialtyGeneralDentistry)},
			WeeklySchedule: []command.DayScheduleInput{
				{Weekday: 1, StartHour: 10, StartMin: 0, EndHour: 16, EndMin: 0},
			},
			AssignedFrom: time.Now(),
			AssignedBy:   uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error por conflictos múltiples")
		}
		if code := domainCode(t, err); code != sharederrors.ErrConflict {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrConflict)
		}
	})

	t.Run("prof.AssignToClinic falla al asignar a la misma sede dos veces", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		// Primera asignación: sin handler (sin conflict check).
		addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewAssignToClinicHandler(repo, checker, noopBus{})

		// Segunda asignación a la MISMA sede en horario distinto (no hay conflicto
		// temporal, pero el aggregate rechaza dos asignaciones activas al mismo clinic).
		_, err := h.Handle(context.Background(), command.AssignToClinicCommand{
			ProfessionalID: prof.ID(),
			ClinicID:       clinic,
			Specialties:    []string{string(valueobject.SpecialtyGeneralDentistry)},
			WeeklySchedule: []command.DayScheduleInput{
				{Weekday: 3, StartHour: 14, StartMin: 0, EndHour: 18, EndMin: 0},
			},
			AssignedFrom: time.Now(),
			AssignedBy:   uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error: asignación duplicada para la misma sede")
		}
	})

	t.Run("propaga error si repo.Update falla", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		saveProfInRepo(t, repo, prof)
		sentinel := errors.New("db error")
		repo.updateErr = sentinel
		h := command.NewAssignToClinicHandler(repo, checker, noopBus{})

		_, err := h.Handle(context.Background(), validAssignCmd(prof.ID()))
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("error en Publish no aborta la asignación", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewAssignToClinicHandler(repo, checker, failBus{})

		_, err := h.Handle(context.Background(), validAssignCmd(prof.ID()))
		if err != nil {
			t.Errorf("Handle() error = %v", err)
		}
	})
}

// ── UpdateClinicScheduleHandler ───────────────────────────────────

func TestUpdateClinicScheduleHandler(t *testing.T) {
	checker := service.NewScheduleConflictChecker()

	t.Run("actualiza el horario de una sede exitosamente", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewUpdateClinicScheduleHandler(repo, checker, noopBus{})

		err := h.Handle(context.Background(), command.UpdateClinicScheduleCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   assignID,
			NewSchedule: []command.DayScheduleInput{
				{Weekday: 2, StartHour: 14, StartMin: 0, EndHour: 18, EndMin: 0},
			},
			UpdatedBy: uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("falla si el profesional no existe", func(t *testing.T) {
		h := command.NewUpdateClinicScheduleHandler(newMockProfRepo(), checker, noopBus{})
		err := h.Handle(context.Background(), command.UpdateClinicScheduleCommand{
			ProfessionalID: uuid.New(),
			AssignmentID:   uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("falla con horario inválido (start >= end)", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewUpdateClinicScheduleHandler(repo, checker, noopBus{})

		err := h.Handle(context.Background(), command.UpdateClinicScheduleCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   assignID,
			NewSchedule: []command.DayScheduleInput{
				{Weekday: 1, StartHour: 18, StartMin: 0, EndHour: 9, EndMin: 0},
			},
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con conflicto de horario contra otra sede", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		// Asignar a dos sedes con horarios distintos.
		clinic1 := sharedtypes.ClinicID(uuid.New())
		addClinicAssignment(t, repo, prof, clinic1, 9, 13) // Lunes 09-13
		clinic2 := sharedtypes.ClinicID(uuid.New())
		assignID2 := addClinicAssignment(t, repo, prof, clinic2, 14, 18) // Lunes 14-18

		h := command.NewUpdateClinicScheduleHandler(repo, checker, noopBus{})
		// Intentar actualizar clinic2 con Lunes 10-16 (solapa con clinic1).
		err := h.Handle(context.Background(), command.UpdateClinicScheduleCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   assignID2,
			NewSchedule: []command.DayScheduleInput{
				{Weekday: 1, StartHour: 10, StartMin: 0, EndHour: 16, EndMin: 0},
			},
		})
		if err == nil {
			t.Fatal("se esperaba error por conflicto")
		}
		if code := domainCode(t, err); code != sharederrors.ErrConflict {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrConflict)
		}
	})

	t.Run("falla si el assignment no existe en el profesional", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewUpdateClinicScheduleHandler(repo, checker, noopBus{})

		err := h.Handle(context.Background(), command.UpdateClinicScheduleCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   uuid.New(), // no existe
			NewSchedule:    []command.DayScheduleInput{{Weekday: 1, StartHour: 9, EndHour: 13}},
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("falla si el nuevo schedule está vacío (UpdateClinicSchedule rechaza schedules vacíos)", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewUpdateClinicScheduleHandler(repo, checker, noopBus{})

		// NewSchedule vacío → CheckScheduleUpdate pasa (sin conflictos), pero
		// prof.UpdateClinicSchedule falla porque UpdateWeeklySchedule requiere al menos un día.
		err := h.Handle(context.Background(), command.UpdateClinicScheduleCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   assignID,
			NewSchedule:    []command.DayScheduleInput{}, // vacío
		})
		if err == nil {
			t.Fatal("se esperaba error: schedule vacío no es válido")
		}
	})

	t.Run("propaga error si repo.Update falla", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		sentinel := errors.New("db down")
		repo.updateErr = sentinel
		h := command.NewUpdateClinicScheduleHandler(repo, checker, noopBus{})

		err := h.Handle(context.Background(), command.UpdateClinicScheduleCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   assignID,
			NewSchedule:    []command.DayScheduleInput{{Weekday: 2, StartHour: 14, EndHour: 18}},
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("error en Publish no aborta la operación", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewUpdateClinicScheduleHandler(repo, checker, failBus{})

		err := h.Handle(context.Background(), command.UpdateClinicScheduleCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   assignID,
			NewSchedule:    []command.DayScheduleInput{{Weekday: 2, StartHour: 14, EndHour: 18}},
		})
		if err != nil {
			t.Errorf("Handle() error = %v", err)
		}
	})
}

// ── AddExceptionHandler ───────────────────────────────────────────

func TestAddExceptionHandler(t *testing.T) {
	t.Run("agrega excepción de día libre (IsWorking=false)", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewAddExceptionHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.AddExceptionCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   assignID,
			Date:           time.Now().Add(24 * time.Hour),
			Reason:         "Congreso médico",
			IsWorking:      false,
			AddedBy:        uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("agrega excepción de horario especial (IsWorking=true)", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewAddExceptionHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.AddExceptionCommand{
			ProfessionalID:   prof.ID(),
			AssignmentID:     assignID,
			Date:             time.Now().Add(48 * time.Hour),
			Reason:           "Horario reducido",
			IsWorking:        true,
			SpecialStartHour: 10,
			SpecialEndHour:   12,
			AddedBy:          uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("falla si el profesional no existe", func(t *testing.T) {
		h := command.NewAddExceptionHandler(newMockProfRepo(), noopBus{})
		err := h.Handle(context.Background(), command.AddExceptionCommand{
			ProfessionalID: uuid.New(),
			Date:           time.Now().Add(24 * time.Hour),
			Reason:         "test",
			IsWorking:      false,
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("falla con IsWorking=true y horario especial inválido (start >= end)", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewAddExceptionHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.AddExceptionCommand{
			ProfessionalID:   prof.ID(),
			AssignmentID:     assignID,
			Date:             time.Now().Add(24 * time.Hour),
			Reason:           "test",
			IsWorking:        true,
			SpecialStartHour: 15, // start > end → inválido
			SpecialEndHour:   9,
			AddedBy:          uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla con motivo vacío (NewExceptionDay falla)", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewAddExceptionHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.AddExceptionCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   assignID,
			Date:           time.Now().Add(24 * time.Hour),
			Reason:         "", // vacío → error
			IsWorking:      false,
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla si el assignment no existe en el profesional", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewAddExceptionHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.AddExceptionCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   uuid.New(),
			Date:           time.Now().Add(24 * time.Hour),
			Reason:         "test",
			IsWorking:      false,
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("propaga error si repo.Update falla", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		sentinel := errors.New("db down")
		repo.updateErr = sentinel
		h := command.NewAddExceptionHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.AddExceptionCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   assignID,
			Date:           time.Now().Add(24 * time.Hour),
			Reason:         "test",
			IsWorking:      false,
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("error en Publish no aborta la operación", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewAddExceptionHandler(repo, failBus{})

		err := h.Handle(context.Background(), command.AddExceptionCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   assignID,
			Date:           time.Now().Add(24 * time.Hour),
			Reason:         "test",
			IsWorking:      false,
		})
		if err != nil {
			t.Errorf("Handle() error = %v", err)
		}
	})
}

// ── SetProcedureDurationHandler ───────────────────────────────────

func TestSetProcedureDurationHandler(t *testing.T) {
	t.Run("setea duración por defecto (AssignmentID=nil)", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewSetProcedureDurationHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.SetProcedureDurationCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   nil,
			ProcedureCode:  "D1110",
			Minutes:        30,
			BufferMinutes:  10,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		p := repo.profs[prof.ID()]
		if _, ok := p.DefaultDurations()["D1110"]; !ok {
			t.Error("duración por defecto no fue seteada")
		}
	})

	t.Run("setea duración específica de una sede (AssignmentID set)", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignID := addClinicAssignment(t, repo, prof, clinic, 9, 13)
		h := command.NewSetProcedureDurationHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.SetProcedureDurationCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   &assignID,
			ProcedureCode:  "D1110",
			Minutes:        45,
			BufferMinutes:  15,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})

	t.Run("falla si el profesional no existe", func(t *testing.T) {
		h := command.NewSetProcedureDurationHandler(newMockProfRepo(), noopBus{})
		err := h.Handle(context.Background(), command.SetProcedureDurationCommand{
			ProfessionalID: uuid.New(),
			ProcedureCode:  "D1110",
			Minutes:        30,
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("falla con duración inválida (minutos negativos)", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewSetProcedureDurationHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.SetProcedureDurationCommand{
			ProfessionalID: prof.ID(),
			ProcedureCode:  "D1110",
			Minutes:        -5,
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla si el assignment no existe al setear duración de sede", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		nonExistentID := uuid.New()
		h := command.NewSetProcedureDurationHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.SetProcedureDurationCommand{
			ProfessionalID: prof.ID(),
			AssignmentID:   &nonExistentID,
			ProcedureCode:  "D1110",
			Minutes:        30,
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("propaga error si repo.Update falla", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		sentinel := errors.New("db down")
		repo.updateErr = sentinel
		h := command.NewSetProcedureDurationHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.SetProcedureDurationCommand{
			ProfessionalID: prof.ID(),
			ProcedureCode:  "D1110",
			Minutes:        30,
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v", err)
		}
	})
}

// ── SuspendProfessionalHandler ────────────────────────────────────

func TestSuspendProfessionalHandler(t *testing.T) {
	t.Run("suspende un profesional activo", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewSuspendProfessionalHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.SuspendProfessionalCommand{
			ProfessionalID: prof.ID(),
			Reason:         "Sanción disciplinaria",
			SuspendedBy:    uuid.New(),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		p := repo.profs[prof.ID()]
		if p.Status().IsActive() {
			t.Error("el profesional debería estar suspendido")
		}
	})

	t.Run("falla si el profesional no existe", func(t *testing.T) {
		h := command.NewSuspendProfessionalHandler(newMockProfRepo(), noopBus{})
		err := h.Handle(context.Background(), command.SuspendProfessionalCommand{
			ProfessionalID: uuid.New(),
			Reason:         "test",
			SuspendedBy:    uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("falla si el profesional ya está suspendido", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		_ = prof.Suspend("primera vez", uuid.New())
		prof.PendingEvents()
		saveProfInRepo(t, repo, prof)
		h := command.NewSuspendProfessionalHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.SuspendProfessionalCommand{
			ProfessionalID: prof.ID(),
			Reason:         "segunda vez",
			SuspendedBy:    uuid.New(),
		})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("propaga error si repo.Update falla", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		sentinel := errors.New("db down")
		repo.updateErr = sentinel
		h := command.NewSuspendProfessionalHandler(repo, noopBus{})

		err := h.Handle(context.Background(), command.SuspendProfessionalCommand{
			ProfessionalID: prof.ID(),
			Reason:         "test",
			SuspendedBy:    uuid.New(),
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("error en Publish no aborta la suspensión", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		saveProfInRepo(t, repo, prof)
		h := command.NewSuspendProfessionalHandler(repo, failBus{})

		err := h.Handle(context.Background(), command.SuspendProfessionalCommand{
			ProfessionalID: prof.ID(),
			Reason:         "test",
			SuspendedBy:    uuid.New(),
		})
		if err != nil {
			t.Errorf("Handle() error = %v", err)
		}
	})
}
