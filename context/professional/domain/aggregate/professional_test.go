package aggregate_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── helpers ──────────────────────────────────────────────────────

func newProf(t *testing.T) *aggregate.Professional {
	t.Helper()
	name, _ := sharedvo.NewFullName("Dr. Juan Perez")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "12345678")
	email, _ := sharedvo.NewEmail("dr@example.com")
	phone, _ := sharedvo.NewPhoneNumber("+5491112345678")
	p := aggregate.NewProfessional(nil, name, docID, email, phone, "Bio", nil)
	p.PendingEvents()
	return p
}

func addGeneralLicense(t *testing.T, prof *aggregate.Professional) {
	t.Helper()
	sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Odontología General")
	if err := prof.AddLicense(sp, "MP12345", "Colegio", time.Now().Add(-365*24*time.Hour), nil, "doc.pdf"); err != nil {
		t.Fatalf("setup: AddLicense: %v", err)
	}
	prof.PendingEvents()
}

func assignClinic(t *testing.T, prof *aggregate.Professional, clinicID sharedtypes.ClinicID) *aggregate.ClinicAssignment {
	t.Helper()
	ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 9, 0, 13, 0)
	if err := prof.AssignToClinic(clinicID,
		[]valueobject.SpecialtyCode{valueobject.SpecialtyGeneralDentistry},
		[]valueobject.DaySchedule{ds},
		time.Now(), uuid.New(),
	); err != nil {
		t.Fatalf("setup: AssignToClinic: %v", err)
	}
	prof.PendingEvents()
	assignments := prof.ClinicAssignments()
	ca := assignments[len(assignments)-1]
	return &ca
}

func domainCode(t *testing.T, err error) sharederrors.ErrorCode {
	t.Helper()
	de, ok := sharederrors.As(err)
	if !ok {
		t.Fatalf("se esperaba *DomainError, se obtuvo %T: %v", err, err)
	}
	return de.Code
}

// ── NewProfessional ───────────────────────────────────────────────

func TestNewProfessional(t *testing.T) {
	t.Run("crea con los campos correctos", func(t *testing.T) {
		p := newProf(t)
		if p.ID() == (sharedtypes.ProfessionalID{}) {
			t.Error("ID vacío")
		}
		if p.Bio() != "Bio" {
			t.Errorf("Bio = %q", p.Bio())
		}
		if p.Status() != valueobject.ProfessionalStatusActive {
			t.Errorf("Status = %v, se esperaba Active", p.Status())
		}
		if p.Version() != 1 {
			t.Errorf("Version = %d, se esperaba 1", p.Version())
		}
	})

	t.Run("agrega evento ProfessionalRegistered", func(t *testing.T) {
		name, _ := sharedvo.NewFullName("Dr. Test")
		docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "99999999")
		email, _ := sharedvo.NewEmail("test@example.com")
		phone, _ := sharedvo.NewPhoneNumber("+5491112345679")
		p := aggregate.NewProfessional(nil, name, docID, email, phone, "", nil)

		evts := p.PendingEvents()
		if len(evts) != 1 {
			t.Fatalf("len(events) = %d, se esperaba 1", len(evts))
		}
		if evts[0].EventType() != "professional.registered" {
			t.Errorf("EventType = %q", evts[0].EventType())
		}
	})

	t.Run("PendingEvents limpia el slice", func(t *testing.T) {
		p := newProf(t)
		aggregate.NewProfessional(nil, p.FullName(), p.NationalID(), p.Email(), p.Phone(), "", nil).PendingEvents()
		// newProf already calls PendingEvents, so create fresh:
		name, _ := sharedvo.NewFullName("Dr. Otro")
		docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "11111111")
		email, _ := sharedvo.NewEmail("otro@example.com")
		phone, _ := sharedvo.NewPhoneNumber("+5491112345680")
		p2 := aggregate.NewProfessional(nil, name, docID, email, phone, "", nil)
		p2.PendingEvents()
		if evts := p2.PendingEvents(); len(evts) != 0 {
			t.Errorf("segunda llamada debería retornar slice vacío, obtuvo %d", len(evts))
		}
	})
}

