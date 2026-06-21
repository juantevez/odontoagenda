package aggregate

import (
	"time"

	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── FreeSlot ──────────────────────────────────────────────────────

// FreeSlot representa un intervalo de tiempo disponible para reservar.
// Es el resultado del SlotCalculator y la unidad que se cachea en Redis.
type FreeSlot struct {
	ProfessionalID sharedtypes.ProfessionalID `json:"professional_id"`
	ClinicID       sharedtypes.ClinicID       `json:"clinic_id"`
	ProcedureCode  string                     `json:"procedure_code"`
	Slot           valueobject.TimeSlot       `json:"slot"`
	DurationMins   int                        `json:"duration_mins"`
}

// ── DayScheduleSummary ────────────────────────────────────────────

// DayScheduleSummary es la vista del día para recepcionistas/profesionales.
// Agrupa citas del día con espacios libres entre ellas.
type DayScheduleSummary struct {
	ProfessionalID sharedtypes.ProfessionalID
	ClinicID       sharedtypes.ClinicID
	Date           time.Time
	Appointments   []*Appointment
	FreeSlots      []FreeSlot
	WorkingHours   *WorkingHour // nil si no trabaja ese día
}
