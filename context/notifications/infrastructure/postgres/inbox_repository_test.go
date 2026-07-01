package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
)

// ── mockInboxScanner ──────────────────────────────────────────────

// mockInboxScanner implementa rowScanner para inyectar filas sin DB.
// Los campos reflejan el orden exacto del SELECT en FindByClinic:
//
//	id, type, clinic_id, reference_id, title, body, read_at, created_at
type mockInboxScanner struct {
	id          uuid.UUID
	notifType   string
	clinicIDPtr *uuid.UUID
	referenceID string
	title       string
	body        string
	readAt      *time.Time
	createdAt   time.Time
	scanErr     error
}

func (m *mockInboxScanner) Scan(dest ...any) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	*dest[0].(*uuid.UUID) = m.id
	*dest[1].(*string) = m.notifType
	*dest[2].(**uuid.UUID) = m.clinicIDPtr
	*dest[3].(*string) = m.referenceID
	*dest[4].(*string) = m.title
	*dest[5].(*string) = m.body
	*dest[6].(**time.Time) = m.readAt
	*dest[7].(*time.Time) = m.createdAt
	return nil
}

// ── helpers ───────────────────────────────────────────────────────

func baseScanner() *mockInboxScanner {
	clinicID := uuid.New()
	return &mockInboxScanner{
		id:          uuid.New(),
		notifType:   string(valueobject.TypeAppointmentBooked),
		clinicIDPtr: &clinicID,
		referenceID: uuid.New().String(),
		title:       "Turno reservado",
		body:        "El paciente reservó un turno.",
		readAt:      nil,
		createdAt:   time.Now().UTC().Truncate(time.Second),
	}
}

// ── scanInboxRow ──────────────────────────────────────────────────

func TestScanInboxRow_Valido_MapeaTodosLosCampos(t *testing.T) {
	sc := baseScanner()
	n, err := scanInboxRow(sc)

	if err != nil {
		t.Fatalf("scanInboxRow() error = %v", err)
	}
	if n.ID != sc.id {
		t.Errorf("ID = %v, quería %v", n.ID, sc.id)
	}
	if n.Type != valueobject.NotificationType(sc.notifType) {
		t.Errorf("Type = %q, quería %q", n.Type, sc.notifType)
	}
	if n.ClinicID == nil || *n.ClinicID != *sc.clinicIDPtr {
		t.Errorf("ClinicID = %v, quería %v", n.ClinicID, sc.clinicIDPtr)
	}
	if n.ReferenceID != sc.referenceID {
		t.Errorf("ReferenceID = %q, quería %q", n.ReferenceID, sc.referenceID)
	}
	if n.Title != sc.title {
		t.Errorf("Title = %q, quería %q", n.Title, sc.title)
	}
	if n.Body != sc.body {
		t.Errorf("Body = %q, quería %q", n.Body, sc.body)
	}
	if n.ReadAt != nil {
		t.Errorf("ReadAt = %v, quería nil", n.ReadAt)
	}
	if !n.CreatedAt.Equal(sc.createdAt) {
		t.Errorf("CreatedAt = %v, quería %v", n.CreatedAt, sc.createdAt)
	}
}

func TestScanInboxRow_ClinicIDNil_VisibleEnTodasLasSedes(t *testing.T) {
	sc := baseScanner()
	sc.clinicIDPtr = nil

	n, err := scanInboxRow(sc)
	if err != nil {
		t.Fatalf("scanInboxRow() error = %v", err)
	}
	if n.ClinicID != nil {
		t.Errorf("ClinicID = %v, quería nil", n.ClinicID)
	}
}

func TestScanInboxRow_ReadAtNil_NotificacionNoLeida(t *testing.T) {
	sc := baseScanner()
	sc.readAt = nil

	n, err := scanInboxRow(sc)
	if err != nil {
		t.Fatalf("scanInboxRow() error = %v", err)
	}
	if n.IsRead() {
		t.Error("IsRead() = true, quería false (ReadAt es nil)")
	}
}

func TestScanInboxRow_ReadAtNonNil_NotificacionLeida(t *testing.T) {
	sc := baseScanner()
	readAt := time.Now().UTC().Add(-time.Hour)
	sc.readAt = &readAt

	n, err := scanInboxRow(sc)
	if err != nil {
		t.Fatalf("scanInboxRow() error = %v", err)
	}
	if !n.IsRead() {
		t.Error("IsRead() = false, quería true (ReadAt no es nil)")
	}
	if !n.ReadAt.Equal(readAt) {
		t.Errorf("ReadAt = %v, quería %v", n.ReadAt, readAt)
	}
}

func TestScanInboxRow_ErrorDeScan_Propagado(t *testing.T) {
	sentinel := errors.New("scan: tipo incompatible")
	sc := &mockInboxScanner{scanErr: sentinel}

	_, err := scanInboxRow(sc)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería el error sentinel", err)
	}
}

func TestScanInboxRow_TodosLosTiposDeNotificacion(t *testing.T) {
	tipos := []valueobject.NotificationType{
		valueobject.TypeAppointmentBooked,
		valueobject.TypeAppointmentConfirmed,
		valueobject.TypeAppointmentCancelled,
		valueobject.TypeAppointmentReminder,
		valueobject.TypeAppointmentCompleted,
		valueobject.TypeAppointmentNoShow,
		valueobject.TypePatientCheckedIn,
		valueobject.TypePatientWelcome,
		valueobject.TypeLicenseExpiringSoon,
		valueobject.TypeAccountSuspended,
	}
	for _, tipo := range tipos {
		sc := baseScanner()
		sc.notifType = string(tipo)

		n, err := scanInboxRow(sc)
		if err != nil {
			t.Errorf("scanInboxRow(%q) error = %v", tipo, err)
			continue
		}
		if n.Type != tipo {
			t.Errorf("Type = %q, quería %q", n.Type, tipo)
		}
	}
}

func TestScanInboxRow_ReferenceIDVacio_Permitido(t *testing.T) {
	sc := baseScanner()
	sc.referenceID = ""

	n, err := scanInboxRow(sc)
	if err != nil {
		t.Fatalf("scanInboxRow() error = %v", err)
	}
	if n.ReferenceID != "" {
		t.Errorf("ReferenceID = %q, quería string vacío", n.ReferenceID)
	}
}

func TestScanInboxRow_CreatedAtPreservado(t *testing.T) {
	sc := baseScanner()
	ts := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	sc.createdAt = ts

	n, _ := scanInboxRow(sc)
	if !n.CreatedAt.Equal(ts) {
		t.Errorf("CreatedAt = %v, quería %v", n.CreatedAt, ts)
	}
}

// ── constructor ───────────────────────────────────────────────────

func TestNewInboxPostgresRepository_NoNil(t *testing.T) {
	r := NewInboxPostgresRepository(nil)
	if r == nil {
		t.Error("NewInboxPostgresRepository(nil) retornó nil")
	}
}

// ── compilación de stubs (interface compliance) ───────────────────

func TestInboxPostgresRepository_ImplementaInterfazRepositorio(t *testing.T) {
	// Verifica en tiempo de compilación que el repo implementa Save, FindByClinic,
	// MarkRead, MarkAllRead y CountUnread sin requerir conexión real.
	_ = NewInboxPostgresRepository(nil)
}
