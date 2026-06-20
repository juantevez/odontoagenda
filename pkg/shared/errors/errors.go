// Package errors define los errores de dominio del sistema OdontoAgenda.
// Todos los bounded contexts usan estos tipos base para garantizar
// consistencia en el manejo de errores a través de las capas.
package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrorCode identifica el tipo semántico del error de dominio.
type ErrorCode string

const (
	// 4xx — errores del cliente / negocio
	ErrNotFound          ErrorCode = "NOT_FOUND"
	ErrAlreadyExists     ErrorCode = "ALREADY_EXISTS"
	ErrInvalidArgument   ErrorCode = "INVALID_ARGUMENT"
	ErrUnauthorized      ErrorCode = "UNAUTHORIZED"
	ErrForbidden         ErrorCode = "FORBIDDEN"
	ErrConflict          ErrorCode = "CONFLICT"          // doble reserva, versión desactualizada
	ErrPrecondition      ErrorCode = "PRECONDITION"      // regla de negocio no satisfecha
	ErrValidation        ErrorCode = "VALIDATION"

	// 5xx — errores de infraestructura
	ErrInternal          ErrorCode = "INTERNAL"
	ErrUnavailable       ErrorCode = "UNAVAILABLE"
	ErrTimeout           ErrorCode = "TIMEOUT"
)

// DomainError es el tipo de error base del sistema.
// Porta suficiente contexto para logging estructurado y respuestas HTTP.
type DomainError struct {
	Code    ErrorCode
	Message string
	Details map[string]any
	Cause   error
}

func (e *DomainError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *DomainError) Unwrap() error { return e.Cause }

// HTTPStatus mapea el código de error al status HTTP correspondiente.
func (e *DomainError) HTTPStatus() int {
	switch e.Code {
	case ErrNotFound:
		return http.StatusNotFound
	case ErrAlreadyExists, ErrConflict:
		return http.StatusConflict
	case ErrInvalidArgument, ErrValidation:
		return http.StatusBadRequest
	case ErrUnauthorized:
		return http.StatusUnauthorized
	case ErrForbidden:
		return http.StatusForbidden
	case ErrPrecondition:
		return http.StatusUnprocessableEntity
	case ErrUnavailable:
		return http.StatusServiceUnavailable
	case ErrTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// ── Constructores ────────────────────────────────────────────────

func NewNotFound(entity, id string) *DomainError {
	return &DomainError{
		Code:    ErrNotFound,
		Message: fmt.Sprintf("%s con id '%s' no encontrado", entity, id),
		Details: map[string]any{"entity": entity, "id": id},
	}
}

func NewAlreadyExists(entity, field, value string) *DomainError {
	return &DomainError{
		Code:    ErrAlreadyExists,
		Message: fmt.Sprintf("%s con %s '%s' ya existe", entity, field, value),
		Details: map[string]any{"entity": entity, "field": field, "value": value},
	}
}

func NewConflict(msg string, cause error) *DomainError {
	return &DomainError{
		Code:    ErrConflict,
		Message: msg,
		Cause:   cause,
	}
}

func NewInvalidArgument(field, reason string) *DomainError {
	return &DomainError{
		Code:    ErrInvalidArgument,
		Message: fmt.Sprintf("argumento inválido en '%s': %s", field, reason),
		Details: map[string]any{"field": field, "reason": reason},
	}
}

func NewValidation(violations map[string]string) *DomainError {
	details := make(map[string]any, len(violations))
	for k, v := range violations {
		details[k] = v
	}
	return &DomainError{
		Code:    ErrValidation,
		Message: "validación fallida",
		Details: details,
	}
}

func NewUnauthorized(reason string) *DomainError {
	return &DomainError{Code: ErrUnauthorized, Message: reason}
}

func NewForbidden(action, resource string) *DomainError {
	return &DomainError{
		Code:    ErrForbidden,
		Message: fmt.Sprintf("acción '%s' no permitida sobre '%s'", action, resource),
		Details: map[string]any{"action": action, "resource": resource},
	}
}

func NewPrecondition(rule, detail string) *DomainError {
	return &DomainError{
		Code:    ErrPrecondition,
		Message: fmt.Sprintf("regla de negocio '%s' no satisfecha: %s", rule, detail),
		Details: map[string]any{"rule": rule, "detail": detail},
	}
}

func NewInternal(cause error) *DomainError {
	return &DomainError{Code: ErrInternal, Message: "error interno del servidor", Cause: cause}
}

// ── Helpers de inspección ────────────────────────────────────────

// As extrae *DomainError de una cadena de errores.
func As(err error) (*DomainError, bool) {
	var de *DomainError
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

// IsCode reporta si el error (o algún error en su cadena) tiene el código dado.
func IsCode(err error, code ErrorCode) bool {
	if de, ok := As(err); ok {
		return de.Code == code
	}
	return false
}

func IsNotFound(err error) bool    { return IsCode(err, ErrNotFound) }
func IsConflict(err error) bool    { return IsCode(err, ErrConflict) }
func IsForbidden(err error) bool   { return IsCode(err, ErrForbidden) }
func IsUnauthorized(err error) bool { return IsCode(err, ErrUnauthorized) }
