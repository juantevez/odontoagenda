package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/juantevez/odontoagenda/context/notifications/domain/entity"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
)

// ── mockInboxScanner (para scanInboxRow) ─────────────────────────

// mockInboxScanner simula una fila con el orden exacto de FindByClinic:
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

// ── mockRows (para FindByClinic) ──────────────────────────────────

type mockRows struct {
	scanners []*mockInboxScanner
	idx      int
	rowsErr  error
}

var _ pgx.Rows = (*mockRows)(nil)

func (m *mockRows) Next() bool              { m.idx++; return m.idx <= len(m.scanners) }
func (m *mockRows) Close()                  {}
func (m *mockRows) Err() error              { return m.rowsErr }
func (m *mockRows) Scan(dest ...any) error  { return m.scanners[m.idx-1].Scan(dest...) }
func (m *mockRows) CommandTag() pgconn.CommandTag          { return pgconn.CommandTag{} }
func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockRows) Values() ([]any, error)  { return nil, nil }
func (m *mockRows) RawValues() [][]byte     { return nil }
func (m *mockRows) Conn() *pgx.Conn         { return nil }

// ── mockRow (para CountUnread) ────────────────────────────────────

type mockRow struct {
	count   int
	scanErr error
}

var _ pgx.Row = (*mockRow)(nil)

func (m *mockRow) Scan(dest ...any) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	*dest[0].(*int) = m.count
	return nil
}

// ── mockQuerier ───────────────────────────────────────────────────

type mockQuerier struct {
	execErr    error
	queryRows  pgx.Rows
	queryErr   error
	queryRow   pgx.Row
	lastSQL    string
	lastArgs   []any
}

var _ dbQuerier = (*mockQuerier)(nil)

func (m *mockQuerier) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	m.lastSQL = sql
	m.lastArgs = args
	return pgconn.CommandTag{}, m.execErr
}

func (m *mockQuerier) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	m.lastSQL = sql
	m.lastArgs = args
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.queryRows, nil
}

func (m *mockQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	m.lastSQL = sql
	m.lastArgs = args
	return m.queryRow
}

// ── helpers ───────────────────────────────────────────────────────

func newRepo(q dbQuerier) *InboxPostgresRepository {
	return &InboxPostgresRepository{pool: q}
}

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
		t.Error("IsRead() = false, quería true")
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
		t.Errorf("error = %v, quería error sentinel", err)
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

func TestScanInboxRow_CreatedAtPreservado(t *testing.T) {
	sc := baseScanner()
	ts := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	sc.createdAt = ts
	n, _ := scanInboxRow(sc)
	if !n.CreatedAt.Equal(ts) {
		t.Errorf("CreatedAt = %v, quería %v", n.CreatedAt, ts)
	}
}

// ── Save ──────────────────────────────────────────────────────────

func TestSave_Exitoso_RetornaNil(t *testing.T) {
	q := &mockQuerier{}
	r := newRepo(q)
	n := entity.NewInboxNotification(
		valueobject.TypeAppointmentBooked, nil, uuid.New().String(), "T", "B",
	)
	if err := r.Save(context.Background(), n); err != nil {
		t.Errorf("Save() error = %v", err)
	}
}

func TestSave_ExecFalla_PropagaError(t *testing.T) {
	sentinel := errors.New("db: unique constraint")
	r := newRepo(&mockQuerier{execErr: sentinel})
	n := entity.NewInboxNotification(valueobject.TypeAppointmentBooked, nil, "", "T", "B")
	if err := r.Save(context.Background(), n); !errors.Is(err, sentinel) {
		t.Errorf("Save() error = %v, quería sentinel", err)
	}
}

// ── FindByClinic ──────────────────────────────────────────────────

func TestFindByClinic_QueryFalla_PropagaError(t *testing.T) {
	sentinel := errors.New("db: connection lost")
	r := newRepo(&mockQuerier{queryErr: sentinel})
	_, err := r.FindByClinic(context.Background(), uuid.New(), false, 50)
	if !errors.Is(err, sentinel) {
		t.Errorf("FindByClinic() error = %v, quería sentinel", err)
	}
}

func TestFindByClinic_SinFilas_RetornaSliceVacio(t *testing.T) {
	r := newRepo(&mockQuerier{queryRows: &mockRows{}})
	items, err := r.FindByClinic(context.Background(), uuid.New(), false, 50)
	if err != nil {
		t.Fatalf("FindByClinic() error = %v", err)
	}
	if items == nil {
		t.Error("FindByClinic() retornó nil, quería slice vacío no-nil")
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, quería 0", len(items))
	}
}

func TestFindByClinic_ConFilas_RetornaNotificaciones(t *testing.T) {
	sc1, sc2 := baseScanner(), baseScanner()
	rows := &mockRows{scanners: []*mockInboxScanner{sc1, sc2}}
	r := newRepo(&mockQuerier{queryRows: rows})

	items, err := r.FindByClinic(context.Background(), uuid.New(), false, 50)
	if err != nil {
		t.Fatalf("FindByClinic() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, quería 2", len(items))
	}
	if items[0].ID != sc1.id {
		t.Errorf("items[0].ID = %v, quería %v", items[0].ID, sc1.id)
	}
}

func TestFindByClinic_ScanFalla_PropagaError(t *testing.T) {
	sentinel := errors.New("scan: binary format inesperado")
	sc := &mockInboxScanner{scanErr: sentinel}
	r := newRepo(&mockQuerier{queryRows: &mockRows{scanners: []*mockInboxScanner{sc}}})

	_, err := r.FindByClinic(context.Background(), uuid.New(), false, 50)
	if !errors.Is(err, sentinel) {
		t.Errorf("FindByClinic() error = %v, quería sentinel", err)
	}
}

