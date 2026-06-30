package service_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/service"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── helpers ──────────────────────────────────────────────────────

func newTestProf(t *testing.T) *aggregate.Professional {
	t.Helper()
	name, _ := sharedvo.NewFullName("Dr. Test")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "12345678")
	email, _ := sharedvo.NewEmail("test@example.com")
	phone, _ := sharedvo.NewPhoneNumber("+5491112345678")
	p := aggregate.NewProfessional(nil, name, docID, email, phone, "", nil)
	p.PendingEvents()
	return p
}

func addLicense(t *testing.T, prof *aggregate.Professional, code valueobject.SpecialtyCode) {
	t.Helper()
	sp, _ := valueobject.NewSpecialty(code, "Test Specialty")
	if err := prof.AddLicense(sp, "MP12345", "Colegio", time.Now().Add(-365*24*time.Hour), nil, ""); err != nil {
		t.Fatalf("setup: AddLicense: %v", err)
	}
	prof.PendingEvents()
}

func addAssignment(t *testing.T, prof *aggregate.Professional, clinicID sharedtypes.ClinicID, weekday valueobject.Weekday, startH, endH int) *aggregate.ClinicAssignment {
	t.Helper()
	ds, _ := valueobject.NewDaySchedule(weekday, startH, 0, endH, 0)
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

// ── ScheduleConflictChecker ───────────────────────────────────────

func TestCheckNewAssignment(t *testing.T) {
	checker := service.NewScheduleConflictChecker()

	t.Run("sin asignaciones previas no hay conflicto", func(t *testing.T) {
		prof := newTestProf(t)
		addLicense(t, prof, valueobject.SpecialtyGeneralDentistry)
		ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 9, 0, 13, 0)

		conflicts, err := checker.CheckNewAssignment(prof, uuid.New(), []valueobject.DaySchedule{ds})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(conflicts) != 0 {
			t.Errorf("len(conflicts) = %d, se esperaba 0", len(conflicts))
		}
	})

	t.Run("horario en día distinto no conflicta", func(t *testing.T) {
		prof := newTestProf(t)
		addLicense(t, prof, valueobject.SpecialtyGeneralDentistry)
		clinic1 := sharedtypes.ClinicID(uuid.New())
		addAssignment(t, prof, clinic1, valueobject.Monday, 9, 13) // Lunes

		ds, _ := valueobject.NewDaySchedule(valueobject.Tuesday, 9, 0, 13, 0) // Martes
		conflicts, err := checker.CheckNewAssignment(prof, uuid.New(), []valueobject.DaySchedule{ds})
		if err != nil || len(conflicts) != 0 {
			t.Errorf("error = %v, conflicts = %d", err, len(conflicts))
		}
	})

	t.Run("horario solapado en mismo día genera conflicto", func(t *testing.T) {
		prof := newTestProf(t)
		addLicense(t, prof, valueobject.SpecialtyGeneralDentistry)
		clinic1 := sharedtypes.ClinicID(uuid.New())
		addAssignment(t, prof, clinic1, valueobject.Monday, 9, 17) // Lunes 09-17

		ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 10, 0, 14, 0) // Lunes 10-14 (solapa)
		conflicts, err := checker.CheckNewAssignment(prof, uuid.New(), []valueobject.DaySchedule{ds})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(conflicts) != 1 {
			t.Errorf("len(conflicts) = %d, se esperaba 1", len(conflicts))
		}
		if conflicts[0].ConflictingClinicID != clinic1 {
			t.Error("ConflictingClinicID no coincide")
		}
	})

	t.Run("misma sede es ignorada por el checker", func(t *testing.T) {
		prof := newTestProf(t)
		addLicense(t, prof, valueobject.SpecialtyGeneralDentistry)
		clinic1 := sharedtypes.ClinicID(uuid.New())
		addAssignment(t, prof, clinic1, valueobject.Monday, 9, 13)

		ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 9, 0, 13, 0)
		conflicts, err := checker.CheckNewAssignment(prof, clinic1, []valueobject.DaySchedule{ds})
		if err != nil || len(conflicts) != 0 {
			t.Errorf("misma sede no debería generar conflicto: err=%v conflicts=%d", err, len(conflicts))
		}
	})
}

