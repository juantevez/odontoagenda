// Package valueobject contiene los Value Objects propios del bounded context IAM.
package valueobject

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// ── Role ─────────────────────────────────────────────────────────

// Role define los roles de autorización del sistema.
type Role string

const (
	RolePatient      Role = "paciente"
	RoleProfessional Role = "profesional"
	RoleReceptionist Role = "recepcionista"
	RoleClinicAdmin  Role = "admin_sucursal"
	RoleSuperAdmin   Role = "superadmin"
)

var validRoles = map[Role]struct{}{
	RolePatient:      {},
	RoleProfessional: {},
	RoleReceptionist: {},
	RoleClinicAdmin:  {},
	RoleSuperAdmin:   {},
}

func (r Role) Validate() error {
	if _, ok := validRoles[r]; !ok {
		return fmt.Errorf("rol inválido '%s'", r)
	}
	return nil
}

func (r Role) String() string { return string(r) }

// IsStaff reporta si el rol pertenece al equipo interno (no paciente).
func (r Role) IsStaff() bool {
	return r == RoleProfessional || r == RoleReceptionist ||
		r == RoleClinicAdmin || r == RoleSuperAdmin
}

// ── UserStatus ────────────────────────────────────────────────────

// UserStatus es el estado del ciclo de vida de un User.
type UserStatus string

const (
	StatusActive    UserStatus = "Active"
	StatusSuspended UserStatus = "Suspended"
	StatusPending   UserStatus = "Pending" // pendiente de verificación de email
)

func (s UserStatus) IsActive() bool  { return s == StatusActive }
func (s UserStatus) String() string  { return string(s) }

func ParseUserStatus(s string) (UserStatus, error) {
	switch UserStatus(s) {
	case StatusActive, StatusSuspended, StatusPending:
		return UserStatus(s), nil
	default:
		return "", fmt.Errorf("status inválido '%s'", s)
	}
}

// ── HashedPassword ────────────────────────────────────────────────

// HashedPassword encapsula un password hasheado con bcrypt.
// Nunca expone el hash directamente: solo permite validar y comparar.
//
// Costo bcrypt: 12 (balance entre seguridad y rendimiento en servidores modernos).
const bcryptCost = 12

type HashedPassword struct {
	hash []byte
}

// HashPassword crea un HashedPassword desde un password en plano.
// Retorna error si el password no cumple las reglas mínimas.
func HashPassword(plain string) (HashedPassword, error) {
	if err := validatePasswordStrength(plain); err != nil {
		return HashedPassword{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return HashedPassword{}, fmt.Errorf("error hasheando password: %w", err)
	}
	return HashedPassword{hash: hash}, nil
}

// LoadHash reconstruye un HashedPassword desde el hash almacenado en BD.
// Para uso exclusivo de la capa de infraestructura (repositorios).
func LoadHash(hash []byte) HashedPassword {
	return HashedPassword{hash: hash}
}

// Matches compara el password en plano con el hash almacenado.
func (p HashedPassword) Matches(plain string) bool {
	err := bcrypt.CompareHashAndPassword(p.hash, []byte(plain))
	return err == nil
}

// Bytes retorna el hash en bytes para persistencia.
func (p HashedPassword) Bytes() []byte { return p.hash }

// String retorna el hash como string (para almacenar en DB como TEXT).
func (p HashedPassword) String() string { return string(p.hash) }

// validatePasswordStrength aplica las reglas de complejidad de contraseña.
func validatePasswordStrength(plain string) error {
	if len(plain) < 8 {
		return fmt.Errorf("password debe tener al menos 8 caracteres")
	}
	if len(plain) > 72 {
		// bcrypt trunca a 72 bytes: limitamos antes para evitar confusión.
		return fmt.Errorf("password no puede superar 72 caracteres")
	}

	var hasUpper, hasLower, hasDigit bool
	for _, c := range plain {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
	}

	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("password debe contener al menos una mayúscula, una minúscula y un dígito")
	}
	return nil
}
