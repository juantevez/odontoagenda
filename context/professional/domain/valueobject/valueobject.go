// Package valueobject define los Value Objects propios del bounded context Professional.
package valueobject

import (
	"fmt"
	"strings"
	"time"
)

// ── SpecialtyCode ─────────────────────────────────────────────────

// SpecialtyCode es el identificador canónico de una especialidad odontológica.
// Se comparte con Treatment Catalog vía código string (no FK directa).
type SpecialtyCode string

const (
	SpecialtyGeneralDentistry  SpecialtyCode = "ODONTOLOGIA_GENERAL"
	SpecialtyEndodontics       SpecialtyCode = "ENDODONCIA"
	SpecialtyOrthodontics      SpecialtyCode = "ORTODONCIA"
	SpecialtyPeriodontics      SpecialtyCode = "PERIODONCIA"
	SpecialtyImplantology      SpecialtyCode = "IMPLANTOLOGIA"
	SpecialtyPediatricDentistry SpecialtyCode = "ODONTOPEDIATRIA"
	SpecialtyOralSurgery       SpecialtyCode = "CIRUGIA_ORAL"
	SpecialtyProsthodontics    SpecialtyCode = "PROSTODONCIA"
	SpecialtyWhitening         SpecialtyCode = "BLANQUEAMIENTO"
	SpecialtyAesthetics        SpecialtyCode = "ESTETICA_DENTAL"
	SpecialtyOrthoImplants     SpecialtyCode = "ORTHO_IMPLANTES"
)

func (s SpecialtyCode) IsValid() bool {
	switch s {
	case SpecialtyGeneralDentistry, SpecialtyEndodontics, SpecialtyOrthodontics,
		SpecialtyPeriodontics, SpecialtyImplantology, SpecialtyPediatricDentistry,
		SpecialtyOralSurgery, SpecialtyProsthodontics, SpecialtyWhitening,
		SpecialtyAesthetics, SpecialtyOrthoImplants:
		return true
	}
	return false
}

func (s SpecialtyCode) String() string { return string(s) }

// ── Specialty — Value Object ──────────────────────────────────────

// Specialty describe la especialidad que practica un profesional.
// Es VO (sin identidad): dos Specialty con el mismo Code son iguales.
// La matrícula y habilitación viven en ProfessionalLicense (Entity).
type Specialty struct {
	Code        SpecialtyCode
	DisplayName string // nombre legible para la UI
}

func NewSpecialty(code SpecialtyCode, displayName string) (Specialty, error) {
	if !code.IsValid() {
		return Specialty{}, fmt.Errorf("especialidad inválida: '%s'", code)
	}
	if strings.TrimSpace(displayName) == "" {
		return Specialty{}, fmt.Errorf("display_name requerido")
	}
	return Specialty{Code: code, DisplayName: strings.TrimSpace(displayName)}, nil
}

func (s Specialty) Equals(other Specialty) bool {
	return s.Code == other.Code
}

// ── ProfessionalStatus ────────────────────────────────────────────

type ProfessionalStatus string

const (
	ProfessionalStatusActive    ProfessionalStatus = "Active"
	ProfessionalStatusInactive  ProfessionalStatus = "Inactive"  // baja temporal
	ProfessionalStatusSuspended ProfessionalStatus = "Suspended" // baja por disciplina
)

func ParseProfessionalStatus(s string) (ProfessionalStatus, error) {
	switch ProfessionalStatus(s) {
	case ProfessionalStatusActive, ProfessionalStatusInactive, ProfessionalStatusSuspended:
		return ProfessionalStatus(s), nil
	}
	return "", fmt.Errorf("estado de profesional inválido: '%s'", s)
}

func (s ProfessionalStatus) IsActive() bool { return s == ProfessionalStatusActive }

// ── LicenseStatus ─────────────────────────────────────────────────

// LicenseStatus es el estado de habilitación de una matrícula profesional.
type LicenseStatus string

const (
	LicenseStatusActive    LicenseStatus = "Active"
	LicenseStatusExpired   LicenseStatus = "Expired"
	LicenseStatusSuspended LicenseStatus = "Suspended" // inhabilitado por ente regulador
	LicenseStatusRevoked   LicenseStatus = "Revoked"
)

func ParseLicenseStatus(s string) (LicenseStatus, error) {
	switch LicenseStatus(s) {
	case LicenseStatusActive, LicenseStatusExpired,
		LicenseStatusSuspended, LicenseStatusRevoked:
		return LicenseStatus(s), nil
	}
	return "", fmt.Errorf("estado de matrícula inválido: '%s'", s)
}

func (s LicenseStatus) IsValid() bool { return s == LicenseStatusActive }

// ── AssignmentStatus ──────────────────────────────────────────────

// AssignmentStatus es el estado de la asignación de un profesional a una sede.
type AssignmentStatus string

const (
	AssignmentStatusActive    AssignmentStatus = "Active"
	AssignmentStatusSuspended AssignmentStatus = "Suspended"
	AssignmentStatusEnded     AssignmentStatus = "Ended"
)

func (s AssignmentStatus) IsActive() bool { return s == AssignmentStatusActive }

// ── ProcedureDuration — Value Object ──────────────────────────────

// ProcedureDuration asocia un procedimiento con su duración estimada
// para un profesional específico en una sede específica.
// Se almacena como map[ProcedureCode]ProcedureDuration en ClinicAssignment.
type ProcedureDuration struct {
	ProcedureCode string
	Minutes       int // duración estimada en minutos
	BufferMinutes int // tiempo de limpieza/preparación entre turnos (incluido en el slot)
}