// ── ProfessionalLicense.IsCurrentlyValid ──────────────────────────

func TestIsCurrentlyValid(t *testing.T) {
	t.Run("activa sin expiración es válida", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		l := p.Licenses()[0]
		if !l.IsCurrentlyValid() {
			t.Error("se esperaba IsCurrentlyValid() = true")
		}
	})

	t.Run("activa con expiración futura es válida", func(t *testing.T) {
		p := newProf(t)
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Test")
		future := time.Now().Add(365 * 24 * time.Hour)
		_ = p.AddLicense(sp, "MP1", "Colegio", time.Now().Add(-time.Hour), &future, "")
		l := p.Licenses()[0]
		if !l.IsCurrentlyValid() {
			t.Error("se esperaba true")
		}
	})

	t.Run("activa con expiración pasada no es válida", func(t *testing.T) {
		p := newProf(t)
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Test")
		past := time.Now().Add(-time.Hour)
		_ = p.AddLicense(sp, "MP1", "Colegio", time.Now().Add(-48*time.Hour), &past, "")
		l := p.Licenses()[0]
		if l.IsCurrentlyValid() {
			t.Error("se esperaba false")
		}
	})
}

// ── AddLicense ────────────────────────────────────────────────────

func TestAddLicense(t *testing.T) {
	t.Run("agrega matrícula exitosamente", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		if len(p.Licenses()) != 1 {
			t.Errorf("len(Licenses) = %d", len(p.Licenses()))
		}
		if p.Licenses()[0].LicenseNumber() != "MP12345" {
			t.Errorf("LicenseNumber = %q", p.Licenses()[0].LicenseNumber())
		}
	})

	t.Run("rechaza matrícula duplicada para la misma especialidad activa", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Odontología General")
		err := p.AddLicense(sp, "MP99999", "Otro", time.Now(), nil, "")
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("emite evento ProfessionalLicenseAdded", func(t *testing.T) {
		p := newProf(t)
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Test")
		_ = p.AddLicense(sp, "MP1", "Colegio", time.Now(), nil, "")
		evts := p.PendingEvents()
		if len(evts) == 0 || evts[0].EventType() != "professional.license.added" {
			t.Errorf("se esperaba evento 'professional.license.added', got %+v", evts)
		}
	})
}

// ── RevokeLicense ─────────────────────────────────────────────────

func TestRevokeLicense(t *testing.T) {
	t.Run("revoca una matrícula activa", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		licID := p.Licenses()[0].ID()

		err := p.RevokeLicense(licID, "Sanción", uuid.New())
		if err != nil {
			t.Fatalf("RevokeLicense() error = %v", err)
		}
		if p.Licenses()[0].Status() != valueobject.LicenseStatusRevoked {
			t.Error("Status debería ser Revoked")
		}
	})

	t.Run("falla si la matrícula no existe", func(t *testing.T) {
		p := newProf(t)
		err := p.RevokeLicense(uuid.New(), "test", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla si la matrícula ya está revocada", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		licID := p.Licenses()[0].ID()
		_ = p.RevokeLicense(licID, "primera", uuid.New())

		err := p.RevokeLicense(licID, "segunda", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v", code)
		}
	})
}

// ── HasValidLicenseFor / ActiveSpecialties ─────────────────────────

func TestHasValidLicenseFor(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)

	if !p.HasValidLicenseFor(valueobject.SpecialtyGeneralDentistry) {
		t.Error("debería tener licencia válida")
	}
	if p.HasValidLicenseFor(valueobject.SpecialtyOrthodontics) {
		t.Error("no debería tener licencia para Ortodoncia")
	}
}

func TestActiveSpecialties(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)
	sp2, _ := valueobject.NewSpecialty(valueobject.SpecialtyOrthodontics, "Ortodoncia")
	_ = p.AddLicense(sp2, "MP99", "Colegio", time.Now().Add(-time.Hour), nil, "")
	p.PendingEvents()

	active := p.ActiveSpecialties()
	if len(active) != 2 {
		t.Errorf("len(ActiveSpecialties) = %d, se esperaba 2", len(active))
	}
}

