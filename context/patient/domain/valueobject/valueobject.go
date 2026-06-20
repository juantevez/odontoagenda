// Package valueobject define los Value Objects propios del bounded context Patient.
// Son inmutables, se comparan por valor, y encapsulan validación en su construcción.
package valueobject

import (
	"fmt"
	"strings"
	"time"
)

// ── Gender ────────────────────────────────────────────────────────

type Gender string

const (
	GenderMale        Gender = "M"
	GenderFemale      Gender = "F"
	GenderNonBinary   Gender = "NB"
	GenderNotSpecified Gender = "NS"
)

func ParseGender(s string) (Gender, error) {
	g := Gender(strings.ToUpper(strings.TrimSpace(s)))
	switch g {
	case GenderMale, GenderFemale, GenderNonBinary, GenderNotSpecified:
		return g, nil
	}
	return "", fmt.Errorf("género inválido '%s': valores válidos M, F, NB, NS", s)
}

func (g Gender) String() string { return string(g) }

// ── BirthDate ─────────────────────────────────────────────────────

// BirthDate representa la fecha de nacimiento con validaciones de rango.
type BirthDate struct {
	value time.Time
}

func NewBirthDate(year, month, day int) (BirthDate, error) {
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

	// Validar que la fecha es real (Go normaliza fechas inválidas, ej: 31 feb)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return BirthDate{}, fmt.Errorf("fecha de nacimiento inválida: %04d-%02d-%02d", year, month, day)
	}

	now := time.Now().UTC()
	if t.After(now) {
		return BirthDate{}, fmt.Errorf("fecha de nacimiento no puede ser futura")
	}
	if now.Year()-t.Year() > 130 {
		return BirthDate{}, fmt.Errorf("fecha de nacimiento demasiado antigua (>130 años)")
	}

	return BirthDate{value: t}, nil
}

func NewBirthDateFromTime(t time.Time) (BirthDate, error) {
	return NewBirthDate(t.Year(), int(t.Month()), t.Day())
}

func (b BirthDate) Time() time.Time  { return b.value }
func (b BirthDate) String() string   { return b.value.Format("2006-01-02") }

// AgeYears calcula la edad en años completos respecto a hoy.
func (b BirthDate) AgeYears() int {
	now := time.Now().UTC()
	years := now.Year() - b.value.Year()
	// Ajuste si aún no llegó el cumpleaños este año
	if now.Month() < b.value.Month() ||
		(now.Month() == b.value.Month() && now.Day() < b.value.Day()) {
		years--
	}
	return years
}

// IsMinor reporta si la persona tiene menos de 18 años.
func (b BirthDate) IsMinor() bool { return b.AgeYears() < 18 }

// ── RiskLevel ─────────────────────────────────────────────────────

// RiskLevel clasifica el nivel de riesgo odontológico del paciente.
type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "Bajo"
	RiskLevelMedium RiskLevel = "Medio"
	RiskLevelHigh   RiskLevel = "Alto"
)

func ParseRiskLevel(s string) (RiskLevel, error) {
	switch RiskLevel(s) {
	case RiskLevelLow, RiskLevelMedium, RiskLevelHigh:
		return RiskLevel(s), nil
	}
	return "", fmt.Errorf("nivel de riesgo inválido '%s': valores válidos Bajo, Medio, Alto", s)
}

// ── AlertSeverity ─────────────────────────────────────────────────

// AlertSeverity indica la gravedad de una alerta médica.
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "Info"     // informativa, sin impacto clínico
	AlertSeverityWarning  AlertSeverity = "Warning"  // requiere precaución
	AlertSeverityCritical AlertSeverity = "Critical" // puede comprometer la atención
)

func ParseAlertSeverity(s string) (AlertSeverity, error) {
	switch AlertSeverity(s) {
	case AlertSeverityInfo, AlertSeverityWarning, AlertSeverityCritical:
		return AlertSeverity(s), nil
	}
	return "", fmt.Errorf("severidad inválida '%s': valores válidos Info, Warning, Critical", s)
}

// ── AlertType ─────────────────────────────────────────────────────

// AlertType categoriza el tipo de alerta médica.
type AlertType string

