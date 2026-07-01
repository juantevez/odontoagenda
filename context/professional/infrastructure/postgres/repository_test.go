// White-box: package postgres para acceder a scanProfessionalRow y dbQuerier.
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── mockRowScanner (para scanProfessionalRow y pgx.Row) ──────────

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

// Implementa tanto rowScanner como pgx.Row (ambos requieren solo Scan).
var _ pgx.Row = (*mockRowScanner)(nil)

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
		fullName:  "Dr. Juan Perez",
		email:     "dr.perez@example.com",
		phone:     "+5491112345678",
		status:    "Active",
		createdAt: now,
		updatedAt: now,
	}
}

// ── mockLicenseScanner ────────────────────────────────────────────
//
// Orden exacto del SELECT en loadLicenses:
// id, specialty_code, license_number, issued_at, expires_at, status, created_at, updated_at

type mockLicenseScanner struct {
	id            string
	specialtyCode string
	licenseNumber string
	issuedAt      time.Time
	expiresAt     *time.Time
	statusStr     string
	createdAt     time.Time
	updatedAt     time.Time
	scanErr       error
}

func (m *mockLicenseScanner) Scan(dest ...any) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	*(dest[0].(*string)) = m.id
	*(dest[1].(*string)) = m.specialtyCode
	*(dest[2].(*string)) = m.licenseNumber
	*(dest[3].(*time.Time)) = m.issuedAt
	*(dest[4].(**time.Time)) = m.expiresAt
	*(dest[5].(*string)) = m.statusStr
	*(dest[6].(*time.Time)) = m.createdAt
	*(dest[7].(*time.Time)) = m.updatedAt
	return nil
}

func validLicenseScanner() *mockLicenseScanner {
	return &mockLicenseScanner{
		id:            uuid.New().String(),
		specialtyCode: string(valueobject.SpecialtyGeneralDentistry),
		licenseNumber: "MAT-1234",
		issuedAt:      time.Now().UTC().Add(-365 * 24 * time.Hour),
		statusStr:     "Active",
		createdAt:     time.Now().UTC(),
		updatedAt:     time.Now().UTC(),
	}
}

// ── genericRows (implementa pgx.Rows con []rowScanner) ────────────

type genericRows struct {
	scanners []rowScanner
	idx      int
	rowsErr  error
}

var _ pgx.Rows = (*genericRows)(nil)

func (r *genericRows) Next() bool              { r.idx++; return r.idx <= len(r.scanners) }
func (r *genericRows) Scan(dest ...any) error  { return r.scanners[r.idx-1].Scan(dest...) }
func (r *genericRows) Close()                  {}
func (r *genericRows) Err() error              { return r.rowsErr }
func (r *genericRows) CommandTag() pgconn.CommandTag          { return pgconn.CommandTag{} }
func (r *genericRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *genericRows) Values() ([]any, error)  { return nil, nil }
func (r *genericRows) RawValues() [][]byte     { return nil }
func (r *genericRows) Conn() *pgx.Conn         { return nil }

func emptyRows() *genericRows { return &genericRows{} }

func profRows(scanners ...*mockRowScanner) *genericRows {
	rs := make([]rowScanner, len(scanners))
	for i, s := range scanners {
		rs[i] = s
	}
	return &genericRows{scanners: rs}
}

func licenseRows(scanners ...*mockLicenseScanner) *genericRows {
	rs := make([]rowScanner, len(scanners))
	for i, s := range scanners {
		rs[i] = s
	}
	return &genericRows{scanners: rs}
}

func errorRows(err error) *genericRows { return &genericRows{rowsErr: err} }

// ── mockQuerier ───────────────────────────────────────────────────

type queryResp struct {
	rows pgx.Rows
	err  error
}

type mockQuerier struct {
	execErr      error
	queryRowResp pgx.Row
	queries      []queryResp // respuestas en orden para llamadas sucesivas a Query
	queryIdx     int
	sqls         []string // captura todos los SQLs recibidos
	lastArgs     []any
}

var _ dbQuerier = (*mockQuerier)(nil)

func (m *mockQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, m.execErr
}

func (m *mockQuerier) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	m.sqls = append(m.sqls, sql)
	m.lastArgs = args
	if m.queryIdx >= len(m.queries) {
		return emptyRows(), nil
	}
	r := m.queries[m.queryIdx]
	m.queryIdx++
	return r.rows, r.err
}

func (m *mockQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	m.sqls = append(m.sqls, sql)
	m.lastArgs = args
	return m.queryRowResp
}

// helper: encola respuestas de Query
func querier(responses ...queryResp) *mockQuerier {
	return &mockQuerier{queries: responses}
}