// ── AssignToClinic ────────────────────────────────────────────────

func TestAssignToClinic(t *testing.T) {
	t.Run("asigna a una sede exitosamente", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())
		if ca.ID() == uuid.Nil {
			t.Error("AssignmentID vacío")
		}
		if ca.Status() != valueobject.AssignmentStatusActive {
			t.Errorf("Status = %v, se esperaba Active", ca.Status())
		}
	})

	t.Run("rechaza asignación duplicada para la misma sede", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignClinic(t, p, clinic)

		ds, _ := valueobject.NewDaySchedule(valueobject.Tuesday, 14, 0, 18, 0)
		err := p.AssignToClinic(clinic,
			[]valueobject.SpecialtyCode{valueobject.SpecialtyGeneralDentistry},
			[]valueobject.DaySchedule{ds}, time.Now(), uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza si no hay matrícula activa para la especialidad", func(t *testing.T) {
		p := newProf(t) // sin licencias
		ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 9, 0, 13, 0)
		err := p.AssignToClinic(uuid.New(),
			[]valueobject.SpecialtyCode{valueobject.SpecialtyGeneralDentistry},
			[]valueobject.DaySchedule{ds}, time.Now(), uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v", code)
		}
	})
}

// ── EndClinicAssignment ───────────────────────────────────────────

func TestEndClinicAssignment(t *testing.T) {
	t.Run("termina una asignación activa", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())

		err := p.EndClinicAssignment(ca.ID(), time.Now().Add(24*time.Hour), uuid.New())
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		updated := p.ClinicAssignments()
		if updated[0].Status() != valueobject.AssignmentStatusEnded {
			t.Errorf("Status = %v, se esperaba Ended", updated[0].Status())
		}
	})

	t.Run("falla si la asignación no existe", func(t *testing.T) {
		p := newProf(t)
		err := p.EndClinicAssignment(uuid.New(), time.Now(), uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("falla si ya está terminada", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())
		_ = p.EndClinicAssignment(ca.ID(), time.Now().Add(24*time.Hour), uuid.New())

		err := p.EndClinicAssignment(ca.ID(), time.Now().Add(24*time.Hour), uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v", code)
		}
	})
}

// ── FindAssignmentForClinic ───────────────────────────────────────

func TestFindAssignmentForClinic(t *testing.T) {
	t.Run("encuentra asignación activa", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignClinic(t, p, clinic)

		ca, found := p.FindAssignmentForClinic(clinic)
		if !found || ca == nil {
			t.Error("se esperaba found=true")
		}
	})

	t.Run("retorna false para sede sin asignación", func(t *testing.T) {
		p := newProf(t)
		_, found := p.FindAssignmentForClinic(uuid.New())
		if found {
			t.Error("se esperaba found=false")
		}
	})
}

// ── AddExceptionToClinic ──────────────────────────────────────────

