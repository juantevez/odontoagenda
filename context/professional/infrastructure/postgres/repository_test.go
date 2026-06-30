// White-box: package postgres para acceder a la función unexported scanProfessionalRow.
//
// NO se testean FindByID/FindByClinic/Search/etc. porque requieren
// una conexión real a PostgreSQL. Esos pertenecen a tests de integración
// (ej. con testcontainers).
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ── mockRowScanner ────────────────────────────────────────────────

type mockRowScanner struct {
	id        uuid.UUID
	userID    *uuid.UUID
	fullName  string
	email     string
	phone     string
	status    string
	createdAt time.Time
	updatedAt time.Time
	err       error
}

func (m *mockRowScanner) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	*(dest[0].(*uuid.UUID)) = m.id
	*(dest[1].(**uuid.UUID)) = m.userID
	*(dest[2].(*string)) = m.fullName
	*(dest[3].(*string)) = m.email
	*(dest[4].(*string)) = m.phone
	*(dest[5].(*string)) = m.status
	*(dest[6].(*time.Time)) = m.createdAt
	*(dest[7].(*time.Time)) = m.updatedAt
	return nil
}

func newValidScanner() *mockRowScanner {
	now := time.Now().UTC()
	return &mockRowScanner{
		id:        uuid.New(),
		userID:    nil,
		fullName:  "Dr. Juan Perez",
		email:     "dr.perez@example.com",
		phone:     "+5491112345678",
		status:    "Active",
		createdAt: now,
		updatedAt: now,
	}
}

// ── scanProfessionalRow ───────────────────────────────────────────

func TestScanProfessionalRow(t *testing.T) {
	t.Run("scan válido retorna Professional con datos correctos", func(t *testing.T) {
		sc := newValidScanner()
		p, err := scanProfessionalRow(sc)
		if err != nil {
			t.Fatalf("scanProfessionalRow() error = %v", err)
		}
		if p.ID() != sc.id {
			t.Errorf("ID = %v, se esperaba %v", p.ID(), sc.id)
		}
		if p.FullName().String() != sc.fullName {
			t.Errorf("FullName = %q, se esperaba %q", p.FullName().String(), sc.fullName)
		}
		if p.Email().String() != sc.email {
			t.Errorf("Email = %q, se esperaba %q", p.Email().String(), sc.email)
		}
		if string(p.Status()) != sc.status {
			t.Errorf("Status = %q, se esperaba %q", p.Status(), sc.status)
		}
	})

	t.Run("scan válido con userID seteado no falla", func(t *testing.T) {
		sc := newValidScanner()
		uid := uuid.New()
		sc.userID = &uid
		p, err := scanProfessionalRow(sc)
		if err != nil {
			t.Fatalf("scanProfessionalRow() error = %v", err)
		}
		if p == nil {
			t.Fatal("se esperaba Professional no nil")
		}
	})

	t.Run("retorna aggregate limpio sin licencias ni asignaciones", func(t *testing.T) {
		p, err := scanProfessionalRow(newValidScanner())
		if err != nil {
			t.Fatalf("scanProfessionalRow() error = %v", err)
		}
		if len(p.Licenses()) != 0 {
			t.Errorf("Licenses len = %d, se esperaba 0", len(p.Licenses()))
		}
		if len(p.ClinicAssignments()) != 0 {
			t.Errorf("ClinicAssignments len = %d, se esperaba 0", len(p.ClinicAssignments()))
		}
	})

	t.Run("pgx.ErrNoRows retorna error 'professional not found'", func(t *testing.T) {
		_, err := scanProfessionalRow(&mockRowScanner{err: pgx.ErrNoRows})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if err.Error() != "professional not found" {
			t.Errorf("error = %q, se esperaba 'professional not found'", err.Error())
		}
	})

	t.Run("error de scan no pgx.ErrNoRows se propaga directamente", func(t *testing.T) {
		sentinel := errors.New("connection reset by peer")
		_, err := scanProfessionalRow(&mockRowScanner{err: sentinel})
		if !errors.Is(err, sentinel) {
			t.Errorf("error = %v, se esperaba %v", err, sentinel)
		}
	})

	t.Run("fullName inválido retorna error", func(t *testing.T) {
		sc := newValidScanner()
		sc.fullName = "X" // demasiado corto → NewFullName falla
		_, err := scanProfessionalRow(sc)
		if err == nil {
			t.Fatal("se esperaba error por fullName inválido")
		}
	})

	t.Run("email inválido retorna error", func(t *testing.T) {
		sc := newValidScanner()
		sc.email = "no-es-email"
		_, err := scanProfessionalRow(sc)
		if err == nil {
			t.Fatal("se esperaba error por email inválido")
		}
	})

	t.Run("teléfono inválido retorna error", func(t *testing.T) {
		sc := newValidScanner()
		sc.phone = "abc"
		_, err := scanProfessionalRow(sc)
		if err == nil {
			t.Fatal("se esperaba error por phone inválido")
		}
	})
}

// ── Save / Update ─────────────────────────────────────────────────

func TestSave(t *testing.T) {
	repo := NewProfessionalPostgresRepository(nil)
	err := repo.Save(context.Background(), nil)
	if err == nil {
		t.Fatal("se esperaba error 'not implemented'")
	}
}

func TestUpdate(t *testing.T) {
	repo := NewProfessionalPostgresRepository(nil)
	err := repo.Update(context.Background(), nil)
	if err == nil {
		t.Fatal("se esperaba error 'not implemented'")
	}
}

// ── Stubs sin conexión ────────────────────────────────────────────

func TestFindWithExpiringLicenses(t *testing.T) {
	repo := NewProfessionalPostgresRepository(nil)
	profs, err := repo.FindWithExpiringLicenses(context.Background(), 30)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(profs) != 0 {
		t.Errorf("len = %d, se esperaba 0", len(profs))
	}
}

func TestExistsByNationalID(t *testing.T) {
	repo := NewProfessionalPostgresRepository(nil)
	exists, err := repo.ExistsByNationalID(context.Background(), "12345678")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if exists {
		t.Error("se esperaba false")
	}
}
