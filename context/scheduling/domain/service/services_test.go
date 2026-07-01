package service_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/service"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
)

// ── helpers ───────────────────────────────────────────────────────

// tomorrow returns midnight UTC of the next calendar day.
func tomorrow() time.Time {
	return time.Now().UTC().AddDate(0, 0, 1).Truncate(24 * time.Hour)
}

// scheduleFor creates an AvailabilitySchedule with working hours [startH, endH) on date's weekday.
func scheduleFor(date time.Time, startH, endH int) *aggregate.AvailabilitySchedule {
	wh := []aggregate.WorkingHour{{
		Weekday: date.Weekday(), StartHour: startH, StartMin: 0, EndHour: endH, EndMin: 0,
	}}
	return aggregate.NewAvailabilitySchedule(uuid.New(), uuid.New(), wh, nil)
}

// slotAt returns a TimeSlot of duration d starting at the given time.
func slotAt(t time.Time, d time.Duration) valueobject.TimeSlot {
	s, _ := valueobject.NewTimeSlot(t, t.Add(d))
	return s
}

// tomorrowAt returns the given hour:00 UTC on tomorrow's date.
func tomorrowAt(hour int) time.Time {
	tom := tomorrow()
	return time.Date(tom.Year(), tom.Month(), tom.Day(), hour, 0, 0, 0, time.UTC)
}

// ── SlotCalculator — CalculateForDate ────────────────────────────