func TestAddExceptionToClinic(t *testing.T) {
	t.Run("agrega excepción exitosamente", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())

		exc, _ := valueobject.NewExceptionDay(time.Now().Add(24*time.Hour), "Feriado", false, nil)
		if err := p.AddExceptionToClinic(ca.ID(), exc); err != nil {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("falla si la asignación no existe", func(t *testing.T) {
		p := newProf(t)
		exc, _ := valueobject.NewExceptionDay(time.Now().Add(24*time.Hour), "test", false, nil)
		err := p.AddExceptionToClinic(uuid.New(), exc)
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
}

// ── UpdateClinicSchedule ──────────────────────────────────────────

func TestUpdateClinicSchedule(t *testing.T) {
	t.Run("actualiza el horario correctamente", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())

		newSchedule, _ := valueobject.NewDaySchedule(valueobject.Tuesday, 14, 0, 18, 0)
		err := p.UpdateClinicSchedule(ca.ID(), []valueobject.DaySchedule{newSchedule})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("falla con schedule vacío", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())

		err := p.UpdateClinicSchedule(ca.ID(), []valueobject.DaySchedule{})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("falla si la asignación no existe", func(t *testing.T) {
		p := newProf(t)
		ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 9, 0, 13, 0)
		err := p.UpdateClinicSchedule(uuid.New(), []valueobject.DaySchedule{ds})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
}

// ── SetDefaultDuration / SetClinicDuration / GetDuration ──────────

func TestDurations(t *testing.T) {
	t.Run("SetDefaultDuration actualiza el mapa de defaults", func(t *testing.T) {
		p := newProf(t)
		dur, _ := valueobject.NewProcedureDuration("D1110", 30, 5)
		p.SetDefaultDuration(dur)

		if _, ok := p.DefaultDurations()["D1110"]; !ok {
			t.Error("D1110 no encontrado en DefaultDurations")
		}
	})

	t.Run("SetClinicDuration agrega override a la asignación", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())

		dur, _ := valueobject.NewProcedureDuration("D1110", 45, 10)
		if err := p.SetClinicDuration(ca.ID(), dur); err != nil {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("SetClinicDuration falla si la asignación no existe", func(t *testing.T) {
		p := newProf(t)
		dur, _ := valueobject.NewProcedureDuration("D1110", 30, 0)
		err := p.SetClinicDuration(uuid.New(), dur)
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("GetDurationForProcedureAtClinic usa override de sede si existe", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		clinic := sharedtypes.ClinicID(uuid.New())
		ca := assignClinic(t, p, clinic)

		defaultDur, _ := valueobject.NewProcedureDuration("D1110", 30, 0)
		p.SetDefaultDuration(defaultDur)

		overrideDur, _ := valueobject.NewProcedureDuration("D1110", 60, 0)
		_ = p.SetClinicDuration(ca.ID(), overrideDur)

		got, found := p.GetDurationForProcedureAtClinic(clinic, "D1110")
		if !found {
			t.Fatal("se esperaba found=true")
		}
		if got.Minutes != 60 {
			t.Errorf("Minutes = %d, se esperaba 60 (override de sede)", got.Minutes)
		}
	})

	t.Run("GetDurationForProcedureAtClinic usa default cuando no hay override", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignClinic(t, p, clinic)

		defaultDur, _ := valueobject.NewProcedureDuration("D9999", 20, 0)
		p.SetDefaultDuration(defaultDur)

		got, found := p.GetDurationForProcedureAtClinic(clinic, "D9999")
		if !found {
			t.Fatal("se esperaba found=true (fallback a default)")
		}
		if got.Minutes != 20 {
			t.Errorf("Minutes = %d, se esperaba 20", got.Minutes)
		}
	})
}

// ── Suspend / Activate ────────────────────────────────────────────

func TestSuspendActivate(t *testing.T) {
	t.Run("suspende un profesional activo", func(t *testing.T) {
		p := newProf(t)
		if err := p.Suspend("sanción", uuid.New()); err != nil {
			t.Fatalf("Suspend() error = %v", err)
		}
		if p.Status() != valueobject.ProfessionalStatusSuspended {
			t.Errorf("Status = %v", p.Status())
		}
		evts := p.PendingEvents()
		if len(evts) == 0 || evts[0].EventType() != "professional.suspended" {
			t.Errorf("se esperaba evento 'professional.suspended', got %+v", evts)
		}
	})

	t.Run("no puede suspenderse si ya está suspendido", func(t *testing.T) {
		p := newProf(t)
		_ = p.Suspend("primera", uuid.New())
		p.PendingEvents()

		err := p.Suspend("segunda", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("reactiva un profesional suspendido", func(t *testing.T) {
		p := newProf(t)
		_ = p.Suspend("test", uuid.New())
		p.PendingEvents()

		if err := p.Activate(uuid.New()); err != nil {
			t.Fatalf("Activate() error = %v", err)
		}
		if !p.Status().IsActive() {
			t.Error("debería estar activo")
		}
	})

	t.Run("no puede activarse si ya está activo", func(t *testing.T) {
		p := newProf(t)
		err := p.Activate(uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v", code)
		}
	})
}

// ── CanAttendAtClinic ─────────────────────────────────────────────

func TestCanAttendAtClinic(t *testing.T) {
	t.Run("profesional suspendido no puede atender", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		clinic := sharedtypes.ClinicID(uuid.New())
		assignClinic(t, p, clinic)
		_ = p.Suspend("test", uuid.New())

		if p.CanAttendAtClinic(clinic, time.Now()) {
			t.Error("profesional suspendido no debería poder atender")
		}
	})

	t.Run("retorna false si no tiene asignación activa en esa sede", func(t *testing.T) {
		p := newProf(t)
		if p.CanAttendAtClinic(uuid.New(), time.Now()) {
			t.Error("sin asignación no puede atender")
		}
	})
}

// ── ClinicAssignment methods ──────────────────────────────────────

func TestClinicAssignmentMethods(t *testing.T) {
	t.Run("GetDurationForProcedure: not found retorna false", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())
		_, found := ca.GetDurationForProcedure("UNKNOWN")
		if found {
			t.Error("se esperaba found=false para procedimiento sin duración")
		}
	})

	t.Run("SetDurationForProcedure guarda la duración", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())
		dur, _ := valueobject.NewProcedureDuration("D1110", 30, 5)
		ca.SetDurationForProcedure(dur)
		got, found := ca.GetDurationForProcedure("D1110")
		if !found || got.Minutes != 30 {
			t.Errorf("found=%v, Minutes=%d", found, got.Minutes)
		}
	})

	t.Run("AddException agrega una excepción al ClinicAssignment", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())
		exc, _ := valueobject.NewExceptionDay(time.Now().Add(24*time.Hour), "test", false, nil)
		if err := ca.AddException(exc); err != nil {
			t.Fatalf("AddException() error = %v", err)
		}
		if len(ca.ExceptionDays()) != 1 {
			t.Errorf("len(ExceptionDays) = %d, se esperaba 1", len(ca.ExceptionDays()))
		}
	})

	t.Run("RemoveException elimina la excepción", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())
		date := time.Now().Add(48 * time.Hour)
		exc, _ := valueobject.NewExceptionDay(date, "test", false, nil)
		_ = ca.AddException(exc)

		if err := ca.RemoveException(date); err != nil {
			t.Fatalf("RemoveException() error = %v", err)
		}
		if len(ca.ExceptionDays()) != 0 {
			t.Error("se esperaba que la excepción fuera eliminada")
		}
	})

	t.Run("RemoveException falla si no existe", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())
		err := ca.RemoveException(time.Now().Add(24 * time.Hour))
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("End termina el ClinicAssignment", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())
		if err := ca.End(time.Now().Add(24*time.Hour), uuid.New()); err != nil {
			t.Fatalf("End() error = %v", err)
		}
		if ca.Status() != valueobject.AssignmentStatusEnded {
			t.Errorf("Status = %v", ca.Status())
		}
	})

	t.Run("End falla si ya está terminado", func(t *testing.T) {
		p := newProf(t)
		addGeneralLicense(t, p)
		ca := assignClinic(t, p, uuid.New())
		_ = ca.End(time.Now().Add(24*time.Hour), uuid.New())
		if err := ca.End(time.Now().Add(24*time.Hour), uuid.New()); err == nil {
			t.Fatal("se esperaba error")
		}
	})
}

