package valueobject_test

import (
	"testing"
	"time"

	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
)

// ── SpecialtyCode ─────────────────────────────────────────────────

func TestSpecialtyCodeIsValid(t *testing.T) {
	valid := []valueobject.SpecialtyCode{
		valueobject.SpecialtyGeneralDentistry, valueobject.SpecialtyPediatricDentistry,
		valueobject.SpecialtyOrthodontics, valueobject.SpecialtyEndodontics,
		valueobject.SpecialtyPeriodontics, valueobject.SpecialtyImplantology,
		valueobject.SpecialtyOralSurgery, valueobject.SpecialtyMaxillofacial,
		valueobject.SpecialtyOralRehabilitation, valueobject.SpecialtyAesthetics,
		valueobject.SpecialtyStomatology, valueobject.SpecialtyRadiology,
	}
	for _, c := range valid {
		t.Run(string(c)+" valid", func(t *testing.T) {
			if !c.IsValid() {
				t.Errorf("IsValid() = false para %q", c)
			}
		})
	}
	t.Run("inválido retorna false", func(t *testing.T) {
		if valueobject.SpecialtyCode("INEXISTENTE").IsValid() {
			t.Error("se esperaba false")
		}
	})
}

func TestSpecialtyCodeDisplayName(t *testing.T) {
	if got := valueobject.SpecialtyGeneralDentistry.DisplayName(); got != "Odontología General" {
		t.Errorf("DisplayName() = %q", got)
	}
	// Código desconocido devuelve el string literal.
	unknown := valueobject.SpecialtyCode("UNKNOWN")
	if got := unknown.DisplayName(); got != "UNKNOWN" {
		t.Errorf("DisplayName() = %q para código desconocido", got)
	}
}

func TestSpecialtyCodeString(t *testing.T) {
	if got := valueobject.SpecialtyOrthodontics.String(); got != "ORTODONCIA" {
		t.Errorf("String() = %q", got)
	}
}

// ── NewSpecialty ──────────────────────────────────────────────────