func TestCheckScheduleUpdate(t *testing.T) {
	checker := service.NewScheduleConflictChecker()

	t.Run("assignment no encontrado retorna ErrNotFound", func(t *testing.T) {
		prof := newTestProf(t)
		ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 9, 0, 13, 0)

		_, err := checker.CheckScheduleUpdate(prof, uuid.New(), []valueobject.DaySchedule{ds})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("sin conflictos retorna lista vacía", func(t *testing.T) {
		prof := newTestProf(t)
		addLicense(t, prof, valueobject.SpecialtyGeneralDentistry)
		clinic1 := sharedtypes.ClinicID(uuid.New())
		ca := addAssignment(t, prof, clinic1, valueobject.Monday, 9, 13)

		ds, _ := valueobject.NewDaySchedule(valueobject.Tuesday, 14, 0, 18, 0)
		conflicts, err := checker.CheckScheduleUpdate(prof, ca.ID(), []valueobject.DaySchedule{ds})
		if err != nil || len(conflicts) != 0 {
			t.Errorf("error = %v, conflicts = %d", err, len(conflicts))
		}
	})

	t.Run("conflicto con otra sede retorna ConflictResult", func(t *testing.T) {
		prof := newTestProf(t)
		addLicense(t, prof, valueobject.SpecialtyGeneralDentistry)
		clinic1 := sharedtypes.ClinicID(uuid.New())
		addAssignment(t, prof, clinic1, valueobject.Monday, 9, 13)
		clinic2 := sharedtypes.ClinicID(uuid.New())
		ca2 := addAssignment(t, prof, clinic2, valueobject.Monday, 14, 18)

		// Intentar actualizar clinic2 con Lunes 10-16 → solapa con clinic1
		ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 10, 0, 16, 0)
		conflicts, err := checker.CheckScheduleUpdate(prof, ca2.ID(), []valueobject.DaySchedule{ds})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(conflicts) == 0 {
			t.Error("se esperaba al menos un conflicto")
		}
	})
}

func TestCheckSpecificDateTime(t *testing.T) {
	checker := service.NewScheduleConflictChecker()

	t.Run("sin asignaciones no hay conflicto", func(t *testing.T) {
		prof := newTestProf(t)
		_, hasConflict := checker.CheckSpecificDateTime(prof, uuid.New(), time.Now())
		if hasConflict {
			t.Error("se esperaba false")
		}
	})

	t.Run("fecha/hora dentro de una asignación activa devuelve conflicto", func(t *testing.T) {
		prof := newTestProf(t)
		addLicense(t, prof, valueobject.SpecialtyGeneralDentistry)
		clinic1 := sharedtypes.ClinicID(uuid.New())
		addAssignment(t, prof, clinic1, valueobject.Monday, 9, 17)

		// Buscar el próximo lunes a las 11:00.
		now := time.Now().UTC()
		daysUntilMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		monday := time.Date(now.Year(), now.Month(), now.Day()+daysUntilMon, 11, 0, 0, 0, time.UTC)

		conflictID, hasConflict := checker.CheckSpecificDateTime(prof, uuid.New(), monday)
		if !hasConflict {
			t.Fatal("se esperaba conflicto")
		}
		if conflictID == nil || *conflictID != clinic1 {
			t.Error("ConflictingClinicID no coincide")
		}
	})

	t.Run("asignación inactiva (Ended) es ignorada", func(t *testing.T) {
		prof := newTestProf(t)
		addLicense(t, prof, valueobject.SpecialtyGeneralDentistry)
		clinic1 := sharedtypes.ClinicID(uuid.New())
		ca := addAssignment(t, prof, clinic1, valueobject.Monday, 9, 17)
		// Terminar la asignación a través del aggregate para que afecte el slice interno.
		_ = prof.EndClinicAssignment(ca.ID(), time.Now().Add(24*time.Hour), uuid.New())

		now := time.Now().UTC()
		daysUntilMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		monday10 := time.Date(now.Year(), now.Month(), now.Day()+daysUntilMon, 11, 0, 0, 0, time.UTC)
		_, hasConflict := checker.CheckSpecificDateTime(prof, uuid.New(), monday10)
		if hasConflict {
			t.Error("asignación inactiva no debería generar conflicto")
		}
	})

	t.Run("exclude clinic no genera auto-conflicto", func(t *testing.T) {
		prof := newTestProf(t)
		addLicense(t, prof, valueobject.SpecialtyGeneralDentistry)
		clinic1 := sharedtypes.ClinicID(uuid.New())
		addAssignment(t, prof, clinic1, valueobject.Monday, 9, 17)

		now := time.Now().UTC()
		daysUntilMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		monday := time.Date(now.Year(), now.Month(), now.Day()+daysUntilMon, 11, 0, 0, 0, time.UTC)

		_, hasConflict := checker.CheckSpecificDateTime(prof, clinic1, monday) // excluir clinic1
		if hasConflict {
			t.Error("se esperaba false: clinic1 está excluido")
		}
	})
}