// ── ReconstituteLicense ───────────────────────────────────────────

func TestReconstituteLicense(t *testing.T) {
	t.Run("reconstruye una matrícula desde persistencia", func(t *testing.T) {
		p := newProf(t)
		id := uuid.New()
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Test")
		issuedAt := time.Now().Add(-365 * 24 * time.Hour)

		p.ReconstituteLicense(id, sp, "MP99", "Colegio", issuedAt, nil, valueobject.LicenseStatusActive, "doc.pdf")

		if len(p.Licenses()) != 1 {
			t.Fatalf("len(Licenses) = %d", len(p.Licenses()))
		}
		l := p.Licenses()[0]
		if l.ID() != id {
			t.Errorf("ID = %v", l.ID())
		}
		if l.Status() != valueobject.LicenseStatusActive {
			t.Errorf("Status = %v", l.Status())
		}
		if l.DocumentRef() != "doc.pdf" {
			t.Errorf("DocumentRef = %q", l.DocumentRef())
		}
	})
}

// ── Reconstitute ──────────────────────────────────────────────────

func TestReconstitute(t *testing.T) {
	name, _ := sharedvo.NewFullName("Dr. Reconstituido")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "77777777")
	email, _ := sharedvo.NewEmail("rec@example.com")
	phone, _ := sharedvo.NewPhoneNumber("+5491100000001")

	p := aggregate.Reconstitute(
		uuid.New(), nil, name, docID, email, phone,
		"Bio", valueobject.ProfessionalStatusSuspended,
		nil, nil, nil, time.Now(), time.Now(), nil, 5,
	)

	if p.Status() != valueobject.ProfessionalStatusSuspended {
		t.Errorf("Status = %v, se esperaba Suspended", p.Status())
	}
	if p.Version() != 5 {
		t.Errorf("Version = %d, se esperaba 5", p.Version())
	}
	if evts := p.PendingEvents(); len(evts) != 0 {
		t.Errorf("Reconstitute no debe generar eventos, obtuvo %d", len(evts))
	}
}

