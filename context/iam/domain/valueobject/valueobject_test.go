package valueobject_test

import (
	"strings"
	"testing"

	"github.com/juantevez/odontoagenda/context/iam/domain/valueobject"
)

// ── Role.Validate ─────────────────────────────────────────────────

func TestRoleValidate(t *testing.T) {
	valid := []valueobject.Role{
		valueobject.RolePatient,
		valueobject.RoleProfessional,
		valueobject.RoleReceptionist,
		valueobject.RoleClinicAdmin,
		valueobject.RoleSuperAdmin,
	}
	for _, r := range valid {
		t.Run(string(r)+" es válido", func(t *testing.T) {
			if err := r.Validate(); err != nil {
				t.Errorf("Validate() error = %v, se esperaba nil", err)
			}
		})
	}

	t.Run("rol desconocido retorna error", func(t *testing.T) {
		if err := valueobject.Role("hacker").Validate(); err == nil {
			t.Error("se esperaba error para rol desconocido")
		}
	})
}

// ── Role.String ───────────────────────────────────────────────────

func TestRoleString(t *testing.T) {
	if got := valueobject.RolePatient.String(); got != "paciente" {
		t.Errorf("String() = %q, se esperaba 'paciente'", got)
	}
}

// ── Role.IsStaff ──────────────────────────────────────────────────

func TestRoleIsStaff(t *testing.T) {
	cases := []struct {
		role  valueobject.Role
		staff bool
	}{
		{valueobject.RolePatient, false},
		{valueobject.RoleProfessional, true},
		{valueobject.RoleReceptionist, true},
		{valueobject.RoleClinicAdmin, true},
		{valueobject.RoleSuperAdmin, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.role), func(t *testing.T) {
			if got := tc.role.IsStaff(); got != tc.staff {
				t.Errorf("IsStaff() = %v, se esperaba %v", got, tc.staff)
			}
		})
	}
}

// ── UserStatus.IsActive ───────────────────────────────────────────

func TestUserStatusIsActive(t *testing.T) {
	cases := []struct {
		status valueobject.UserStatus
		active bool
	}{
		{valueobject.StatusActive, true},
		{valueobject.StatusSuspended, false},
		{valueobject.StatusPending, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := tc.status.IsActive(); got != tc.active {
				t.Errorf("IsActive() = %v, se esperaba %v", got, tc.active)
			}
		})
	}
}

// ── UserStatus.String ─────────────────────────────────────────────

func TestUserStatusString(t *testing.T) {
	if got := valueobject.StatusSuspended.String(); got != "Suspended" {
		t.Errorf("String() = %q, se esperaba 'Suspended'", got)
	}
}

// ── ParseUserStatus ───────────────────────────────────────────────

func TestParseUserStatus(t *testing.T) {
	cases := []struct {
		input   string
		want    valueobject.UserStatus
		wantErr bool
	}{
		{"Active", valueobject.StatusActive, false},
		{"Suspended", valueobject.StatusSuspended, false},
		{"Pending", valueobject.StatusPending, false},
		{"activo", valueobject.UserStatus(""), true},
		{"", valueobject.UserStatus(""), true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := valueobject.ParseUserStatus(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseUserStatus(%q) esperaba error, obtuvo nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseUserStatus(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("got = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}

// ── HashPassword ──────────────────────────────────────────────────

func TestHashPassword(t *testing.T) {
	t.Run("hashea un password válido", func(t *testing.T) {
		hp, err := valueobject.HashPassword("Sup3rSecret")
		if err != nil {
			t.Fatalf("HashPassword() error = %v", err)
		}
		if len(hp.Bytes()) == 0 {
			t.Error("hash vacío")
		}
	})

	strengthCases := []struct {
		name    string
		pw      string
		wantMsg string
	}{
		{
			name:    "rechaza password menor a 8 caracteres",
			pw:      "Ab1",
			wantMsg: "al menos 8 caracteres",
		},
		{
			name:    "rechaza password mayor a 72 caracteres",
			pw:      "Aa1" + strings.Repeat("b", 70),
			wantMsg: "72 caracteres",
		},
		{
			name:    "rechaza password sin mayúscula",
			pw:      "sup3rsecret",
			wantMsg: "mayúscula",
		},
		{
			name:    "rechaza password sin minúscula",
			pw:      "SUP3RSECRET",
			wantMsg: "minúscula",
		},
		{
			name:    "rechaza password sin dígito",
			pw:      "SuprSecret",
			wantMsg: "dígito",
		},
	}

	for _, tc := range strengthCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := valueobject.HashPassword(tc.pw)
			if err == nil {
				t.Fatalf("se esperaba error para password %q", tc.pw)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, se esperaba que contenga %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

// ── LoadHash ──────────────────────────────────────────────────────

func TestLoadHash(t *testing.T) {
	hp, err := valueobject.HashPassword("Sup3rSecret")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	rawBytes := hp.Bytes()

	loaded := valueobject.LoadHash(rawBytes)

	if string(loaded.Bytes()) != string(rawBytes) {
		t.Error("LoadHash: los bytes no coinciden con el original")
	}
	if !loaded.Matches("Sup3rSecret") {
		t.Error("LoadHash: Matches() debería ser true para el password original")
	}
}

// ── HashedPassword.Matches ────────────────────────────────────────

func TestMatches(t *testing.T) {
	hp, err := valueobject.HashPassword("Sup3rSecret")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Run("password correcto devuelve true", func(t *testing.T) {
		if !hp.Matches("Sup3rSecret") {
			t.Error("Matches() = false, se esperaba true")
		}
	})

	t.Run("password incorrecto devuelve false", func(t *testing.T) {
		if hp.Matches("OtraClave1") {
			t.Error("Matches() = true, se esperaba false")
		}
	})
}

// ── HashedPassword.String ─────────────────────────────────────────

func TestHashedPasswordString(t *testing.T) {
	hp, err := valueobject.HashPassword("Sup3rSecret")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	s := hp.String()
	if !strings.HasPrefix(s, "$2a$") {
		t.Errorf("String() = %q, se esperaba prefijo de bcrypt '$2a$'", s)
	}
	if s != string(hp.Bytes()) {
		t.Error("String() no coincide con Bytes()")
	}
}
