package valueobject_test

import (
	"testing"
	"time"

	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
)

// ── Gender ────────────────────────────────────────────────────────

func TestParseGender(t *testing.T) {
	cases := []struct {
		input   string
		want    valueobject.Gender
		wantErr bool
	}{
		{"M", valueobject.GenderMale, false},
		{"F", valueobject.GenderFemale, false},
		{"NB", valueobject.GenderNonBinary, false},
		{"NS", valueobject.GenderNotSpecified, false},
		{"m", valueobject.GenderMale, false},     // case-insensitive
		{"  F  ", valueobject.GenderFemale, false}, // trim
		{"X", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := valueobject.ParseGender(tc.input)
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

func TestGenderString(t *testing.T) {
	if got := valueobject.GenderFemale.String(); got != "F" {
		t.Errorf("String() = %q, se esperaba 'F'", got)
	}
}

// ── BirthDate ─────────────────────────────────────────────────────

func TestNewBirthDate(t *testing.T) {
	t.Run("fecha válida en el pasado", func(t *testing.T) {
		bd, err := valueobject.NewBirthDate(1990, 6, 15)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if bd.String() != "1990-06-15" {
			t.Errorf("String() = %q", bd.String())
		}
	})

	t.Run("rechaza fecha inexistente (31 de febrero)", func(t *testing.T) {
		_, err := valueobject.NewBirthDate(1990, 2, 31)
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("rechaza fecha inexistente (mes 13)", func(t *testing.T) {
		_, err := valueobject.NewBirthDate(1990, 13, 1)
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("rechaza fecha futura", func(t *testing.T) {
		future := time.Now().AddDate(1, 0, 0)
		_, err := valueobject.NewBirthDate(future.Year(), int(future.Month()), future.Day())
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("rechaza fecha de más de 130 años", func(t *testing.T) {
		tooOld := time.Now().AddDate(-131, 0, 0)
		_, err := valueobject.NewBirthDate(tooOld.Year(), int(tooOld.Month()), tooOld.Day())
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("acepta fecha de exactamente 130 años", func(t *testing.T) {
		edge := time.Now().AddDate(-130, 0, 0)
		_, err := valueobject.NewBirthDate(edge.Year(), int(edge.Month()), edge.Day())
		if err != nil {
			t.Errorf("error = %v, se esperaba nil para exactamente 130 años", err)
		}
	})

	t.Run("acepta fecha de hoy (recién nacido)", func(t *testing.T) {
		now := time.Now().UTC()
		_, err := valueobject.NewBirthDate(now.Year(), int(now.Month()), now.Day())
		if err != nil {
			t.Errorf("error = %v, se esperaba nil", err)
		}
	})
}

func TestNewBirthDateFromTime(t *testing.T) {
	src := time.Date(1985, 3, 20, 14, 30, 0, 0, time.UTC)
	bd, err := valueobject.NewBirthDateFromTime(src)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if bd.String() != "1985-03-20" {
		t.Errorf("String() = %q, se esperaba '1985-03-20'", bd.String())
	}
}

func TestBirthDateTime(t *testing.T) {
	bd, _ := valueobject.NewBirthDate(2000, 1, 1)
	tm := bd.Time()
	if tm.Year() != 2000 || tm.Month() != 1 || tm.Day() != 1 {
		t.Errorf("Time() = %v", tm)
	}
}

func TestAgeYears(t *testing.T) {
	now := time.Now().UTC()

	t.Run("cumpleaños ya pasó este año", func(t *testing.T) {
		// Nacido hace 30 años, un mes antes de "ahora" (cumpleaños ya ocurrió).
		birth := now.AddDate(-30, -1, 0)
		bd, err := valueobject.NewBirthDate(birth.Year(), int(birth.Month()), birth.Day())
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		if got := bd.AgeYears(); got != 30 {
			t.Errorf("AgeYears() = %d, se esperaba 30", got)
		}
	})

	t.Run("cumpleaños es hoy", func(t *testing.T) {
		birth := now.AddDate(-25, 0, 0)
		bd, err := valueobject.NewBirthDate(birth.Year(), int(birth.Month()), birth.Day())
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		if got := bd.AgeYears(); got != 25 {
			t.Errorf("AgeYears() = %d, se esperaba 25", got)
		}
	})

	t.Run("cumpleaños aún no llega este año", func(t *testing.T) {
		// Nacido hace 20 años, un mes después de "ahora" (cumpleaños todavía no llegó):
		// usamos AddDate(-20, 1, 0) para que el mes de nacimiento sea posterior al actual.
		birth := now.AddDate(-20, 1, 0)
		bd, err := valueobject.NewBirthDate(birth.Year(), int(birth.Month()), birth.Day())
		if err != nil {
			t.Skipf("fecha de prueba inválida para esta época del año: %v", err)
		}
		if got := bd.AgeYears(); got != 19 {
			t.Errorf("AgeYears() = %d, se esperaba 19 (cumpleaños no llegó aún)", got)
		}
	})

	t.Run("recién nacido tiene 0 años", func(t *testing.T) {
		bd, err := valueobject.NewBirthDate(now.Year(), int(now.Month()), now.Day())
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		if got := bd.AgeYears(); got != 0 {
			t.Errorf("AgeYears() = %d, se esperaba 0", got)
		}
	})
}

func TestIsMinor(t *testing.T) {
	now := time.Now().UTC()

	t.Run("menor de 18 años", func(t *testing.T) {
		birth := now.AddDate(-10, 0, 0)
		bd, _ := valueobject.NewBirthDate(birth.Year(), int(birth.Month()), birth.Day())
		if !bd.IsMinor() {
			t.Error("se esperaba IsMinor() = true")
		}
	})

	t.Run("mayor de 18 años", func(t *testing.T) {
		birth := now.AddDate(-25, 0, 0)
		bd, _ := valueobject.NewBirthDate(birth.Year(), int(birth.Month()), birth.Day())
		if bd.IsMinor() {
			t.Error("se esperaba IsMinor() = false")
		}
	})

	t.Run("exactamente 18 años no es menor", func(t *testing.T) {
		birth := now.AddDate(-18, -1, 0) // 18 años y un mes, cumpleaños ya pasado
		bd, err := valueobject.NewBirthDate(birth.Year(), int(birth.Month()), birth.Day())
		if err != nil {
			t.Skipf("fecha de prueba inválida: %v", err)
		}
		if bd.IsMinor() {
			t.Error("una persona de 18 años no debería ser menor")
		}
	})
}

// ── RiskLevel ─────────────────────────────────────────────────────

func TestParseRiskLevel(t *testing.T) {
	cases := []struct {
		input   string
		want    valueobject.RiskLevel
		wantErr bool
	}{
		{"Bajo", valueobject.RiskLevelLow, false},
		{"Medio", valueobject.RiskLevelMedium, false},
		{"Alto", valueobject.RiskLevelHigh, false},
		{"Extremo", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := valueobject.ParseRiskLevel(tc.input)
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

// ── AlertSeverity ─────────────────────────────────────────────────

func TestParseAlertSeverity(t *testing.T) {
	cases := []struct {
		input   string
		want    valueobject.AlertSeverity
		wantErr bool
	}{
		{"Info", valueobject.AlertSeverityInfo, false},
		{"Warning", valueobject.AlertSeverityWarning, false},
		{"Critical", valueobject.AlertSeverityCritical, false},
		{"Urgent", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := valueobject.ParseAlertSeverity(tc.input)
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

// ── AlertType ─────────────────────────────────────────────────────

func TestParseAlertType(t *testing.T) {
	cases := []struct {
		input   string
		want    valueobject.AlertType
		wantErr bool
	}{
		{"Alergia", valueobject.AlertTypeAllergy, false},
		{"Medicamento", valueobject.AlertTypeMedication, false},
		{"Condición", valueobject.AlertTypeCondition, false},
		{"Anestesia", valueobject.AlertTypeAnesthesia, false},
		{"RiesgoSangrado", valueobject.AlertTypeBleedingRisk, false},
		{"RiesgoInfeccioso", valueobject.AlertTypeInfectiousRisk, false},
		{"Otro", valueobject.AlertTypeOther, false},
		{"Inexistente", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := valueobject.ParseAlertType(tc.input)
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

// ── PreferredTimeOfDay ────────────────────────────────────────────

func TestParsePreferredTimeOfDay(t *testing.T) {
	cases := []struct {
		input   string
		want    valueobject.PreferredTimeOfDay
		wantErr bool
	}{
		{"Mañana", valueobject.TimeOfDayMorning, false},
		{"Tarde", valueobject.TimeOfDayAfternoon, false},
		{"Cualquiera", valueobject.TimeOfDayAny, false},
		{"Noche", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := valueobject.ParsePreferredTimeOfDay(tc.input)
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

// ── CommunicationChannel ──────────────────────────────────────────

func TestParseCommunicationChannel(t *testing.T) {
	cases := []struct {
		input   string
		want    valueobject.CommunicationChannel
		wantErr bool
	}{
		{"WhatsApp", valueobject.ChannelWhatsApp, false},
		{"Email", valueobject.ChannelEmail, false},
		{"SMS", valueobject.ChannelSMS, false},
		{"Telegram", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := valueobject.ParseCommunicationChannel(tc.input)
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

// ── CoverageType ──────────────────────────────────────────────────

func TestParseCoverageType(t *testing.T) {
	cases := []struct {
		input   string
		want    valueobject.CoverageType
		wantErr bool
	}{
		{"Privado", valueobject.CoverageTypePrivate, false},
		{"PrepagaPropia", valueobject.CoverageTypeOwnPrepaid, false},
		{"PrepagaExterna", valueobject.CoverageTypeExtPrepaid, false},
		{"ObraSocial", valueobject.CoverageTypeObraSocial, false},
		{"Corporativo", valueobject.CoverageTypeCorporate, false},
		{"ConvenioEspecial", valueobject.CoverageTypeSpecial, false},
		{"Inexistente", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := valueobject.ParseCoverageType(tc.input)
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

func TestRequiresExternalAuthorization(t *testing.T) {
	cases := []struct {
		ct   valueobject.CoverageType
		want bool
	}{
		{valueobject.CoverageTypeExtPrepaid, true},
		{valueobject.CoverageTypeObraSocial, true},
		{valueobject.CoverageTypePrivate, false},
		{valueobject.CoverageTypeOwnPrepaid, false},
		{valueobject.CoverageTypeCorporate, false},
		{valueobject.CoverageTypeSpecial, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.ct), func(t *testing.T) {
			if got := tc.ct.RequiresExternalAuthorization(); got != tc.want {
				t.Errorf("RequiresExternalAuthorization() = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}

func TestIsThirdPartyBilled(t *testing.T) {
	cases := []struct {
		ct   valueobject.CoverageType
		want bool
	}{
		{valueobject.CoverageTypePrivate, false},
		{valueobject.CoverageTypeOwnPrepaid, true},
		{valueobject.CoverageTypeExtPrepaid, true},
		{valueobject.CoverageTypeObraSocial, true},
		{valueobject.CoverageTypeCorporate, true},
		{valueobject.CoverageTypeSpecial, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.ct), func(t *testing.T) {
			if got := tc.ct.IsThirdPartyBilled(); got != tc.want {
				t.Errorf("IsThirdPartyBilled() = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}