// ── ClinicAssignment getters y IsAvailableAt ──────────────────────

func TestClinicAssignmentGetters(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)
	clinic := sharedtypes.ClinicID(uuid.New())
	ca := assignClinic(t, p, clinic)

	if ca.ClinicID() != clinic {
		t.Error("ClinicID no coincide")
	}
	if len(ca.AssignedSpecialties()) == 0 {
		t.Error("AssignedSpecialties debería tener al menos 1")
	}
	if len(ca.WeeklySchedule()) == 0 {
		t.Error("WeeklySchedule debería tener al menos 1")
	}
	if ca.ProcedureDurations() == nil {
		t.Error("ProcedureDurations no debería ser nil")
	}
	if ca.AssignedFrom().IsZero() {
		t.Error("AssignedFrom cero")
	}
	if ca.AssignedUntil() != nil {
		t.Error("AssignedUntil debería ser nil para asignación activa sin fecha fin")
	}
	if ca.AssignedBy() == (uuid.UUID{}) {
		t.Error("AssignedBy vacío")
	}
	if ca.CreatedAt().IsZero() {
		t.Error("CreatedAt cero")
	}
	if ca.UpdatedAt().IsZero() {
		t.Error("UpdatedAt cero")
	}
}

func TestIsAvailableAt(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)
	clinic := sharedtypes.ClinicID(uuid.New())
	ca := assignClinic(t, p, clinic) // Lunes 09:00-13:00

	// Calcular el próximo lunes.
	now := time.Now().UTC()
	daysUntilMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
	monday10 := time.Date(now.Year(), now.Month(), now.Day()+daysUntilMon, 10, 0, 0, 0, time.UTC)
	monday15 := time.Date(now.Year(), now.Month(), now.Day()+daysUntilMon, 15, 0, 0, 0, time.UTC)

	t.Run("disponible en horario regular", func(t *testing.T) {
		if !ca.IsAvailableAt(monday10) {
			t.Error("debería estar disponible lunes 10:00")
		}
	})

	t.Run("no disponible fuera de horario", func(t *testing.T) {
		if ca.IsAvailableAt(monday15) {
			t.Error("no debería estar disponible lunes 15:00")
		}
	})

	t.Run("excepción de día libre → no disponible", func(t *testing.T) {
		exc, _ := valueobject.NewExceptionDay(monday10, "Feriado", false, nil)
		_ = ca.AddException(exc)
		if ca.IsAvailableAt(monday10) {
			t.Error("excepción de día libre debería impedir disponibilidad")
		}
	})

	t.Run("assignment inactivo → no disponible", func(t *testing.T) {
		p2 := newProf(t)
		addGeneralLicense(t, p2)
		ca2 := assignClinic(t, p2, uuid.New())
		_ = ca2.End(time.Now().Add(24*time.Hour), uuid.New())
		if ca2.IsAvailableAt(monday10) {
			t.Error("assignment terminado no debería estar disponible")
		}
	})
}

