// White-box: package postgres para acceder a los helpers unexported
// marshalPreferences, unmarshalPreferences, contact*, nullableStr,
// strOrEmpty e isUniqueViolation.
//
// NO se testean los métodos Save/Update/FindByID/etc. porque requieren
// una conexión real a PostgreSQL. Esos pertenecen a tests de integración
// (ej. con testcontainers).
package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── helpers de setup ──────────────────────────────────────────────

func newTestPatient(t *testing.T) *aggregate.Patient {
	t.Helper()
	name, _ := sharedvo.NewFullName("Juan Perez")
	bd, _ := valueobject.NewBirthDate(1990, 1, 1)
	g, _ := valueobject.ParseGender("M")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "12345678")
	phone, _ := sharedvo.NewPhoneNumber("+5491112345678")
	p, err := aggregate.NewPatient(nil, name, bd, g, docID, phone, nil)
	if err != nil {
		t.Fatalf("setup: NewPatient: %v", err)
	}
	p.PendingEvents()
	return p
}

// ── marshalPreferences / unmarshalPreferences ─────────────────────

func TestMarshalUnmarshalPreferences(t *testing.T) {
	t.Run("round-trip preserva todos los campos", func(t *testing.T) {
		clinicID := sharedtypes.ClinicID(uuid.New())
		prefs := aggregate.PatientPreferences{
			PreferredClinicID:    &clinicID,
			PreferredTimeOfDay:   valueobject.TimeOfDayMorning,
			CommunicationChannel: valueobject.ChannelEmail,
		}

		data, err := marshalPreferences(prefs)
		if err != nil {
			t.Fatalf("marshalPreferences() error = %v", err)
		}
		if len(data) == 0 {
			t.Fatal("marshalPreferences() retornó slice vacío")
		}

		restored, err := unmarshalPreferences(data)
		if err != nil {
			t.Fatalf("unmarshalPreferences() error = %v", err)
		}
		if restored.PreferredTimeOfDay != valueobject.TimeOfDayMorning {
			t.Errorf("PreferredTimeOfDay = %v", restored.PreferredTimeOfDay)
		}
		if restored.CommunicationChannel != valueobject.ChannelEmail {
			t.Errorf("CommunicationChannel = %v", restored.CommunicationChannel)
		}
		if restored.PreferredClinicID == nil || *restored.PreferredClinicID != clinicID {
			t.Errorf("PreferredClinicID = %v, se esperaba %v", restored.PreferredClinicID, clinicID)
		}
	})

	t.Run("sin PreferredClinicID serializa y deserializa a nil", func(t *testing.T) {
		prefs := aggregate.PatientPreferences{
			PreferredTimeOfDay:   valueobject.TimeOfDayAny,
			CommunicationChannel: valueobject.ChannelWhatsApp,
		}

		data, err := marshalPreferences(prefs)
		if err != nil {
			t.Fatalf("marshalPreferences() error = %v", err)
		}
		restored, err := unmarshalPreferences(data)
		if err != nil {
			t.Fatalf("unmarshalPreferences() error = %v", err)
		}
		if restored.PreferredClinicID != nil {
			t.Error("PreferredClinicID debería ser nil")
		}
	})

	t.Run("unmarshalPreferences ignora PreferredClinicID con UUID inválido", func(t *testing.T) {
		raw := []byte(`{"preferred_clinic_id":"not-a-uuid","preferred_time_of_day":"Mañana","communication_channel":"SMS"}`)
		prefs, err := unmarshalPreferences(raw)
		if err != nil {
			t.Fatalf("unmarshalPreferences() error = %v", err)
		}
		if prefs.PreferredClinicID != nil {
			t.Error("PreferredClinicID debería ser nil para UUID inválido")
		}
	})

	t.Run("unmarshalPreferences falla con JSON inválido", func(t *testing.T) {
		_, err := unmarshalPreferences([]byte("{invalid json"))
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
}

// ── contact* helpers ──────────────────────────────────────────────

func TestContactHelpers(t *testing.T) {
	t.Run("contactPhone retorna el teléfono del paciente", func(t *testing.T) {
		p := newTestPatient(t)
		if got := contactPhone(p); got != "+5491112345678" {
			t.Errorf("contactPhone() = %q, se esperaba '+5491112345678'", got)
		}
	})

	t.Run("contactWhatsApp retorna nil cuando no está seteado", func(t *testing.T) {
		p := newTestPatient(t)
		if contactWhatsApp(p) != nil {
			t.Error("contactWhatsApp() debería ser nil por defecto")
		}
	})

	t.Run("contactWhatsApp retorna el valor cuando está seteado", func(t *testing.T) {
		p := newTestPatient(t)
		phone, _ := sharedvo.NewPhoneNumber("+5491100000000")
		p.UpdateContactInfo(aggregate.ContactInfo{
			Phone:    p.ContactInfo().Phone,
			WhatsApp: &phone,
		})
		got := contactWhatsApp(p)
		if got == nil {
			t.Fatal("contactWhatsApp() debería estar seteado")
		}
		if *got != "+5491100000000" {
			t.Errorf("contactWhatsApp() = %q", *got)
		}
	})

	t.Run("contactEmail retorna nil cuando no está seteado", func(t *testing.T) {
		p := newTestPatient(t)
		if contactEmail(p) != nil {
			t.Error("contactEmail() debería ser nil por defecto")
		}
	})

	t.Run("contactEmail retorna el valor cuando está seteado", func(t *testing.T) {
		p := newTestPatient(t)
		email, _ := sharedvo.NewEmail("test@example.com")
		p.UpdateContactInfo(aggregate.ContactInfo{
			Phone: p.ContactInfo().Phone,
			Email: &email,
		})
		got := contactEmail(p)
		if got == nil {
			t.Fatal("contactEmail() debería estar seteado")
		}
		if *got != "test@example.com" {
			t.Errorf("contactEmail() = %q", *got)
		}
	})

	t.Run("contactEmergencyPhone retorna nil cuando no está seteado", func(t *testing.T) {
		p := newTestPatient(t)
		if contactEmergencyPhone(p) != nil {
			t.Error("contactEmergencyPhone() debería ser nil por defecto")
		}
	})

	t.Run("contactEmergencyPhone retorna el valor cuando está seteado", func(t *testing.T) {
		p := newTestPatient(t)
		epPhone, _ := sharedvo.NewPhoneNumber("+5491199999999")
		p.UpdateContactInfo(aggregate.ContactInfo{
			Phone:          p.ContactInfo().Phone,
			EmergencyPhone: &epPhone,
		})
		got := contactEmergencyPhone(p)
		if got == nil {
			t.Fatal("contactEmergencyPhone() debería estar seteado")
		}
		if *got != "+5491199999999" {
			t.Errorf("contactEmergencyPhone() = %q", *got)
		}
	})
}

// ── nullableStr / strOrEmpty ──────────────────────────────────────

func TestNullableStr(t *testing.T) {
	cases := []struct {
		input   string
		wantNil bool
	}{
		{"hola", false},
		{"", true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			got := nullableStr(tc.input)
			if tc.wantNil {
				if got != nil {
					t.Errorf("nullableStr(%q) = %v, se esperaba nil", tc.input, *got)
				}
			} else {
				if got == nil {
					t.Errorf("nullableStr(%q) = nil, se esperaba %q", tc.input, tc.input)
				} else if *got != tc.input {
					t.Errorf("nullableStr(%q) = %q, se esperaba %q", tc.input, *got, tc.input)
				}
			}
		})
	}
}

func TestStrOrEmpty(t *testing.T) {
	t.Run("nil retorna cadena vacía", func(t *testing.T) {
		if got := strOrEmpty(nil); got != "" {
			t.Errorf("strOrEmpty(nil) = %q, se esperaba ''", got)
		}
	})

	t.Run("puntero no nil retorna el valor", func(t *testing.T) {
		s := "odontoagenda"
		if got := strOrEmpty(&s); got != "odontoagenda" {
			t.Errorf("strOrEmpty(&%q) = %q", s, got)
		}
	})
}

// ── isUniqueViolation ─────────────────────────────────────────────

func TestIsUniqueViolation(t *testing.T) {
	t.Run("true para PgError con código 23505", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23505"}
		if !isUniqueViolation(pgErr) {
			t.Error("se esperaba true para SQLSTATE 23505")
		}
	})

	t.Run("false para PgError con otro código", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23000"}
		if isUniqueViolation(pgErr) {
			t.Error("se esperaba false para SQLSTATE 23000")
		}
	})

	t.Run("true si el PgError está envuelto", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23505"}
		wrapped := fmt.Errorf("operación fallida: %w", pgErr)
		if !isUniqueViolation(wrapped) {
			t.Error("errors.As debería desenvolver el PgError")
		}
	})

	t.Run("false para errores que no son PgError", func(t *testing.T) {
		if isUniqueViolation(errors.New("connection refused")) {
			t.Error("se esperaba false para error genérico")
		}
	})

	t.Run("false para nil", func(t *testing.T) {
		if isUniqueViolation(nil) {
			t.Error("se esperaba false para nil")
		}
	})
}