func okQuery(rows pgx.Rows) queryResp    { return queryResp{rows: rows} }
func errQuery(err error) queryResp       { return queryResp{err: err} }

// ── helper de repo ────────────────────────────────────────────────

func newRepo(q dbQuerier) *ProfessionalPostgresRepository {
	return &ProfessionalPostgresRepository{pool: q}
}

// ── scanProfessionalRow ───────────────────────────────────────────

func TestScanProfessionalRow_ValidoRetornaProfessional(t *testing.T) {
	sc := newValidScanner()
	p, err := scanProfessionalRow(sc)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if p.ID() != sc.id {
		t.Errorf("ID = %v, quería %v", p.ID(), sc.id)
	}
	if p.FullName().String() != sc.fullName {
		t.Errorf("FullName = %q, quería %q", p.FullName().String(), sc.fullName)
	}
	if p.Email().String() != sc.email {
		t.Errorf("Email = %q, quería %q", p.Email().String(), sc.email)
	}
	if string(p.Status()) != sc.status {
		t.Errorf("Status = %q, quería %q", p.Status(), sc.status)
	}
}

func TestScanProfessionalRow_ConUserID(t *testing.T) {
	sc := newValidScanner()
	uid := uuid.New()
	sc.userID = &uid
	p, err := scanProfessionalRow(sc)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if p == nil {
		t.Fatal("Professional es nil")
	}
}

func TestScanProfessionalRow_SinLicenciasNiAsignaciones(t *testing.T) {
	p, err := scanProfessionalRow(newValidScanner())
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(p.Licenses()) != 0 {
		t.Errorf("Licenses len = %d, quería 0", len(p.Licenses()))
	}
	if len(p.ClinicAssignments()) != 0 {
		t.Errorf("ClinicAssignments len = %d, quería 0", len(p.ClinicAssignments()))
	}
}

func TestScanProfessionalRow_ErrNoRows_RetornaProfessionalNotFound(t *testing.T) {
	_, err := scanProfessionalRow(&mockRowScanner{err: pgx.ErrNoRows})
	if err == nil || err.Error() != "professional not found" {
		t.Errorf("error = %v, quería 'professional not found'", err)
	}
}

