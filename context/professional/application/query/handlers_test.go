package query_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/professional/application/query"
	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/repository"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
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

// ── helpers ──────────────────────────────────────────────────────

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

func addClinicAssignment(t *testing.T, repo *mockProfRepo, prof *aggregate.Professional, clinicID sharedtypes.ClinicID, startH, endH int) uuid.UUID {
	t.Helper()
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

// ── GetProfessionalByIDHandler ────────────────────────────────────

func TestGetProfessionalByIDHandler(t *testing.T) {
	t.Run("retorna DTO completo para un profesional existente", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		_ = repo.Save(context.Background(), prof)
		h := query.NewGetProfessionalByIDHandler(repo)

		dto, err := h.Handle(context.Background(), query.GetProfessionalByIDQuery{ProfessionalID: prof.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if dto.ID != prof.ID().String() {
			t.Errorf("ID = %q, se esperaba %q", dto.ID, prof.ID().String())
		}
		if dto.FullName != prof.FullName().String() {
			t.Errorf("FullName = %q, se esperaba %q", dto.FullName, prof.FullName().String())
		}
		if dto.Email != prof.Email().String() {
			t.Errorf("Email = %q, se esperaba %q", dto.Email, prof.Email().String())
		}
		if dto.Bio != prof.Bio() {
			t.Errorf("Bio = %q, se esperaba %q", dto.Bio, prof.Bio())
		}
	})

	t.Run("mapea matrículas al DTO correctamente", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		_ = repo.Save(context.Background(), prof)
		h := query.NewGetProfessionalByIDHandler(repo)

		dto, err := h.Handle(context.Background(), query.GetProfessionalByIDQuery{ProfessionalID: prof.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dto.Licenses) != 1 {
			t.Fatalf("Licenses len = %d, se esperaba 1", len(dto.Licenses))
		}
		lic := dto.Licenses[0]
		if lic.SpecialtyCode != string(valueobject.SpecialtyGeneralDentistry) {
			t.Errorf("SpecialtyCode = %q", lic.SpecialtyCode)
		}
		if lic.LicenseNumber != "MP12345" {
			t.Errorf("LicenseNumber = %q", lic.LicenseNumber)
		}
		if !lic.IsValid {
			t.Error("IsValid debería ser true para una matrícula vigente")
		}
		if lic.ExpiresAt != nil {
			t.Error("ExpiresAt debería ser nil para matrícula sin vencimiento")
		}
	})

	t.Run("mapea asignaciones de sede al DTO correctamente", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinicID := sharedtypes.ClinicID(uuid.New())
		addClinicAssignment(t, repo, prof, clinicID, 9, 17)
		h := query.NewGetProfessionalByIDHandler(repo)

		dto, err := h.Handle(context.Background(), query.GetProfessionalByIDQuery{ProfessionalID: prof.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dto.Assignments) != 1 {
			t.Fatalf("Assignments len = %d, se esperaba 1", len(dto.Assignments))
		}
		a := dto.Assignments[0]
		if a.ClinicID != clinicID.String() {
			t.Errorf("ClinicID = %q, se esperaba %q", a.ClinicID, clinicID.String())
		}
		if len(a.WeeklySchedule) != 1 {
			t.Fatalf("WeeklySchedule len = %d, se esperaba 1", len(a.WeeklySchedule))
		}
		ws := a.WeeklySchedule[0]
		if ws.StartHour != 9 || ws.EndHour != 17 {
			t.Errorf("horario = %d-%d, se esperaba 9-17", ws.StartHour, ws.EndHour)
		}
	})

	t.Run("retorna error NotFound cuando el profesional no existe", func(t *testing.T) {
		h := query.NewGetProfessionalByIDHandler(newMockProfRepo())

		_, err := h.Handle(context.Background(), query.GetProfessionalByIDQuery{ProfessionalID: uuid.New()})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v, se esperaba ErrNotFound", code)
		}
	})

	t.Run("propaga error del repositorio", func(t *testing.T) {
		repo := newMockProfRepo()
		sentinel := errors.New("db timeout")
		repo.findByIDErr = sentinel
		h := query.NewGetProfessionalByIDHandler(repo)

		_, err := h.Handle(context.Background(), query.GetProfessionalByIDQuery{ProfessionalID: uuid.New()})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, sentinel)
		}
	})
}