func TestAddExceptionDuplicate(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)
	ca := assignClinic(t, p, uuid.New())
	date := time.Now().Add(72 * time.Hour)
	exc, _ := valueobject.NewExceptionDay(date, "primera", false, nil)
	_ = ca.AddException(exc)

	// Segunda excepción para la misma fecha → ErrPrecondition.
	exc2, _ := valueobject.NewExceptionDay(date, "segunda", false, nil)
	err := ca.AddException(exc2)
	if err == nil {
		t.Fatal("se esperaba error por excepción duplicada")
	}
	if code := domainCode(t, err); code != sharederrors.ErrAlreadyExists {
		t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrAlreadyExists)
	}
}

// ── Professional.UpdatedAt ─────────────────────────────────────────

func TestProfessionalUpdatedAt(t *testing.T) {
	p := newProf(t)
	if p.UpdatedAt().IsZero() {
		t.Error("UpdatedAt no debería ser cero")
	}
}

// ── Casos adicionales para aumentar cobertura ─────────────────────

func TestIsCurrentlyValidRevoked(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)
	licID := p.Licenses()[0].ID()
	_ = p.RevokeLicense(licID, "test", uuid.New())
	// Licencia revocada → IsCurrentlyValid = false por status != Active.
	if p.Licenses()[0].IsCurrentlyValid() {
		t.Error("licencia revocada debería ser IsCurrentlyValid=false")
	}
}

func TestCanAttendReturnsTrue(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)
	clinic := sharedtypes.ClinicID(uuid.New())
	assignClinic(t, p, clinic)

	now := time.Now().UTC()
	daysUntilMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
	monday10 := time.Date(now.Year(), now.Month(), now.Day()+daysUntilMon, 10, 0, 0, 0, time.UTC)

	if !p.CanAttendAtClinic(clinic, monday10) {
		t.Error("CanAttendAtClinic debería ser true: profesional activo, horario válido")
	}
}

func TestGetDurationNotFound(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)
	clinic := sharedtypes.ClinicID(uuid.New())
	assignClinic(t, p, clinic)

	_, found := p.GetDurationForProcedureAtClinic(clinic, "D_SIN_DURACION")
	if found {
		t.Error("debería retornar false cuando no hay duración definida")
	}
}

func TestAssignedUntilAfterEnd(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)
	ca := assignClinic(t, p, uuid.New())
	until := time.Now().Add(30 * 24 * time.Hour)
	_ = ca.End(until, uuid.New())

	if ca.AssignedUntil() == nil {
		t.Fatal("AssignedUntil debería estar seteado tras End")
	}
	if !ca.AssignedUntil().Equal(until) {
		t.Errorf("AssignedUntil = %v, se esperaba %v", ca.AssignedUntil(), until)
	}
}

func TestIsAvailableAtAfterEnd(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)
	ca := assignClinic(t, p, uuid.New())

	// Terminamos con una fecha un mes en el futuro, luego verificamos
	// que IsAvailableAt en fecha posterior a until retorna false.
	until := time.Now().Add(7 * 24 * time.Hour)
	_ = ca.End(until, uuid.New())

	// Fecha dentro del rango pero ya terminado (status Ended) → false.
	now := time.Now().UTC()
	daysUntilMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
	monday10 := time.Date(now.Year(), now.Month(), now.Day()+daysUntilMon, 10, 0, 0, 0, time.UTC)
	if ca.IsAvailableAt(monday10) {
		t.Error("assignment terminado no debería estar disponible")
	}
}

// ── Getters faltantes y paths adicionales ────────────────────────