func TestSlotCalculator_CalculateForDate(t *testing.T) {
	calc := service.NewSlotCalculator()

	t.Run("durationMins < 5 → ErrInvalidArgument", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10)
		_, err := calc.CalculateForDate(schedule, tomorrow(), "D1110", 4)
		if !sharederrors.IsCode(err, sharederrors.ErrInvalidArgument) {
			t.Errorf("se esperaba ErrInvalidArgument, error = %v", err)
		}
	})

	t.Run("schedule inactivo → slice vacío", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10)
		schedule.Deactivate()

		slots, err := calc.CalculateForDate(schedule, tomorrow(), "D1110", 30)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(slots))
		}
	})

	t.Run("día sin horario laboral → slice vacío", func(t *testing.T) {
		// Working hours only for a different weekday
		otherDay := tomorrow().AddDate(0, 0, 1) // day after tomorrow
		wh := []aggregate.WorkingHour{{
			Weekday: otherDay.Weekday(), StartHour: 8, EndHour: 10,
		}}
		schedule := aggregate.NewAvailabilitySchedule(uuid.New(), uuid.New(), wh, nil)

		slots, err := calc.CalculateForDate(schedule, tomorrow(), "D1110", 30)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(slots))
		}
	})

	t.Run("ventana de 2 horas con 60 min → 2 slots", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10)

		slots, err := calc.CalculateForDate(schedule, tomorrow(), "D1110", 60)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 2 {
			t.Errorf("len = %d, se esperaban 2 slots", len(slots))
		}
	})

	t.Run("ventana de 2 horas con 30 min → 4 slots", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10)

		slots, err := calc.CalculateForDate(schedule, tomorrow(), "D1110", 30)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 4 {
			t.Errorf("len = %d, se esperaban 4 slots", len(slots))
		}
	})

	t.Run("fecha en el pasado → todos los slots filtrados", func(t *testing.T) {
		// Use a fixed past Monday
		past := time.Date(2020, 1, 6, 0, 0, 0, 0, time.UTC) // Monday
		wh := []aggregate.WorkingHour{{Weekday: time.Monday, StartHour: 0, EndHour: 24}}
		schedule := aggregate.NewAvailabilitySchedule(uuid.New(), uuid.New(), wh, nil)

		slots, err := calc.CalculateForDate(schedule, past, "D1110", 30)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 0 {
			t.Errorf("len = %d, se esperaba 0 (todos en el pasado)", len(slots))
		}
	})

	t.Run("todos los slots retornados son en el futuro", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 0, 24)

		slots, err := calc.CalculateForDate(schedule, tomorrow(), "D1110", 30)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		now := time.Now().UTC()
		for _, s := range slots {
			if s.Slot.Start.Before(now) {
				t.Errorf("slot en el pasado: %v", s.Slot.Start)
			}
		}
	})

	t.Run("slot bloqueado no aparece en libres", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10) // 08:00-09:00, 09:00-10:00

		blockedStart := tomorrowAt(8)
		blocked := slotAt(blockedStart, time.Hour)
		_ = schedule.AddBlockedSlot(blocked, valueobject.BlockedSlotReason("vacation"), "")

		slots, err := calc.CalculateForDate(schedule, tomorrow(), "D1110", 60)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 1 {
			t.Errorf("len = %d, se esperaba 1 (08:00 bloqueado)", len(slots))
		}
		if len(slots) == 1 && slots[0].Slot.Start.Hour() != 9 {
			t.Errorf("slot libre en hora %d, se esperaba 9", slots[0].Slot.Start.Hour())
		}
	})

	t.Run("booked slot activo no aparece en libres", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10)

		bookedStart := tomorrowAt(8)
		booked := slotAt(bookedStart, time.Hour)
		_ = schedule.AddBookedSlot(uuid.New(), booked, uuid.New(), "D1110", valueobject.StatusConfirmed)

		slots, err := calc.CalculateForDate(schedule, tomorrow(), "D1110", 60)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 1 {
			t.Errorf("len = %d, se esperaba 1 (08:00 reservado)", len(slots))
		}
	})

	t.Run("booked slot cancelado no bloquea disponibilidad", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10)

		bookedStart := tomorrowAt(8)
		booked := slotAt(bookedStart, time.Hour)
		_ = schedule.AddBookedSlot(uuid.New(), booked, uuid.New(), "D1110", valueobject.StatusCancelled)

		slots, err := calc.CalculateForDate(schedule, tomorrow(), "D1110", 60)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 2 {
			t.Errorf("len = %d, se esperaban 2 (cancelado no bloquea)", len(slots))
		}
	})

	t.Run("excepción de día no laboral → 0 slots", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10)
		tom := tomorrow()
		schedule.SetExceptionDay(aggregate.ExceptionDay{Date: tom, IsWorking: false, Reason: "feriado"})

		slots, err := calc.CalculateForDate(schedule, tom, "D1110", 60)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 0 {
			t.Errorf("len = %d, se esperaba 0 (día no laboral por excepción)", len(slots))
		}
	})

	t.Run("excepción de día laboral sobreescribe horario recurrente", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10) // horario recurrente: 08:00-10:00 (2 slots de 60 min)
		tom := tomorrow()
		// Excepción: día laboral 10:00-12:00 → debe producir 2 slots diferentes
		schedule.SetExceptionDay(aggregate.ExceptionDay{
			Date: tom, IsWorking: true,
			StartHour: 10, StartMin: 0, EndHour: 12, EndMin: 0,
		})

		slots, err := calc.CalculateForDate(schedule, tom, "D1110", 60)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 2 {
			t.Errorf("len = %d, se esperaban 2 slots por excepción 10:00-12:00", len(slots))
		}
		if len(slots) > 0 && slots[0].Slot.Start.Hour() != 10 {
			t.Errorf("primer slot en hora %d, se esperaba 10 (excepción)", slots[0].Slot.Start.Hour())
		}
	})

	t.Run("FreeSlot lleva ProfessionalID, ClinicID y ProcedureCode correctos", func(t *testing.T) {
		profID, clinicID := uuid.New(), uuid.New()
		wh := []aggregate.WorkingHour{{Weekday: tomorrow().Weekday(), StartHour: 8, EndHour: 9}}
		schedule := aggregate.NewAvailabilitySchedule(profID, clinicID, wh, nil)

		slots, err := calc.CalculateForDate(schedule, tomorrow(), "D0150", 60)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 1 {
			t.Fatalf("len = %d, se esperaba 1", len(slots))
		}
		s := slots[0]
		if s.ProfessionalID != profID {
			t.Errorf("ProfessionalID = %v, se esperaba %v", s.ProfessionalID, profID)
		}
		if s.ClinicID != clinicID {
			t.Errorf("ClinicID = %v, se esperaba %v", s.ClinicID, clinicID)
		}
		if s.ProcedureCode != "D0150" {
			t.Errorf("ProcedureCode = %q, se esperaba 'D0150'", s.ProcedureCode)
		}
		if s.DurationMins != 60 {
			t.Errorf("DurationMins = %d, se esperaba 60", s.DurationMins)
		}
	})
}