const (
	AlertTypeAllergy         AlertType = "Alergia"
	AlertTypeMedication      AlertType = "Medicamento"
	AlertTypeCondition       AlertType = "Condición"       // hipertensión, diabetes, etc.
	AlertTypeAnesthesia      AlertType = "Anestesia"       // reacciones o contraindicaciones
	AlertTypeBleedingRisk    AlertType = "RiesgoSangrado"  // anticoagulantes, hemofilia
	AlertTypeInfectiousRisk  AlertType = "RiesgoInfeccioso"
	AlertTypeOther           AlertType = "Otro"
)

func ParseAlertType(s string) (AlertType, error) {
	switch AlertType(s) {
	case AlertTypeAllergy, AlertTypeMedication, AlertTypeCondition,
		AlertTypeAnesthesia, AlertTypeBleedingRisk, AlertTypeInfectiousRisk, AlertTypeOther:
		return AlertType(s), nil
	}
	return "", fmt.Errorf("tipo de alerta inválido: '%s'", s)
}

// ── PreferredTimeOfDay ────────────────────────────────────────────

type PreferredTimeOfDay string

const (
	TimeOfDayMorning   PreferredTimeOfDay = "Mañana"   // 08:00 - 13:00
	TimeOfDayAfternoon PreferredTimeOfDay = "Tarde"    // 13:00 - 20:00
	TimeOfDayAny       PreferredTimeOfDay = "Cualquiera"
)

func ParsePreferredTimeOfDay(s string) (PreferredTimeOfDay, error) {
	switch PreferredTimeOfDay(s) {
	case TimeOfDayMorning, TimeOfDayAfternoon, TimeOfDayAny:
		return PreferredTimeOfDay(s), nil
	}
	return "", fmt.Errorf("horario preferido inválido: '%s'", s)
}

// ── CommunicationChannel ──────────────────────────────────────────

type CommunicationChannel string

const (
	ChannelWhatsApp CommunicationChannel = "WhatsApp"
	ChannelEmail    CommunicationChannel = "Email"
	ChannelSMS      CommunicationChannel = "SMS"
)

func ParseCommunicationChannel(s string) (CommunicationChannel, error) {
	switch CommunicationChannel(s) {
	case ChannelWhatsApp, ChannelEmail, ChannelSMS:
		return CommunicationChannel(s), nil
	}
	return "", fmt.Errorf("canal de comunicación inválido: '%s'", s)
}

// ── CoverageType ──────────────────────────────────────────────────

// CoverageType clasifica el tipo de cobertura del paciente.
// Es el discriminador del submódulo Coverage.
type CoverageType string

const (
	CoverageTypePrivate     CoverageType = "Privado"          // pago de bolsillo
	CoverageTypeOwnPrepaid  CoverageType = "PrepagaPropia"    // plan propio de la clínica
	CoverageTypeExtPrepaid  CoverageType = "PrepagaExterna"   // OSDE, Swiss Medical, etc.
	CoverageTypeObraSocial  CoverageType = "ObraSocial"       // sindicatos, mutuales
	CoverageTypeCorporate   CoverageType = "Corporativo"      // empresa empleadora
	CoverageTypeSpecial     CoverageType = "ConvenioEspecial" // instituciones, colegios
)

func ParseCoverageType(s string) (CoverageType, error) {
	switch CoverageType(s) {
	case CoverageTypePrivate, CoverageTypeOwnPrepaid, CoverageTypeExtPrepaid,
		CoverageTypeObraSocial, CoverageTypeCorporate, CoverageTypeSpecial:
		return CoverageType(s), nil
	}
	return "", fmt.Errorf("tipo de cobertura inválido: '%s'", s)
}

// RequiresExternalAuthorization indica si este tipo de cobertura
// puede requerir autorización de un tercero externo.
func (ct CoverageType) RequiresExternalAuthorization() bool {
	return ct == CoverageTypeExtPrepaid || ct == CoverageTypeObraSocial
}

// IsThirdPartyBilled indica si la facturación va a un tercero (no al paciente directamente).
func (ct CoverageType) IsThirdPartyBilled() bool {
	return ct != CoverageTypePrivate
}