func TestLicenseGetters(t *testing.T) {
	p := newProf(t)
	sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyOrthodontics, "Ortodoncia")
	future := time.Now().Add(365 * 24 * time.Hour)
	_ = p.AddLicense(sp, "MP999", "Colegio", time.Now().Add(-time.Hour), &future, "doc.pdf")
	l := p.Licenses()[0]

	if l.Specialty().Code != valueobject.SpecialtyOrthodontics {
		t.Errorf("Specialty.Code = %v", l.Specialty().Code)
	}
	if l.IssuingBody() != "Colegio" {
		t.Errorf("IssuingBody = %q", l.IssuingBody())
	}
	if l.IssuedAt().IsZero() {
		t.Error("IssuedAt cero")
	}
	if l.ExpiresAt() == nil {
		t.Error("ExpiresAt debería estar seteado")
	}
}

func TestIsAvailableAtAssignedFromFuture(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)

	// Asignar desde 48h en el futuro → cualquier fecha pasada debería estar NO disponible.
	ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 9, 0, 17, 0)
	_ = p.AssignToClinic(uuid.New(),
		[]valueobject.SpecialtyCode{valueobject.SpecialtyGeneralDentistry},
		[]valueobject.DaySchedule{ds},
		time.Now().Add(48*time.Hour), uuid.New(),
	)

	assignments := p.ClinicAssignments()
	ca := &assignments[0]

	// Lunes 6-ene-2020: date en el pasado, en día correcto, en horario correcto.
	// assignedFrom (now+48h) > 2020 → assignedFrom.After(pastDate) = true.
	pastMonday10 := time.Date(2020, 1, 6, 10, 0, 0, 0, time.UTC)

	if ca.IsAvailableAt(pastMonday10) {
		t.Error("no debería estar disponible: assignedFrom está en el futuro respecto a pastMonday10")
	}
}

func TestAddExceptionToClinicErrorPropagation(t *testing.T) {
	p := newProf(t)
	addGeneralLicense(t, p)
	clinic := sharedtypes.ClinicID(uuid.New())
	ca := assignClinic(t, p, clinic)

	date := time.Now().Add(72 * time.Hour)
	exc, _ := valueobject.NewExceptionDay(date, "primera", false, nil)
	// Primera llamada: debe tener éxito.
	if err := p.AddExceptionToClinic(ca.ID(), exc); err != nil {
		t.Fatalf("primera AddExceptionToClinic() error = %v", err)
	}

	// Segunda llamada con la misma fecha → AddException falla por duplicado.
	exc2, _ := valueobject.NewExceptionDay(date, "segunda", false, nil)
	err := p.AddExceptionToClinic(ca.ID(), exc2)
	if err == nil {
		t.Fatal("se esperaba error por excepción duplicada en AddExceptionToClinic")
	}
}

// ── Getters smoke test ────────────────────────────────────────────

func TestProfessionalGetters(t *testing.T) {
	userID := uuid.New()
	p := aggregate.NewProfessional(&userID, func() sharedvo.FullName {
		n, _ := sharedvo.NewFullName("Dr. Getters")
		return n
	}(), func() sharedvo.NationalID {
		id, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "55555555")
		return id
	}(), func() sharedvo.Email {
		e, _ := sharedvo.NewEmail("getters@example.com")
		return e
	}(), func() sharedvo.PhoneNumber {
		ph, _ := sharedvo.NewPhoneNumber("+5491100000000")
		return ph
	}(), "Bio test", nil)
	p.PendingEvents()

	if p.UserID() == nil || *p.UserID() != userID {
		t.Error("UserID no coincide")
	}
	if p.FullName().String() != "Dr. Getters" {
		t.Errorf("FullName = %q", p.FullName().String())
	}
	if p.Bio() != "Bio test" {
		t.Errorf("Bio = %q", p.Bio())
	}
	if p.CreatedAt().IsZero() {
		t.Error("CreatedAt cero")
	}
	if p.CreatedBy() != nil {
		t.Error("CreatedBy debería ser nil")
	}
}
