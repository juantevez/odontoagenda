package valueobject_test

import (
	"testing"
	"time"

	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
)

// ── AppointmentStatus ─────────────────────────────────────────────

func TestParseAppointmentStatus(t *testing.T) {
	valid := []string{"Pending", "Confirmed", "InProgress", "Completed", "Cancelled", "NoShow"}
	for _, s := range valid {
		t.Run("válido_"+s, func(t *testing.T) {
			got, err := valueobject.ParseAppointmentStatus(s)
			if err != nil {
				t.Fatalf("ParseAppointmentStatus(%q) error = %v", s, err)
			}
			if got.String() != s {
				t.Errorf("got %q, se esperaba %q", got, s)
			}
		})
	}

	t.Run("inválido retorna error", func(t *testing.T) {
		_, err := valueobject.ParseAppointmentStatus("Borrado")
		if err == nil {
			t.Fatal("se esperaba error para estado inválido")
		}
	})

	t.Run("cadena vacía retorna error", func(t *testing.T) {
		_, err := valueobject.ParseAppointmentStatus("")
		if err == nil {
			t.Fatal("se esperaba error para cadena vacía")
		}
	})
}

func TestAppointmentStatus_IsActive(t *testing.T) {
	cases := []struct {
		status valueobject.AppointmentStatus
		want   bool
	}{
		{valueobject.StatusPending, true},
		{valueobject.StatusConfirmed, true},
		{valueobject.StatusInProgress, true},
		{valueobject.StatusCompleted, false},
		{valueobject.StatusCancelled, false},
		{valueobject.StatusNoShow, false},
	}
	for _, tc := range cases {
		t.Run(tc.status.String(), func(t *testing.T) {
			if got := tc.status.IsActive(); got != tc.want {
				t.Errorf("IsActive() = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}

func TestAppointmentStatus_IsTerminal(t *testing.T) {
	cases := []struct {
		status valueobject.AppointmentStatus
		want   bool
	}{
		{valueobject.StatusPending, false},
		{valueobject.StatusConfirmed, false},
		{valueobject.StatusInProgress, false},
		{valueobject.StatusCompleted, true},
		{valueobject.StatusCancelled, true},
		{valueobject.StatusNoShow, true},
	}
	for _, tc := range cases {
		t.Run(tc.status.String(), func(t *testing.T) {
			if got := tc.status.IsTerminal(); got != tc.want {
				t.Errorf("IsTerminal() = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}

func TestAppointmentStatus_IsActiveAndIsTerminal_SonMutuamenteExcluyentes(t *testing.T) {
	all := []valueobject.AppointmentStatus{
		valueobject.StatusPending, valueobject.StatusConfirmed, valueobject.StatusInProgress,
		valueobject.StatusCompleted, valueobject.StatusCancelled, valueobject.StatusNoShow,
	}
	for _, s := range all {
		t.Run(s.String(), func(t *testing.T) {
			if s.IsActive() && s.IsTerminal() {
				t.Errorf("%q no puede ser activo y terminal a la vez", s)
			}
			if !s.IsActive() && !s.IsTerminal() {
				t.Errorf("%q debe ser activo o terminal", s)
			}
		})
	}
}

// ── CancellationReason ────────────────────────────────────────────

func TestCancellationReason_IsValid(t *testing.T) {
	valid := []valueobject.CancellationReason{
		valueobject.CancelByPatient,
		valueobject.CancelByStaff,
		valueobject.CancelBySystem,
		valueobject.CancelLateNotice,
		valueobject.CancelNoAuthorization,
	}
	for _, r := range valid {
		t.Run("válido_"+string(r), func(t *testing.T) {
			if !r.IsValid() {
				t.Errorf("IsValid() = false para %q", r)
			}
		})
	}

	invalid := []valueobject.CancellationReason{"", "cancelado", "PATIENT_REQUEST", "otro"}
	for _, r := range invalid {
		t.Run("inválido_"+string(r), func(t *testing.T) {
			if r.IsValid() {
				t.Errorf("IsValid() = true para %q (inválido)", r)
			}
		})
	}
}

// ── TimeSlot — NewTimeSlot ────────────────────────────────────────

func TestNewTimeSlot(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Minute)

	t.Run("válido retorna slot con tiempos en UTC", func(t *testing.T) {
		// Usar hora con zona no-UTC para verificar normalización
		loc, _ := time.LoadLocation("America/Buenos_Aires")
		start := time.Now().In(loc).Add(time.Hour)
		end := start.Add(30 * time.Minute)

		slot, err := valueobject.NewTimeSlot(start, end)
		if err != nil {
			t.Fatalf("NewTimeSlot() error = %v", err)
		}
		if slot.Start.Location() != time.UTC {
			t.Errorf("Start.Location = %v, se esperaba UTC", slot.Start.Location())
		}
		if slot.End.Location() != time.UTC {
			t.Errorf("End.Location = %v, se esperaba UTC", slot.End.Location())
		}
	})

	t.Run("end == start → error", func(t *testing.T) {
		_, err := valueobject.NewTimeSlot(base, base)
		if err == nil {
			t.Fatal("se esperaba error con end == start")
		}
	})

	t.Run("end < start → error", func(t *testing.T) {
		_, err := valueobject.NewTimeSlot(base.Add(time.Hour), base)
		if err == nil {
			t.Fatal("se esperaba error con end < start")
		}
	})

	t.Run("duración < 5 min → error", func(t *testing.T) {
		_, err := valueobject.NewTimeSlot(base, base.Add(4*time.Minute))
		if err == nil {
			t.Fatal("se esperaba error con duración < 5 min")
		}
	})

	t.Run("duración == 5 min exactos → válido", func(t *testing.T) {
		_, err := valueobject.NewTimeSlot(base, base.Add(5*time.Minute))
		if err != nil {
			t.Fatalf("NewTimeSlot() error = %v (5 min debe ser válido)", err)
		}
	})

	t.Run("duración > 8 horas → error", func(t *testing.T) {
		_, err := valueobject.NewTimeSlot(base, base.Add(8*time.Hour+time.Second))
		if err == nil {
			t.Fatal("se esperaba error con duración > 8 horas")
		}
	})

	t.Run("duración == 8 horas exactas → válido", func(t *testing.T) {
		_, err := valueobject.NewTimeSlot(base, base.Add(8*time.Hour))
		if err != nil {
			t.Fatalf("NewTimeSlot() error = %v (8 horas debe ser válido)", err)
		}
	})
}

// ── TimeSlot — Duration / DurationMinutes ─────────────────────────

func TestTimeSlot_Duration(t *testing.T) {
	base := time.Now().UTC()
	slot, _ := valueobject.NewTimeSlot(base, base.Add(45*time.Minute))

	if d := slot.Duration(); d != 45*time.Minute {
		t.Errorf("Duration() = %v, se esperaba 45m", d)
	}
	if m := slot.DurationMinutes(); m != 45 {
		t.Errorf("DurationMinutes() = %d, se esperaba 45", m)
	}
}

// ── TimeSlot — Overlaps ───────────────────────────────────────────

func TestTimeSlot_Overlaps(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Minute)

	mk := func(startMin, endMin int) valueobject.TimeSlot {
		s, _ := valueobject.NewTimeSlot(
			base.Add(time.Duration(startMin)*time.Minute),
			base.Add(time.Duration(endMin)*time.Minute),
		)
		return s
	}

	cases := []struct {
		name  string
		a, b  valueobject.TimeSlot
		want  bool
	}{
		// Solapamiento total: A contiene a B
		{"A contiene B", mk(0, 120), mk(30, 60), true},
		// Solapamiento parcial: A empieza antes que B termina
		{"solapamiento parcial", mk(0, 60), mk(30, 90), true},
		// Solapamiento inverso: B empieza antes que A
		{"solapamiento inverso", mk(30, 90), mk(0, 60), true},
		// Iguales
		{"iguales", mk(0, 60), mk(0, 60), true},
		// Adyacentes: A termina donde B empieza (sin solapamiento)
		{"adyacentes A→B", mk(0, 60), mk(60, 120), false},
		// Adyacentes: B termina donde A empieza
		{"adyacentes B→A", mk(60, 120), mk(0, 60), false},
		// Sin contacto: A antes de B
		{"A antes de B", mk(0, 30), mk(60, 90), false},
		// Sin contacto: B antes de A
		{"B antes de A", mk(60, 90), mk(0, 30), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Overlaps(tc.b); got != tc.want {
				t.Errorf("Overlaps() = %v, se esperaba %v", got, tc.want)
			}
			// La operación debe ser simétrica
			if got := tc.b.Overlaps(tc.a); got != tc.want {
				t.Errorf("Overlaps() simetría = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}

// ── TimeSlot — Contains ───────────────────────────────────────────

func TestTimeSlot_Contains(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Minute)
	slot, _ := valueobject.NewTimeSlot(base, base.Add(60*time.Minute))

	cases := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"exactamente en start (incluido)", base, true},
		{"a mitad del slot", base.Add(30 * time.Minute), true},
		{"un instante antes del end", base.Add(59*time.Minute + 59*time.Second), true},
		{"exactamente en end (excluido)", base.Add(60 * time.Minute), false},
		{"antes del start", base.Add(-time.Second), false},
		{"después del end", base.Add(61 * time.Minute), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := slot.Contains(tc.t); got != tc.want {
				t.Errorf("Contains() = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}

// ── TimeSlot — String ─────────────────────────────────────────────

func TestTimeSlot_String(t *testing.T) {
	start := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	slot, _ := valueobject.NewTimeSlot(start, start.Add(30*time.Minute))

	got := slot.String()
	if got == "" {
		t.Fatal("String() retornó cadena vacía")
	}
	// Verificar que incluye la fecha y las horas en el formato esperado
	if got != "[2026-07-01 10:00 → 10:30]" {
		t.Errorf("String() = %q, se esperaba '[2026-07-01 10:00 → 10:30]'", got)
	}
}

// ── BlockedSlotReason ─────────────────────────────────────────────

func TestBlockedSlotReason_IsValid(t *testing.T) {
	valid := []valueobject.BlockedSlotReason{
		valueobject.BlockedVacation,
		valueobject.BlockedMeeting,
		valueobject.BlockedMaintenance,
		valueobject.BlockedPersonal,
		valueobject.BlockedOther,
	}
	for _, r := range valid {
		t.Run("válido_"+string(r), func(t *testing.T) {
			if !r.IsValid() {
				t.Errorf("IsValid() = false para %q", r)
			}
		})
	}

	invalid := []valueobject.BlockedSlotReason{"", "VACATION", "licencia", "otro_valor"}
	for _, r := range invalid {
		t.Run("inválido_"+string(r), func(t *testing.T) {
			if r.IsValid() {
				t.Errorf("IsValid() = true para %q (inválido)", r)
			}
		})
	}
}

// ── DefaultBookingConstraints ─────────────────────────────────────

func TestDefaultBookingConstraints(t *testing.T) {
	c := valueobject.DefaultBookingConstraints()

	if c.MinAdvanceHours != 1 {
		t.Errorf("MinAdvanceHours = %d, se esperaba 1", c.MinAdvanceHours)
	}
	if c.MaxAdvanceDays != 60 {
		t.Errorf("MaxAdvanceDays = %d, se esperaba 60", c.MaxAdvanceDays)
	}
	if c.CancellationFreeHours != 24 {
		t.Errorf("CancellationFreeHours = %d, se esperaba 24", c.CancellationFreeHours)
	}
	if c.MaxActiveAppointmentsPerPatient != 5 {
		t.Errorf("MaxActiveAppointmentsPerPatient = %d, se esperaba 5", c.MaxActiveAppointmentsPerPatient)
	}
}