// ── GetProfessionalForSchedulingHandler ──────────────────────────

func TestGetProfessionalForSchedulingHandler(t *testing.T) {
	t.Run("retorna DTO de scheduling con asignaciones activas", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinicID := sharedtypes.ClinicID(uuid.New())
		addClinicAssignment(t, repo, prof, clinicID, 9, 17)
		h := query.NewGetProfessionalForSchedulingHandler(repo)

		dto, err := h.Handle(context.Background(), query.GetProfessionalForSchedulingQuery{ProfessionalID: prof.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if dto.ProfessionalID != prof.ID().String() {
			t.Errorf("ProfessionalID = %q, se esperaba %q", dto.ProfessionalID, prof.ID().String())
		}
		if dto.FullName != prof.FullName().String() {
			t.Errorf("FullName = %q, se esperaba %q", dto.FullName, prof.FullName().String())
		}
		if !dto.IsActive {
			t.Error("IsActive debería ser true")
		}
		if len(dto.ClinicAssignments) != 1 {
			t.Fatalf("ClinicAssignments len = %d, se esperaba 1", len(dto.ClinicAssignments))
		}
		ca := dto.ClinicAssignments[0]
		if ca.ClinicID != clinicID.String() {
			t.Errorf("ClinicID = %q, se esperaba %q", ca.ClinicID, clinicID.String())
		}
	})

	t.Run("mapea especialidades activas", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		_ = repo.Save(context.Background(), prof)
		h := query.NewGetProfessionalForSchedulingHandler(repo)

		dto, err := h.Handle(context.Background(), query.GetProfessionalForSchedulingQuery{ProfessionalID: prof.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dto.ActiveSpecialties) != 1 {
			t.Fatalf("ActiveSpecialties len = %d, se esperaba 1", len(dto.ActiveSpecialties))
		}
		if dto.ActiveSpecialties[0] != string(valueobject.SpecialtyGeneralDentistry) {
			t.Errorf("ActiveSpecialties[0] = %q", dto.ActiveSpecialties[0])
		}
	})

	t.Run("excluye asignaciones inactivas del DTO", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		_ = repo.Save(context.Background(), prof)
		h := query.NewGetProfessionalForSchedulingHandler(repo)

		dto, err := h.Handle(context.Background(), query.GetProfessionalForSchedulingQuery{ProfessionalID: prof.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dto.ClinicAssignments) != 0 {
			t.Errorf("ClinicAssignments len = %d, se esperaba 0 para prof sin asignaciones", len(dto.ClinicAssignments))
		}
	})

	t.Run("mapea horario semanal de la asignación", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		clinicID := sharedtypes.ClinicID(uuid.New())
		addClinicAssignment(t, repo, prof, clinicID, 8, 14)
		h := query.NewGetProfessionalForSchedulingHandler(repo)

		dto, err := h.Handle(context.Background(), query.GetProfessionalForSchedulingQuery{ProfessionalID: prof.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		ca := dto.ClinicAssignments[0]
		if len(ca.WeeklySchedule) != 1 {
			t.Fatalf("WeeklySchedule len = %d, se esperaba 1", len(ca.WeeklySchedule))
		}
		ws := ca.WeeklySchedule[0]
		if ws.StartHour != 8 || ws.EndHour != 14 {
			t.Errorf("horario = %d-%d, se esperaba 8-14", ws.StartHour, ws.EndHour)
		}
	})

	t.Run("retorna error NotFound cuando el profesional no existe", func(t *testing.T) {
		h := query.NewGetProfessionalForSchedulingHandler(newMockProfRepo())

		_, err := h.Handle(context.Background(), query.GetProfessionalForSchedulingQuery{ProfessionalID: uuid.New()})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v, se esperaba ErrNotFound", code)
		}
	})

	t.Run("propaga error del repositorio", func(t *testing.T) {
		repo := newMockProfRepo()
		sentinel := errors.New("db timeout")
		repo.findByIDErr = sentinel
		h := query.NewGetProfessionalForSchedulingHandler(repo)

		_, err := h.Handle(context.Background(), query.GetProfessionalForSchedulingQuery{ProfessionalID: uuid.New()})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, sentinel)
		}
	})
}