func TestNewSpecialty(t *testing.T) {
	t.Run("código y nombre válidos", func(t *testing.T) {
		sp, err := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "Odontología General")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if sp.Code != valueobject.SpecialtyGeneralDentistry {
			t.Errorf("Code = %v", sp.Code)
		}
	})
	t.Run("código inválido falla", func(t *testing.T) {
		_, err := valueobject.NewSpecialty("INVALIDO", "test")
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
	t.Run("displayName vacío falla", func(t *testing.T) {
		_, err := valueobject.NewSpecialty(valueobject.SpecialtyOrthodontics, "   ")
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
}

func TestSpecialtyEquals(t *testing.T) {
	sp1, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "A")
	sp2, _ := valueobject.NewSpecialty(valueobject.SpecialtyGeneralDentistry, "B")
	sp3, _ := valueobject.NewSpecialty(valueobject.SpecialtyOrthodontics, "C")

	if !sp1.Equals(sp2) {
		t.Error("misma especialidad distinto nombre debería ser igual")
	}
	if sp1.Equals(sp3) {
		t.Error("distintas especialidades deberían ser distintas")
	}
}

// ── ProfessionalStatus ────────────────────────────────────────────

func TestParseProfessionalStatus(t *testing.T) {
	cases := []struct {
		input   string
		want    valueobject.ProfessionalStatus
		wantErr bool
	}{
		{"Active", valueobject.ProfessionalStatusActive, false},
		{"Inactive", valueobject.ProfessionalStatusInactive, false},
		{"Suspended", valueobject.ProfessionalStatusSuspended, false},
		{"Inexistente", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := valueobject.ParseProfessionalStatus(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("se esperaba error para %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if got != tc.want {
				t.Errorf("got = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}

func TestProfessionalStatusIsActive(t *testing.T) {
	if !valueobject.ProfessionalStatusActive.IsActive() {
		t.Error("Active debería ser IsActive=true")
	}
	if valueobject.ProfessionalStatusSuspended.IsActive() {
		t.Error("Suspended debería ser IsActive=false")
	}
}

// ── LicenseStatus ─────────────────────────────────────────────────

func TestParseLicenseStatus(t *testing.T) {
	valid := []string{"Active", "Expired", "Suspended", "Revoked"}
	for _, s := range valid {
		t.Run(s, func(t *testing.T) {
			if _, err := valueobject.ParseLicenseStatus(s); err != nil {
				t.Errorf("ParseLicenseStatus(%q) error = %v", s, err)
			}
		})
	}
	t.Run("inválido falla", func(t *testing.T) {
		if _, err := valueobject.ParseLicenseStatus("Unknown"); err == nil {
			t.Error("se esperaba error")
		}
	})
}

func TestLicenseStatusIsValid(t *testing.T) {
	if !valueobject.LicenseStatusActive.IsValid() {
		t.Error("Active debería ser IsValid=true")
	}
	if valueobject.LicenseStatusExpired.IsValid() {
		t.Error("Expired debería ser IsValid=false")
	}
}

// ── AssignmentStatus ──────────────────────────────────────────────

func TestAssignmentStatusIsActive(t *testing.T) {
	if !valueobject.AssignmentStatusActive.IsActive() {
		t.Error("Active debería ser IsActive=true")
	}
	if valueobject.AssignmentStatusEnded.IsActive() {
		t.Error("Ended debería ser IsActive=false")
	}
}

// ── NewProcedureDuration ──────────────────────────────────────────

func TestNewProcedureDuration(t *testing.T) {
	t.Run("valores válidos", func(t *testing.T) {
		pd, err := valueobject.NewProcedureDuration("D1110", 30, 10)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if pd.TotalMinutes() != 40 {
			t.Errorf("TotalMinutes() = %d, se esperaba 40", pd.TotalMinutes())
		}
		if pd.TotalDuration() != 40*time.Minute {
			t.Errorf("TotalDuration() = %v", pd.TotalDuration())
		}
	})
	cases := []struct {
		name          string
		code          string
		minutes, buff int
	}{
		{"código vacío", "", 30, 0},
		{"minutos < 5", "D1110", 4, 0},
		{"minutos > 480", "D1110", 481, 0},
		{"buffer negativo", "D1110", 30, -1},
		{"buffer > 60", "D1110", 30, 61},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := valueobject.NewProcedureDuration(tc.code, tc.minutes, tc.buff); err == nil {
				t.Errorf("se esperaba error para %q minutes=%d buffer=%d", tc.name, tc.minutes, tc.buff)
			}
		})
	}
}

// ── Weekday ───────────────────────────────────────────────────────

func TestWeekdayString(t *testing.T) {
	if got := valueobject.Monday.String(); got != "Lunes" {
		t.Errorf("Monday.String() = %q", got)
	}
	if got := valueobject.Weekday(99).String(); got != "Desconocido" {
		t.Errorf("Weekday(99).String() = %q", got)
	}
}

// ── NewDaySchedule ────────────────────────────────────────────────

func TestNewDaySchedule(t *testing.T) {
	t.Run("horario válido", func(t *testing.T) {
		ds, err := valueobject.NewDaySchedule(valueobject.Monday, 9, 0, 13, 0)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if ds.DurationMinutes() != 240 {
			t.Errorf("DurationMinutes() = %d, se esperaba 240", ds.DurationMinutes())
		}
	})
	cases := []struct {
		name                               string
		sh, sm, eh, em int
		wd             valueobject.Weekday
	}{
		{"hora inválida (>23)", 25, 0, 26, 0, valueobject.Monday},
		{"minutos inválidos (>59)", 9, 61, 13, 0, valueobject.Monday},
		{"fin <= inicio", 13, 0, 9, 0, valueobject.Monday},
		{"duración < 30 min", 9, 0, 9, 15, valueobject.Monday},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := valueobject.NewDaySchedule(tc.wd, tc.sh, tc.sm, tc.eh, tc.em); err == nil {
				t.Errorf("se esperaba error para %q", tc.name)
			}
		})
	}
}

func TestDayScheduleContainsTime(t *testing.T) {
	ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 9, 0, 13, 0)

	t.Run("lunes a las 10:00 está dentro", func(t *testing.T) {
		// Buscar el próximo lunes.
		now := time.Now()
		daysUntilMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		monday := time.Date(now.Year(), now.Month(), now.Day()+daysUntilMon, 10, 0, 0, 0, time.UTC)
		if !ds.ContainsTime(monday) {
			t.Error("se esperaba true para lunes 10:00")
		}
	})
	t.Run("martes no está dentro (día incorrecto)", func(t *testing.T) {
		now := time.Now()
		daysUntilTue := (int(time.Tuesday) - int(now.Weekday()) + 7) % 7
		tuesday := time.Date(now.Year(), now.Month(), now.Day()+daysUntilTue, 10, 0, 0, 0, time.UTC)
		if ds.ContainsTime(tuesday) {
			t.Error("se esperaba false para martes")
		}
	})
	t.Run("lunes a las 14:00 está fuera del horario", func(t *testing.T) {
		now := time.Now()
		daysUntilMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		monday := time.Date(now.Year(), now.Month(), now.Day()+daysUntilMon, 14, 0, 0, 0, time.UTC)
		if ds.ContainsTime(monday) {
			t.Error("se esperaba false para lunes 14:00")
		}
	})
}

// ── NewExceptionDay ───────────────────────────────────────────────

func TestNewExceptionDay(t *testing.T) {
	tomorrow := time.Now().Add(24 * time.Hour)
	ds, _ := valueobject.NewDaySchedule(valueobject.Monday, 10, 0, 12, 0)

	t.Run("día libre (is_working=false)", func(t *testing.T) {
		exc, err := valueobject.NewExceptionDay(tomorrow, "Feriado", false, nil)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if exc.IsWorking {
			t.Error("IsWorking debería ser false")
		}
		if !exc.MatchesDate(tomorrow) {
			t.Error("MatchesDate debería ser true para el mismo día")
		}
	})
	t.Run("horario especial (is_working=true)", func(t *testing.T) {
		_, err := valueobject.NewExceptionDay(tomorrow, "Horario reducido", true, &ds)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("motivo vacío falla", func(t *testing.T) {
		_, err := valueobject.NewExceptionDay(tomorrow, "", false, nil)
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
	t.Run("is_working=true sin schedule falla", func(t *testing.T) {
		_, err := valueobject.NewExceptionDay(tomorrow, "test", true, nil)
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
	t.Run("is_working=false con schedule falla", func(t *testing.T) {
		_, err := valueobject.NewExceptionDay(tomorrow, "test", false, &ds)
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
	t.Run("MatchesDate retorna false para otra fecha", func(t *testing.T) {
		exc, _ := valueobject.NewExceptionDay(tomorrow, "test", false, nil)
		if exc.MatchesDate(tomorrow.Add(48 * time.Hour)) {
			t.Error("se esperaba false para fecha distinta")
		}
	})
}
