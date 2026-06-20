// Package valueobject define Value Objects compartidos por múltiples bounded contexts.
// Son inmutables, se comparan por valor, y encapsulan validación en su construcción.
package valueobject

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ── Email ────────────────────────────────────────────────────────

// Email garantiza una dirección de correo válida.
type Email struct {
	value string
}

func NewEmail(raw string) (Email, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return Email{}, fmt.Errorf("email inválido '%s': %w", raw, err)
	}
	return Email{value: strings.ToLower(addr.Address)}, nil
}

func (e Email) String() string  { return e.value }
func (e Email) IsZero() bool    { return e.value == "" }
func (e Email) Equals(o Email) bool { return e.value == o.value }

// ── PhoneNumber ──────────────────────────────────────────────────

var phoneRegex = regexp.MustCompile(`^\+?[1-9]\d{6,14}$`)

// PhoneNumber almacena teléfonos en formato E.164 (ej: +541155556666).
type PhoneNumber struct {
	value string
}

func NewPhoneNumber(raw string) (PhoneNumber, error) {
	// Limpia espacios, guiones y paréntesis
	cleaned := regexp.MustCompile(`[\s\-\(\)\.]+`).ReplaceAllString(raw, "")
	if !phoneRegex.MatchString(cleaned) {
		return PhoneNumber{}, fmt.Errorf("teléfono inválido '%s': debe tener formato E.164", raw)
	}
	return PhoneNumber{value: cleaned}, nil
}

func (p PhoneNumber) String() string         { return p.value }
func (p PhoneNumber) IsZero() bool           { return p.value == "" }
func (p PhoneNumber) Equals(o PhoneNumber) bool { return p.value == o.value }

// ── FullName ─────────────────────────────────────────────────────

// FullName representa el nombre completo de una persona.
// Mínimo 2 caracteres, máximo 150, sin caracteres de control.
type FullName struct {
	value string
}

func NewFullName(raw string) (FullName, error) {
	trimmed := strings.TrimSpace(raw)
	length := utf8.RuneCountInString(trimmed)
	if length < 2 {
		return FullName{}, fmt.Errorf("nombre demasiado corto: mínimo 2 caracteres")
	}
	if length > 150 {
		return FullName{}, fmt.Errorf("nombre demasiado largo: máximo 150 caracteres")
	}
	return FullName{value: trimmed}, nil
}

func (n FullName) String() string          { return n.value }
func (n FullName) Equals(o FullName) bool  { return n.value == o.value }

// ── Address ──────────────────────────────────────────────────────

// Address representa una dirección física.
// El campo Coordinates es opcional y se completa con PostGIS.
type Address struct {
	Street     string
	Number     string
	Floor      string // opcional
	Apartment  string // opcional
	City       string
	Province   string
	PostalCode string
	Country    string
	// Coordinates geoespaciales (EPSG:4326)
	Latitude  *float64
	Longitude *float64
}

func NewAddress(street, number, city, province, postalCode, country string) (Address, error) {
	violations := map[string]string{}
	if strings.TrimSpace(street) == "" {
		violations["street"] = "requerido"
	}
	if strings.TrimSpace(city) == "" {
		violations["city"] = "requerido"
	}
	if strings.TrimSpace(country) == "" {
		violations["country"] = "requerido"
	}
	if len(violations) > 0 {
		return Address{}, fmt.Errorf("address inválida: %v", violations)
	}
	return Address{
		Street:     strings.TrimSpace(street),
		Number:     strings.TrimSpace(number),
		City:       strings.TrimSpace(city),
		Province:   strings.TrimSpace(province),
		PostalCode: strings.TrimSpace(postalCode),
		Country:    strings.TrimSpace(country),
	}, nil
}

func (a Address) WithCoordinates(lat, lng float64) Address {
	a.Latitude = &lat
	a.Longitude = &lng
	return a
}

func (a Address) HasCoordinates() bool {
	return a.Latitude != nil && a.Longitude != nil
}

func (a Address) String() string {
	parts := []string{a.Street}
	if a.Number != "" {
		parts = append(parts, a.Number)
	}
	if a.Floor != "" || a.Apartment != "" {
		parts = append(parts, fmt.Sprintf("Piso %s Dto %s", a.Floor, a.Apartment))
	}
	parts = append(parts, a.City, a.Province, a.PostalCode, a.Country)
	return strings.Join(parts, ", ")
}

// ── Money ────────────────────────────────────────────────────────

// Money representa un valor monetario con su moneda.
// Se almacena en centavos (int64) para evitar errores de punto flotante.
type Money struct {
	amountCents int64  // en centavos
	currency    string // ISO 4217: ARS, USD, etc.
}

func NewMoney(amountCents int64, currency string) (Money, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return Money{}, fmt.Errorf("moneda inválida '%s': debe ser código ISO 4217 de 3 letras", currency)
	}
	if amountCents < 0 {
		return Money{}, fmt.Errorf("monto no puede ser negativo")
	}
	return Money{amountCents: amountCents, currency: currency}, nil
}

func (m Money) AmountCents() int64   { return m.amountCents }
func (m Money) Currency() string     { return m.currency }
func (m Money) IsZero() bool         { return m.amountCents == 0 }

func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("no se pueden sumar monedas distintas: %s y %s", m.currency, other.currency)
	}
	return Money{amountCents: m.amountCents + other.amountCents, currency: m.currency}, nil
}

func (m Money) String() string {
	return fmt.Sprintf("%.2f %s", float64(m.amountCents)/100.0, m.currency)
}

func (m Money) Equals(o Money) bool {
	return m.amountCents == o.amountCents && m.currency == o.currency
}

// ── DocumentID ───────────────────────────────────────────────────

// DocumentType representa el tipo de documento de identidad.
type DocumentType string

const (
	DocDNI      DocumentType = "DNI"
	DocPasaporte DocumentType = "PASAPORTE"
	DocCUIT     DocumentType = "CUIT"
	DocCUIL     DocumentType = "CUIL"
	DocCE       DocumentType = "CE" // Cédula de Extranjería
)

// NationalID encapsula tipo + número de documento como unidad.
type NationalID struct {
	Type   DocumentType
	Number string
}

func NewNationalID(docType DocumentType, number string) (NationalID, error) {
	number = strings.TrimSpace(number)
	if number == "" {
		return NationalID{}, fmt.Errorf("número de documento requerido")
	}
	switch docType {
	case DocDNI, DocPasaporte, DocCUIT, DocCUIL, DocCE:
		// tipo válido
	default:
		return NationalID{}, fmt.Errorf("tipo de documento inválido: %s", docType)
	}
	return NationalID{Type: docType, Number: number}, nil
}

func (d NationalID) String() string { return fmt.Sprintf("%s: %s", d.Type, d.Number) }

func (d NationalID) Equals(o NationalID) bool {
	return d.Type == o.Type && d.Number == o.Number
}