// ── FindByClinicHandler ───────────────────────────────────────────

func TestFindByClinicHandler(t *testing.T) {
	clinicID := sharedtypes.ClinicID(uuid.New())

	t.Run("retorna lista de DTOs por sede sin filtro", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		repo.findByClinicResult = []*aggregate.Professional{prof}
		h := query.NewFindByClinicHandler(repo)

		dtos, err := h.Handle(context.Background(), query.FindByClinicQuery{ClinicID: clinicID})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dtos) != 1 {
			t.Fatalf("len = %d, se esperaba 1", len(dtos))
		}
		if dtos[0].ID != prof.ID().String() {
			t.Errorf("ID = %q, se esperaba %q", dtos[0].ID, prof.ID().String())
		}
	})

	t.Run("filtra por especialidad cuando Specialty está seteado", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		repo.findByClinicResult = []*aggregate.Professional{prof}
		h := query.NewFindByClinicHandler(repo)

		sp := string(valueobject.SpecialtyGeneralDentistry)
		dtos, err := h.Handle(context.Background(), query.FindByClinicQuery{
			ClinicID:  clinicID,
			Specialty: &sp,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dtos) != 1 {
			t.Fatalf("len = %d, se esperaba 1", len(dtos))
		}
	})

	t.Run("usa Search cuando Q está seteado", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		repo.searchResult = []*aggregate.Professional{prof}
		h := query.NewFindByClinicHandler(repo)

		dtos, err := h.Handle(context.Background(), query.FindByClinicQuery{
			ClinicID: clinicID,
			Q:        "Perez",
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dtos) != 1 {
			t.Fatalf("len = %d, se esperaba 1", len(dtos))
		}
		if dtos[0].ID != prof.ID().String() {
			t.Errorf("ID = %q, se esperaba %q", dtos[0].ID, prof.ID().String())
		}
	})

	t.Run("Q tiene precedencia sobre Specialty al elegir la estrategia", func(t *testing.T) {
		// Cuando Q != "" se ignora Specialty y se llama Search.
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		repo.searchResult = []*aggregate.Professional{prof}
		// findByClinicResult queda vacío: si el handler llamara FindByClinic en lugar de Search, retornaría 0 elementos.
		h := query.NewFindByClinicHandler(repo)

		sp := string(valueobject.SpecialtyGeneralDentistry)
		dtos, err := h.Handle(context.Background(), query.FindByClinicQuery{
			ClinicID:  clinicID,
			Q:         "General",
			Specialty: &sp,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dtos) != 1 {
			t.Fatalf("len = %d, se esperaba 1 (desde searchResult)", len(dtos))
		}
	})

	t.Run("retorna lista vacía cuando no hay resultados", func(t *testing.T) {
		repo := newMockProfRepo()
		repo.findByClinicResult = []*aggregate.Professional{}
		h := query.NewFindByClinicHandler(repo)

		dtos, err := h.Handle(context.Background(), query.FindByClinicQuery{ClinicID: clinicID})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dtos) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(dtos))
		}
	})

	t.Run("propaga error de FindByClinic", func(t *testing.T) {
		repo := newMockProfRepo()
		sentinel := errors.New("db error")
		repo.findByClinicErr = sentinel
		h := query.NewFindByClinicHandler(repo)

		_, err := h.Handle(context.Background(), query.FindByClinicQuery{ClinicID: clinicID})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba %v", err, sentinel)
		}
	})

	t.Run("propaga error de Search", func(t *testing.T) {
		repo := newMockProfRepo()
		sentinel := errors.New("search error")
		repo.searchErr = sentinel
		h := query.NewFindByClinicHandler(repo)

		_, err := h.Handle(context.Background(), query.FindByClinicQuery{
			ClinicID: clinicID,
			Q:        "algo",
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba %v", err, sentinel)
		}
	})
}