// ── SlotCalculator — CalculateForRange ───────────────────────────

func TestSlotCalculator_CalculateForRange(t *testing.T) {
	calc := service.NewSlotCalculator()

	t.Run("To < From → ErrInvalidArgument", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10)
		from := tomorrow()
		to := from.AddDate(0, 0, -1)

		_, err := calc.CalculateForRange(schedule, from, to, "D1110", 60, 10)
		if !sharederrors.IsCode(err, sharederrors.ErrInvalidArgument) {
			t.Errorf("se esperaba ErrInvalidArgument, error = %v", err)
		}
	})

	t.Run("From == To → calcula solo ese día", func(t *testing.T) {
		schedule := scheduleFor(tomorrow(), 8, 10)
		tom := tomorrow()

		slots, err := calc.CalculateForRange(schedule, tom, tom, "D1110", 60, 10)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 2 {
			t.Errorf("len = %d, se esperaban 2 slots del mismo día", len(slots))
		}
	})

	t.Run("rango de 2 días con horario en ambos → slots de los dos días", func(t *testing.T) {
		// Schedule con horario todos los días
		wh := make([]aggregate.WorkingHour, 7)
		for d := 0; d < 7; d++ {
			wh[d] = aggregate.WorkingHour{Weekday: time.Weekday(d), StartHour: 8, EndHour: 9}
		}
		schedule := aggregate.NewAvailabilitySchedule(uuid.New(), uuid.New(), wh, nil)

		from := tomorrow()
		to := from.AddDate(0, 0, 1) // 2 días
		slots, err := calc.CalculateForRange(schedule, from, to, "D1110", 60, 100)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 2 {
			t.Errorf("len = %d, se esperaban 2 slots (1 por día)", len(slots))
		}
	})

	t.Run("MaxResults limita el resultado", func(t *testing.T) {
		wh := make([]aggregate.WorkingHour, 7)
		for d := 0; d < 7; d++ {
			wh[d] = aggregate.WorkingHour{Weekday: time.Weekday(d), StartHour: 8, EndHour: 16}
		}
		schedule := aggregate.NewAvailabilitySchedule(uuid.New(), uuid.New(), wh, nil)

		from := tomorrow()
		to := from.AddDate(0, 0, 6) // 7 días, muchos slots
		slots, err := calc.CalculateForRange(schedule, from, to, "D1110", 60, 3)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) > 3 {
			t.Errorf("len = %d, se esperaban ≤ 3 (MaxResults)", len(slots))
		}
	})

	t.Run("MaxResults=0 → default 50", func(t *testing.T) {
		wh := make([]aggregate.WorkingHour, 7)
		for d := 0; d < 7; d++ {
			wh[d] = aggregate.WorkingHour{Weekday: time.Weekday(d), StartHour: 0, EndHour: 24}
		}
		schedule := aggregate.NewAvailabilitySchedule(uuid.New(), uuid.New(), wh, nil)

		from := tomorrow()
		to := from.AddDate(0, 0, 6)
		slots, err := calc.CalculateForRange(schedule, from, to, "D1110", 30, 0)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) == 0 {
			t.Error("se esperaban slots con MaxResults default")
		}
		if len(slots) > 50 {
			t.Errorf("len = %d, se esperaban ≤ 50 (default)", len(slots))
		}
	})

	t.Run("rango sin días laborables → slice vacío", func(t *testing.T) {
		// Schedule sin horarios
		schedule := aggregate.NewAvailabilitySchedule(uuid.New(), uuid.New(), nil, nil)

		from := tomorrow()
		to := from.AddDate(0, 0, 3)
		slots, err := calc.CalculateForRange(schedule, from, to, "D1110", 30, 10)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(slots) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(slots))
		}
	})
}