func TestFindByClinic_RowsErrPropagado(t *testing.T) {
	sentinel := errors.New("network: EOF")
	rows := &mockRows{scanners: []*mockInboxScanner{}, rowsErr: sentinel}
	r := newRepo(&mockQuerier{queryRows: rows})

	_, err := r.FindByClinic(context.Background(), uuid.New(), false, 50)
	if !errors.Is(err, sentinel) {
		t.Errorf("FindByClinic() error = %v, quería rows.Err() sentinel", err)
	}
}

func TestFindByClinic_UnreadOnlyFalse_NoFiltraReadAt(t *testing.T) {
	q := &mockQuerier{queryRows: &mockRows{}}
	r := newRepo(q)
	r.FindByClinic(context.Background(), uuid.New(), false, 50)

	if strings.Contains(q.lastSQL, "read_at IS NULL") {
		t.Error("SQL contiene filtro read_at cuando unreadOnly=false")
	}
}

func TestFindByClinic_UnreadOnlyTrue_AgregaFiltroReadAt(t *testing.T) {
	q := &mockQuerier{queryRows: &mockRows{}}
	r := newRepo(q)
	r.FindByClinic(context.Background(), uuid.New(), true, 50)

	if !strings.Contains(q.lastSQL, "read_at IS NULL") {
		t.Errorf("SQL = %q, debe contener filtro 'read_at IS NULL' cuando unreadOnly=true", q.lastSQL)
	}
}

func TestFindByClinic_LimitPropagadoComoArg(t *testing.T) {
	q := &mockQuerier{queryRows: &mockRows{}}
	r := newRepo(q)
	r.FindByClinic(context.Background(), uuid.New(), false, 25)

	if len(q.lastArgs) < 2 || q.lastArgs[1] != 25 {
		t.Errorf("lastArgs = %v, quería limit=25 como segundo argumento", q.lastArgs)
	}
}

// ── MarkRead ──────────────────────────────────────────────────────

func TestMarkRead_Exitoso_RetornaNil(t *testing.T) {
	r := newRepo(&mockQuerier{})
	if err := r.MarkRead(context.Background(), uuid.New()); err != nil {
		t.Errorf("MarkRead() error = %v", err)
	}
}

func TestMarkRead_ExecFalla_PropagaError(t *testing.T) {
	sentinel := errors.New("db: deadlock")
	r := newRepo(&mockQuerier{execErr: sentinel})
	if err := r.MarkRead(context.Background(), uuid.New()); !errors.Is(err, sentinel) {
		t.Errorf("MarkRead() error = %v, quería sentinel", err)
	}
}

func TestMarkRead_PropagaIDComoArg(t *testing.T) {
	q := &mockQuerier{}
	r := newRepo(q)
	id := uuid.New()
	r.MarkRead(context.Background(), id)
	if len(q.lastArgs) == 0 || q.lastArgs[0] != id {
		t.Errorf("lastArgs = %v, quería id=%v", q.lastArgs, id)
	}
}

// ── MarkAllRead ───────────────────────────────────────────────────

func TestMarkAllRead_Exitoso_RetornaNil(t *testing.T) {
	r := newRepo(&mockQuerier{})
	if err := r.MarkAllRead(context.Background(), uuid.New()); err != nil {
		t.Errorf("MarkAllRead() error = %v", err)
	}
}

func TestMarkAllRead_ExecFalla_PropagaError(t *testing.T) {
	sentinel := errors.New("db: timeout")
	r := newRepo(&mockQuerier{execErr: sentinel})
	if err := r.MarkAllRead(context.Background(), uuid.New()); !errors.Is(err, sentinel) {
		t.Errorf("MarkAllRead() error = %v, quería sentinel", err)
	}
}

func TestMarkAllRead_PropagaClinicIDComoArg(t *testing.T) {
	q := &mockQuerier{}
	r := newRepo(q)
	clinicID := uuid.New()
	r.MarkAllRead(context.Background(), clinicID)
	if len(q.lastArgs) == 0 || q.lastArgs[0] != clinicID {
		t.Errorf("lastArgs = %v, quería clinicID=%v", q.lastArgs, clinicID)
	}
}

// ── CountUnread ───────────────────────────────────────────────────

func TestCountUnread_RetornaConteo(t *testing.T) {
	r := newRepo(&mockQuerier{queryRow: &mockRow{count: 7}})
	count, err := r.CountUnread(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("CountUnread() error = %v", err)
	}
	if count != 7 {
		t.Errorf("count = %d, quería 7", count)
	}
}

func TestCountUnread_ScanFalla_PropagaError(t *testing.T) {
	sentinel := errors.New("scan: null en columna non-nullable")
	r := newRepo(&mockQuerier{queryRow: &mockRow{scanErr: sentinel}})
	_, err := r.CountUnread(context.Background(), uuid.New())
	if !errors.Is(err, sentinel) {
		t.Errorf("CountUnread() error = %v, quería sentinel", err)
	}
}

func TestCountUnread_PropagaClinicIDComoArg(t *testing.T) {
	q := &mockQuerier{queryRow: &mockRow{count: 0}}
	r := newRepo(q)
	clinicID := uuid.New()
	r.CountUnread(context.Background(), clinicID)
	if len(q.lastArgs) == 0 || q.lastArgs[0] != clinicID {
		t.Errorf("lastArgs = %v, quería clinicID=%v", q.lastArgs, clinicID)
	}
}

// ── constructor ───────────────────────────────────────────────────

func TestNewInboxPostgresRepository_NoNil(t *testing.T) {
	if NewInboxPostgresRepository(nil) == nil {
		t.Error("NewInboxPostgresRepository(nil) retornó nil")
	}
}