// ── FindAvailableAtHandler ────────────────────────────────────────

func TestFindAvailableAtHandler(t *testing.T) {
	clinicID := sharedtypes.ClinicID(uuid.New())
	at := time.Now().Add(24 * time.Hour)

	t.Run("retorna lista de DTOs de scheduling para disponibilidad", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		addClinicAssignment(t, repo, prof, clinicID, 9, 17)
		repo.availableResult = []*aggregate.Professional{prof}
		h := query.NewFindAvailableAtHandler(repo)

		dtos, err := h.Handle(context.Background(), query.FindAvailableAtQuery{
			ClinicID: clinicID,
			At:       at,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dtos) != 1 {
			t.Fatalf("len = %d, se esperaba 1", len(dtos))
		}
		if dtos[0].ProfessionalID != prof.ID().String() {
			t.Errorf("ProfessionalID = %q, se esperaba %q", dtos[0].ProfessionalID, prof.ID().String())
		}
	})

	t.Run("filtra por especialidad cuando Specialty está seteado", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProfWithLicense(t)
		repo.availableResult = []*aggregate.Professional{prof}
		h := query.NewFindAvailableAtHandler(repo)

		sp := string(valueobject.SpecialtyGeneralDentistry)
		dtos, err := h.Handle(context.Background(), query.FindAvailableAtQuery{
			ClinicID:  clinicID,
			At:        at,
			Specialty: &sp,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dtos) != 1 {
			t.Fatalf("len = %d, se esperaba 1", len(dtos))
		}
		if dtos[0].ProfessionalID != prof.ID().String() {
			t.Errorf("ProfessionalID = %q", dtos[0].ProfessionalID)
		}
	})

	t.Run("retorna lista vacía cuando no hay profesionales disponibles", func(t *testing.T) {
		repo := newMockProfRepo()
		repo.availableResult = []*aggregate.Professional{}
		h := query.NewFindAvailableAtHandler(repo)

		dtos, err := h.Handle(context.Background(), query.FindAvailableAtQuery{
			ClinicID: clinicID,
			At:       at,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dtos) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(dtos))
		}
	})

	t.Run("propaga error del repositorio", func(t *testing.T) {
		repo := newMockProfRepo()
		sentinel := errors.New("db timeout")
		repo.availableErr = sentinel
		h := query.NewFindAvailableAtHandler(repo)

		_, err := h.Handle(context.Background(), query.FindAvailableAtQuery{
			ClinicID: clinicID,
			At:       at,
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, se esperaba %v", err, sentinel)
		}
	})

	t.Run("mapea asignaciones activas en el DTO de scheduling", func(t *testing.T) {
		repo := newMockProfRepo()
		prof := newTestProf(t)
		addClinicAssignment(t, repo, prof, clinicID, 10, 16)
		repo.availableResult = []*aggregate.Professional{prof}
		h := query.NewFindAvailableAtHandler(repo)

		dtos, err := h.Handle(context.Background(), query.FindAvailableAtQuery{
			ClinicID: clinicID,
			At:       at,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(dtos[0].ClinicAssignments) != 1 {
			t.Fatalf("ClinicAssignments len = %d, se esperaba 1", len(dtos[0].ClinicAssignments))
		}
		ws := dtos[0].ClinicAssignments[0].WeeklySchedule
		if len(ws) != 1 || ws[0].StartHour != 10 || ws[0].EndHour != 16 {
			t.Errorf("WeeklySchedule = %+v, se esperaba StartHour=10 EndHour=16", ws)
		}
	})
}