// ── BookingPolicy — Validate ──────────────────────────────────────

func TestBookingPolicy_Validate(t *testing.T) {
	policy := service.NewBookingPolicy()
	defaults := valueobject.DefaultBookingConstraints() // Min=1h, Max=60d, MaxActive=5
	patientID := uuid.New()

	t.Run("slot válido sin violaciones", func(t *testing.T) {
		slot := slotAt(time.Now().UTC().Add(2*time.Hour), 30*time.Minute)
		violations := policy.Validate(slot, defaults, 0, patientID, patientID)
		if len(violations) != 0 {
			t.Errorf("se esperaban 0 violaciones, se obtuvieron %d: %v", len(violations), violations)
		}
	})

	t.Run("slot en el pasado → no_past_booking", func(t *testing.T) {
		slot := slotAt(time.Now().UTC().Add(-2*time.Hour), 30*time.Minute)
		violations := policy.Validate(slot, defaults, 0, patientID, patientID)
		if !containsRule(violations, "no_past_booking") {
			t.Errorf("se esperaba violación 'no_past_booking', se obtuvo: %v", violations)
		}
	})

	t.Run("slot dentro del período de anticipación mínima → min_advance", func(t *testing.T) {
		// Slot en 30 min, MinAdvanceHours = 1 → falla
		slot := slotAt(time.Now().UTC().Add(30*time.Minute), 30*time.Minute)
		violations := policy.Validate(slot, defaults, 0, patientID, patientID)
		if !containsRule(violations, "min_advance") {
			t.Errorf("se esperaba violación 'min_advance', se obtuvo: %v", violations)
		}
	})

	t.Run("slot más allá del máximo de días → max_advance", func(t *testing.T) {
		// Slot en 61 días, MaxAdvanceDays = 60 → falla
		slot := slotAt(time.Now().UTC().AddDate(0, 0, 61), 30*time.Minute)
		violations := policy.Validate(slot, defaults, 0, patientID, patientID)
		if !containsRule(violations, "max_advance") {
			t.Errorf("se esperaba violación 'max_advance', se obtuvo: %v", violations)
		}
	})

	t.Run("activeCount == maxActive → max_active_appointments", func(t *testing.T) {
		slot := slotAt(time.Now().UTC().Add(2*time.Hour), 30*time.Minute)
		// activeCount = 5 = MaxActiveAppointmentsPerPatient
		violations := policy.Validate(slot, defaults, 5, patientID, patientID)
		if !containsRule(violations, "max_active_appointments") {
			t.Errorf("se esperaba violación 'max_active_appointments', se obtuvo: %v", violations)
		}
	})

	t.Run("múltiples violaciones simultáneas", func(t *testing.T) {
		// Slot en el pasado + count al límite → ≥2 violaciones
		slot := slotAt(time.Now().UTC().Add(-time.Hour), 30*time.Minute)
		violations := policy.Validate(slot, defaults, 5, patientID, patientID)
		if len(violations) < 2 {
			t.Errorf("se esperaban ≥2 violaciones, se obtuvieron %d", len(violations))
		}
	})

	t.Run("MinAdvanceHours=0 deshabilita la regla", func(t *testing.T) {
		c := valueobject.BookingConstraints{MinAdvanceHours: 0, MaxAdvanceDays: 60, MaxActiveAppointmentsPerPatient: 5}
		// Slot en 5 min: sin regla de anticipación → no_past_booking tampoco (es futuro)
		slot := slotAt(time.Now().UTC().Add(5*time.Minute), 30*time.Minute)
		violations := policy.Validate(slot, c, 0, patientID, patientID)
		if containsRule(violations, "min_advance") {
			t.Error("min_advance no debe evaluarse cuando MinAdvanceHours=0")
		}
	})

	t.Run("MaxAdvanceDays=0 deshabilita la regla", func(t *testing.T) {
		c := valueobject.BookingConstraints{MinAdvanceHours: 1, MaxAdvanceDays: 0, MaxActiveAppointmentsPerPatient: 5}
		slot := slotAt(time.Now().UTC().AddDate(0, 0, 100), 30*time.Minute)
		violations := policy.Validate(slot, c, 0, patientID, patientID)
		if containsRule(violations, "max_advance") {
			t.Error("max_advance no debe evaluarse cuando MaxAdvanceDays=0")
		}
	})

	t.Run("MaxActiveAppointmentsPerPatient=0 deshabilita la regla", func(t *testing.T) {
		c := valueobject.BookingConstraints{MinAdvanceHours: 1, MaxAdvanceDays: 60, MaxActiveAppointmentsPerPatient: 0}
		slot := slotAt(time.Now().UTC().Add(2*time.Hour), 30*time.Minute)
		// count=100 no debería disparar la regla
		violations := policy.Validate(slot, c, 100, patientID, patientID)
		if containsRule(violations, "max_active_appointments") {
			t.Error("max_active_appointments no debe evaluarse cuando MaxActiveAppointmentsPerPatient=0")
		}
	})
}