func NewProcedureDuration(procedureCode string, minutes, bufferMinutes int) (ProcedureDuration, error) {
	if strings.TrimSpace(procedureCode) == "" {
		return ProcedureDuration{}, fmt.Errorf("procedure_code requerido")
	}
	if minutes < 5 || minutes > 480 {
		return ProcedureDuration{}, fmt.Errorf("duración debe estar entre 5 y 480 minutos, recibido: %d", minutes)
	}
	if bufferMinutes < 0 || bufferMinutes > 60 {
		return ProcedureDuration{}, fmt.Errorf("buffer debe estar entre 0 y 60 minutos, recibido: %d", bufferMinutes)
	}
	return ProcedureDuration{
		ProcedureCode: strings.TrimSpace(procedureCode),
		Minutes:       minutes,
		BufferMinutes: bufferMinutes,
	}, nil
}

// TotalMinutes retorna la duración total incluyendo el buffer entre turnos.
func (pd ProcedureDuration) TotalMinutes() int { return pd.Minutes + pd.BufferMinutes }

// TotalDuration retorna la duración total como time.Duration.
func (pd ProcedureDuration) TotalDuration() time.Duration {
	return time.Duration(pd.TotalMinutes()) * time.Minute
}

// ── WorkingHours — Value Object ───────────────────────────────────

// Weekday es el día de la semana (0 = domingo, 6 = sábado, igual que time.Weekday).
type Weekday int

const (
	Sunday    Weekday = 0
	Monday    Weekday = 1
	Tuesday   Weekday = 2
	Wednesday Weekday = 3
	Thursday  Weekday = 4
	Friday    Weekday = 5
	Saturday  Weekday = 6
)

func (w Weekday) String() string {
	names := []string{"Domingo", "Lunes", "Martes", "Miércoles", "Jueves", "Viernes", "Sábado"}
	if int(w) < len(names) {
		return names[w]
	}
	return "Desconocido"
}

// DaySchedule define el horario de un día de la semana.
type DaySchedule struct {
	Weekday   Weekday
	StartHour int // 0-23
	StartMin  int // 0-59
	EndHour   int // 0-23
	EndMin    int // 0-59
}

func NewDaySchedule(weekday Weekday, startHour, startMin, endHour, endMin int) (DaySchedule, error) {
	if startHour < 0 || startHour > 23 || endHour < 0 || endHour > 23 {
		return DaySchedule{}, fmt.Errorf("hora inválida: debe estar entre 0 y 23")
	}
	if startMin < 0 || startMin > 59 || endMin < 0 || endMin > 59 {
		return DaySchedule{}, fmt.Errorf("minutos inválidos: debe estar entre 0 y 59")
	}
	startTotal := startHour*60 + startMin
	endTotal := endHour*60 + endMin
	if endTotal <= startTotal {
		return DaySchedule{}, fmt.Errorf("hora de fin debe ser posterior a hora de inicio")
	}
	if endTotal-startTotal < 30 {
		return DaySchedule{}, fmt.Errorf("duración mínima del turno: 30 minutos")
	}
	return DaySchedule{
		Weekday:  weekday,
		StartHour: startHour, StartMin: startMin,
		EndHour: endHour, EndMin: endMin,
	}, nil
}

// DurationMinutes retorna la duración del turno en minutos.
func (d DaySchedule) DurationMinutes() int {
	return (d.EndHour*60 + d.EndMin) - (d.StartHour*60 + d.StartMin)
}

// ContainsTime verifica si un time.Time cae dentro del horario del día.
func (d DaySchedule) ContainsTime(t time.Time) bool {
	if Weekday(t.Weekday()) != d.Weekday {
		return false
	}
	slotStart := d.StartHour*60 + d.StartMin
	slotEnd := d.EndHour*60 + d.EndMin
	tMinutes := t.Hour()*60 + t.Minute()
	return tMinutes >= slotStart && tMinutes < slotEnd
}

// ── ExceptionDay — Value Object ───────────────────────────────────

// ExceptionDay representa un día con horario especial o sin atención.
// Sobreescribe el WorkingHours recurrente para esa fecha específica.
type ExceptionDay struct {
	Date      time.Time    // fecha exacta (solo año/mes/día, hora ignorada)
	Reason    string       // "Vacaciones", "Feriado", "Capacitación", etc.
	IsWorking bool         // false = no trabaja ese día
	Schedule  *DaySchedule // horario especial si IsWorking=true y difiere del regular
}

func NewExceptionDay(date time.Time, reason string, isWorking bool, schedule *DaySchedule) (ExceptionDay, error) {
	if strings.TrimSpace(reason) == "" {
		return ExceptionDay{}, fmt.Errorf("reason requerido para exception day")
	}
	if isWorking && schedule == nil {
		return ExceptionDay{}, fmt.Errorf("schedule requerido si is_working=true")
	}
	if !isWorking && schedule != nil {
		return ExceptionDay{}, fmt.Errorf("schedule debe ser nil si is_working=false")
	}
	return ExceptionDay{
		Date:      time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC),
		Reason:    strings.TrimSpace(reason),
		IsWorking: isWorking,
		Schedule:  schedule,
	}, nil
}

// MatchesDate verifica si el ExceptionDay corresponde a una fecha dada.
func (e ExceptionDay) MatchesDate(t time.Time) bool {
	return e.Date.Year() == t.Year() &&
		e.Date.Month() == t.Month() &&
		e.Date.Day() == t.Day()
}