// ── LicenseExpirationChecker ──────────────────────────────────────

func TestLicenseExpirationChecker(t *testing.T) {
	checker := service.NewLicenseExpirationChecker()

	t.Run("sin matrículas retorna slice vacío", func(t *testing.T) {
		prof := newTestProf(t)
		reports := checker.Evaluate(prof)
		if len(reports) != 0 {
			t.Errorf("len(reports) = %d, se esperaba 0", len(reports))
		}
	})

	t.Run("matrícula sin fecha de expiración → NoExpiry", func(t *testing.T) {
		prof := newTestProf(t)
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Test")
		_ = prof.AddLicense(sp, "MP111", "Colegio", time.Now().Add(-time.Hour), nil, "") // nil = sin vencimiento
		prof.PendingEvents()

		reports := checker.Evaluate(prof)
		if len(reports) != 1 || reports[0].Status != service.ExpirationStatusNoExpiry {
			t.Errorf("se esperaba NoExpiry, got %+v", reports)
		}
	})

	t.Run("matrícula expirada → Expired con DaysRemaining negativo", func(t *testing.T) {
		prof := newTestProf(t)
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Test")
		past := time.Now().Add(-48 * time.Hour)
		_ = prof.AddLicense(sp, "MP111", "Colegio", time.Now().Add(-365*24*time.Hour), &past, "")
		prof.PendingEvents()

		reports := checker.Evaluate(prof)
		if len(reports) != 1 || reports[0].Status != service.ExpirationStatusExpired {
			t.Errorf("se esperaba Expired, got %v", reports[0].Status)
		}
		if reports[0].DaysRemaining >= 0 {
			t.Errorf("DaysRemaining = %d, se esperaba negativo", reports[0].DaysRemaining)
		}
	})

	t.Run("matrícula que vence en ≤30 días → ExpiringSoon", func(t *testing.T) {
		prof := newTestProf(t)
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Test")
		soon := time.Now().Add(15 * 24 * time.Hour)
		_ = prof.AddLicense(sp, "MP111", "Colegio", time.Now().Add(-365*24*time.Hour), &soon, "")
		prof.PendingEvents()

		reports := checker.Evaluate(prof)
		if reports[0].Status != service.ExpirationStatusExpiring {
			t.Errorf("se esperaba ExpiringSoon, got %v", reports[0].Status)
		}
	})

	t.Run("matrícula que vence en >30 días → Valid", func(t *testing.T) {
		prof := newTestProf(t)
		sp, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Test")
		future := time.Now().Add(90 * 24 * time.Hour)
		_ = prof.AddLicense(sp, "MP111", "Colegio", time.Now().Add(-365*24*time.Hour), &future, "")
		prof.PendingEvents()

		reports := checker.Evaluate(prof)
		if reports[0].Status != service.ExpirationStatusValid {
			t.Errorf("se esperaba Valid, got %v", reports[0].Status)
		}
	})
}