// containsRule reports whether violations contains a given rule name.
func containsRule(violations []service.BookingViolation, rule string) bool {
	for _, v := range violations {
		if v.Rule == rule {
			return true
		}
	}
	return false
}

// ── BookingPolicy — IsCancellationFree ───────────────────────────

func TestBookingPolicy_IsCancellationFree(t *testing.T) {
	policy := service.NewBookingPolicy()

	t.Run("slot 48h en el futuro, ventana 24h → gratuita", func(t *testing.T) {
		slotStart := time.Now().UTC().Add(48 * time.Hour)
		if !policy.IsCancellationFree(slotStart, 24) {
			t.Error("se esperaba cancelación gratuita (48h > 24h)")
		}
	})

	t.Run("slot 12h en el futuro, ventana 24h → con cargo", func(t *testing.T) {
		slotStart := time.Now().UTC().Add(12 * time.Hour)
		if policy.IsCancellationFree(slotStart, 24) {
			t.Error("se esperaba cancelación con cargo (12h < 24h)")
		}
	})

	t.Run("slot exactamente en el límite (≥) → gratuita", func(t *testing.T) {
		// Añadir buffer de 1 minuto para evitar flakiness por ejecución lenta
		slotStart := time.Now().UTC().Add(24*time.Hour + time.Minute)
		if !policy.IsCancellationFree(slotStart, 24) {
			t.Error("se esperaba cancelación gratuita en el límite")
		}
	})

	t.Run("ventana 0 → siempre gratuita", func(t *testing.T) {
		slotStart := time.Now().UTC().Add(time.Minute) // apenas en el futuro
		if !policy.IsCancellationFree(slotStart, 0) {
			t.Error("se esperaba cancelación gratuita con ventana=0")
		}
	})

	t.Run("slot en el pasado → no gratuita", func(t *testing.T) {
		slotStart := time.Now().UTC().Add(-time.Hour)
		if policy.IsCancellationFree(slotStart, 24) {
			t.Error("se esperaba cancelación con cargo (slot en el pasado)")
		}
	})
}