func TestScanProfessionalRow_ErrorGenerico_Propagado(t *testing.T) {
	sentinel := errors.New("conn reset")
	_, err := scanProfessionalRow(&mockRowScanner{err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestScanProfessionalRow_FullNameInvalido_RetornaError(t *testing.T) {
	sc := newValidScanner()
	sc.fullName = "X" // demasiado corto
	if _, err := scanProfessionalRow(sc); err == nil {
		t.Fatal("quería error por fullName inválido")
	}
}

func TestScanProfessionalRow_EmailInvalido_RetornaError(t *testing.T) {
	sc := newValidScanner()
	sc.email = "no-es-email"
	if _, err := scanProfessionalRow(sc); err == nil {
		t.Fatal("quería error por email inválido")
	}
}

func TestScanProfessionalRow_TelefonoInvalido_RetornaError(t *testing.T) {
	sc := newValidScanner()
	sc.phone = "abc"
	if _, err := scanProfessionalRow(sc); err == nil {
		t.Fatal("quería error por phone inválido")
	}
}

// ── FindByID ──────────────────────────────────────────────────────

func TestFindByID_Exitoso_SinLicencias(t *testing.T) {
	sc := newValidScanner()
	q := &mockQuerier{
		queryRowResp: sc,
		queries:      []queryResp{okQuery(emptyRows())}, // loadLicenses → sin filas
	}
	r := newRepo(q)

	p, err := r.FindByID(context.Background(), sharedtypes.ProfessionalID(sc.id))
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if p.ID() != sc.id {
		t.Errorf("ID = %v, quería %v", p.ID(), sc.id)
	}
	if len(p.Licenses()) != 0 {
		t.Errorf("Licenses = %d, quería 0", len(p.Licenses()))
	}
}

func TestFindByID_Exitoso_ConLicenciaValida(t *testing.T) {
	sc := newValidScanner()
	lic := validLicenseScanner()
	q := &mockQuerier{
		queryRowResp: sc,
		queries:      []queryResp{okQuery(licenseRows(lic))},
	}

	p, err := newRepo(q).FindByID(context.Background(), sharedtypes.ProfessionalID(sc.id))
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if len(p.Licenses()) != 1 {
		t.Errorf("Licenses = %d, quería 1", len(p.Licenses()))
	}
}

func TestFindByID_EspecialidadInvalida_LicenciaOmitida(t *testing.T) {
	sc := newValidScanner()
	lic := validLicenseScanner()
	lic.specialtyCode = "CODIGO_INEXISTENTE" // no pasa IsValid() → skipped
	q := &mockQuerier{
		queryRowResp: sc,
		queries:      []queryResp{okQuery(licenseRows(lic))},
	}

	p, err := newRepo(q).FindByID(context.Background(), sharedtypes.ProfessionalID(sc.id))
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if len(p.Licenses()) != 0 {
		t.Errorf("Licenses = %d, quería 0 (especialidad inválida omitida)", len(p.Licenses()))
	}
}

func TestFindByID_ScanError_ErrNoRows(t *testing.T) {
	q := &mockQuerier{queryRowResp: &mockRowScanner{err: pgx.ErrNoRows}}
	_, err := newRepo(q).FindByID(context.Background(), sharedtypes.ProfessionalID(uuid.New()))
	if err == nil || err.Error() != "professional not found" {
		t.Errorf("error = %v, quería 'professional not found'", err)
	}
}

func TestFindByID_ScanError_Generico(t *testing.T) {
	sentinel := errors.New("db: type mismatch")
	q := &mockQuerier{queryRowResp: &mockRowScanner{err: sentinel}}
	_, err := newRepo(q).FindByID(context.Background(), sharedtypes.ProfessionalID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestFindByID_LoadLicenses_QueryError(t *testing.T) {
	sentinel := errors.New("db: conn lost")
	q := &mockQuerier{
		queryRowResp: newValidScanner(),
		queries:      []queryResp{errQuery(sentinel)},
	}
	_, err := newRepo(q).FindByID(context.Background(), sharedtypes.ProfessionalID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel de loadLicenses", err)
	}
}

func TestFindByID_LoadLicenses_ScanError(t *testing.T) {
	sentinel := errors.New("scan: unexpected EOF")
	lic := &mockLicenseScanner{scanErr: sentinel}
	q := &mockQuerier{
		queryRowResp: newValidScanner(),
		queries:      []queryResp{okQuery(licenseRows(lic))},
	}
	_, err := newRepo(q).FindByID(context.Background(), sharedtypes.ProfessionalID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel de loadLicenses scan", err)
	}
}

func TestFindByID_LoadLicenses_RowsErr(t *testing.T) {
	sentinel := errors.New("network: EOF")
	q := &mockQuerier{
		queryRowResp: newValidScanner(),
		queries:      []queryResp{okQuery(errorRows(sentinel))},
	}
	_, err := newRepo(q).FindByID(context.Background(), sharedtypes.ProfessionalID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería rows.Err() sentinel", err)
	}
}

// ── FindByClinic ──────────────────────────────────────────────────

func TestFindByClinic_SinEspecialidad_SinResultados(t *testing.T) {
	q := querier(okQuery(emptyRows()))
	items, err := newRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()), nil)
	if err != nil {
		t.Fatalf("FindByClinic() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, quería 0", len(items))
	}
	if items == nil {
		t.Error("retornó nil, quería slice vacío no-nil")
	}
}

func TestFindByClinic_SinEspecialidad_ConUnProfessional(t *testing.T) {
	sc := newValidScanner()
	// findMany Query retorna 1 fila; loadLicenses Query retorna vacío
	q := querier(okQuery(profRows(sc)), okQuery(emptyRows()))
	items, err := newRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()), nil)
	if err != nil {
		t.Fatalf("FindByClinic() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len = %d, quería 1", len(items))
	}
	if items[0].ID() != sc.id {
		t.Errorf("ID = %v, quería %v", items[0].ID(), sc.id)
	}
}

func TestFindByClinic_SinEspecialidad_QueryError(t *testing.T) {
	sentinel := errors.New("db: timeout")
	q := querier(errQuery(sentinel))
	_, err := newRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()), nil)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestFindByClinic_SinEspecialidad_ScanError(t *testing.T) {
	sentinel := errors.New("scan: null en non-nullable")
	sc := &mockRowScanner{err: sentinel}
	q := querier(okQuery(profRows(sc)))
	_, err := newRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()), nil)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestFindByClinic_SinEspecialidad_RowsErr(t *testing.T) {
	sentinel := errors.New("network error")
	q := querier(okQuery(errorRows(sentinel)))
	_, err := newRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()), nil)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería rows.Err() sentinel", err)
	}
}

func TestFindByClinic_ConEspecialidad_PasaEspecialidadComoArg(t *testing.T) {
	q := querier(okQuery(emptyRows()))
	specialty := valueobject.SpecialtyOrthodontics
	newRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()), &specialty)

	// Cuando hay especialidad, el query tiene $2, así que lastArgs tiene 2 elementos.
	if len(q.lastArgs) < 2 {
		t.Errorf("lastArgs len = %d, quería ≥2 (clinicID + specialty)", len(q.lastArgs))
		return
	}
	if q.lastArgs[1] != string(specialty) {
		t.Errorf("lastArgs[1] = %v, quería %q", q.lastArgs[1], specialty)
	}
}

