// Package types contiene tipos compartidos por todos los bounded contexts.
// Solo tipos primitivos del dominio: IDs, paginación, resultados.
// NO debe contener lógica de negocio ni dependencias de infraestructura.
package types

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ── IDs tipados ──────────────────────────────────────────────────
// Usar types distintos previene mezclar IDs de entidades diferentes en compile time.

type (
	PatientID      = uuid.UUID
	ProfessionalID = uuid.UUID
	ClinicID       = uuid.UUID
	AppointmentID  = uuid.UUID
	UserID         = uuid.UUID
	FamilyID       = uuid.UUID
	AgreementID    = uuid.UUID
	ProcedureCode  = string
)

func NewID() uuid.UUID { return uuid.New() }

func ParseID(s string) (uuid.UUID, error) { return uuid.Parse(s) }

func MustParseID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		panic("types.MustParseID: " + err.Error())
	}
	return id
}

// ── Paginación ───────────────────────────────────────────────────

// Page encapsula los parámetros de paginación de una query.
type Page struct {
	Limit  int // máximo de items por página (default 20, max 100)
	Offset int // desplazamiento desde el inicio
}

func NewPage(limit, offset int) Page {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return Page{Limit: limit, Offset: offset}
}

// PagedResult envuelve una colección con metadatos de paginación.
type PagedResult[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Limit      int   `json:"limit"`
	Offset     int   `json:"offset"`
	HasMore    bool  `json:"has_more"`
}

func NewPagedResult[T any](items []T, total int64, page Page) PagedResult[T] {
	return PagedResult[T]{
		Items:   items,
		Total:   total,
		Limit:   page.Limit,
		Offset:  page.Offset,
		HasMore: int64(page.Offset+page.Limit) < total,
	}
}

// ── Result monadico ──────────────────────────────────────────────
// Permite propagar éxito/error de forma explícita en casos de uso.

type Result[T any] struct {
	value T
	err   error
	ok    bool
}

func Ok[T any](v T) Result[T]        { return Result[T]{value: v, ok: true} }
func Err[T any](err error) Result[T] { return Result[T]{err: err} }

func (r Result[T]) IsOk() bool      { return r.ok }
func (r Result[T]) Value() T        { return r.value }
func (r Result[T]) Error() error    { return r.err }

func (r Result[T]) Unwrap() (T, error) { return r.value, r.err }

// ── AuditInfo ────────────────────────────────────────────────────
// Campos de auditoría reutilizables en cualquier aggregate.

type AuditInfo struct {
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	CreatedBy *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty"`
}

func NewAuditInfo(byUser *uuid.UUID) AuditInfo {
	now := time.Now().UTC()
	return AuditInfo{
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: byUser,
		UpdatedBy: byUser,
	}
}

func (a *AuditInfo) Touch(byUser *uuid.UUID) {
	a.UpdatedAt = time.Now().UTC()
	a.UpdatedBy = byUser
}

// ── SortOrder ────────────────────────────────────────────────────

type SortOrder string

const (
	SortAsc  SortOrder = "ASC"
	SortDesc SortOrder = "DESC"
)

func (s SortOrder) IsValid() bool {
	return s == SortAsc || s == SortDesc
}

// ── TimeRange ────────────────────────────────────────────────────

type TimeRange struct {
	From time.Time
	To   time.Time
}

func NewTimeRange(from, to time.Time) (TimeRange, error) {
	if to.Before(from) {
		return TimeRange{}, fmt.Errorf("TimeRange: 'to' (%s) debe ser posterior a 'from' (%s)", to, from)
	}
	return TimeRange{From: from, To: to}, nil
}

func (tr TimeRange) Duration() time.Duration { return tr.To.Sub(tr.From) }

func (tr TimeRange) Contains(t time.Time) bool {
	return !t.Before(tr.From) && !t.After(tr.To)
}

func (tr TimeRange) Overlaps(other TimeRange) bool {
	return tr.From.Before(other.To) && other.From.Before(tr.To)
}