// ── FindBySpecialty ───────────────────────────────────────────────

func TestFindBySpecialty_Exitoso_SinResultados(t *testing.T) {
	q := querier(okQuery(emptyRows()))
	items, err := newRepo(q).FindBySpecialty(context.Background(), valueobject.SpecialtyGeneralDentistry)
	if err != nil {
		t.Fatalf("FindBySpecialty() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, quería 0", len(items))
	}
}

func TestFindBySpecialty_QueryError(t *testing.T) {
	sentinel := errors.New("db: down")
	q := querier(errQuery(sentinel))
	_, err := newRepo(q).FindBySpecialty(context.Background(), valueobject.SpecialtyGeneralDentistry)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

// ── FindAvailableAt ───────────────────────────────────────────────

func TestFindAvailableAt_DelegaAFindByClinic(t *testing.T) {
	sc := newValidScanner()
	q := querier(okQuery(profRows(sc)), okQuery(emptyRows()))
	items, err := newRepo(q).FindAvailableAt(context.Background(), sharedtypes.ClinicID(uuid.New()), time.Now(), nil)
	if err != nil {
		t.Fatalf("FindAvailableAt() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("len = %d, quería 1 (mismo resultado que FindByClinic)", len(items))
	}
}

// ── Search ────────────────────────────────────────────────────────

func TestSearch_SinResultados_RetornaSliceVacio(t *testing.T) {
	q := querier(okQuery(emptyRows()))
	items, err := newRepo(q).Search(context.Background(), sharedtypes.ClinicID(uuid.New()), "ortodoncia")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if items == nil {
		t.Error("retornó nil, quería slice vacío no-nil")
	}
}

func TestSearch_QueryError(t *testing.T) {
	sentinel := errors.New("db: conn refused")
	q := querier(errQuery(sentinel))
	_, err := newRepo(q).Search(context.Background(), sharedtypes.ClinicID(uuid.New()), "juan")
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestSearch_ScanError(t *testing.T) {
	sentinel := errors.New("scan: type mismatch")
	sc := &mockRowScanner{err: sentinel}
	q := querier(okQuery(profRows(sc)))
	_, err := newRepo(q).Search(context.Background(), sharedtypes.ClinicID(uuid.New()), "ana")
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestSearch_RowsErr(t *testing.T) {
	sentinel := errors.New("network: broken pipe")
	q := querier(okQuery(errorRows(sentinel)))
	_, err := newRepo(q).Search(context.Background(), sharedtypes.ClinicID(uuid.New()), "test")
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería rows.Err() sentinel", err)
	}
}

func TestSearch_LoadLicensesError(t *testing.T) {
	sentinel := errors.New("db: license query failed")
	sc := newValidScanner()
	// primera Query = profesionales, segunda Query = loadLicenses → error
	q := querier(okQuery(profRows(sc)), errQuery(sentinel))
	_, err := newRepo(q).Search(context.Background(), sharedtypes.ClinicID(uuid.New()), "juan")
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel de loadLicenses", err)
	}
}

func TestSearch_ConResultado_RetornaProfessional(t *testing.T) {
	sc := newValidScanner()
	q := querier(okQuery(profRows(sc)), okQuery(emptyRows()))
	items, err := newRepo(q).Search(context.Background(), sharedtypes.ClinicID(uuid.New()), "juan")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(items) != 1 || items[0].ID() != sc.id {
		t.Errorf("items = %v, quería 1 item con ID=%v", items, sc.id)
	}
}

// ── Stubs sin conexión ────────────────────────────────────────────

func TestSave_RetornaNotImplemented(t *testing.T) {
	if err := NewProfessionalPostgresRepository(nil).Save(context.Background(), nil); err == nil {
		t.Fatal("quería error 'not implemented'")
	}
}

func TestUpdate_RetornaNotImplemented(t *testing.T) {
	if err := NewProfessionalPostgresRepository(nil).Update(context.Background(), nil); err == nil {
		t.Fatal("quería error 'not implemented'")
	}
}

func TestFindWithExpiringLicenses_RetornaSliceVacio(t *testing.T) {
	profs, err := NewProfessionalPostgresRepository(nil).FindWithExpiringLicenses(context.Background(), 30)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(profs) != 0 {
		t.Errorf("len = %d, quería 0", len(profs))
	}
}

func TestExistsByNationalID_RetornaFalse(t *testing.T) {
	exists, err := NewProfessionalPostgresRepository(nil).ExistsByNationalID(context.Background(), "12345678")
	if err != nil || exists {
		t.Errorf("exists=%v err=%v, quería false/nil", exists, err)
	}
}
