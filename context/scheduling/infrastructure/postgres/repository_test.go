package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/repository"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── mockApptScanner ───────────────────────────────────────────────

type mockApptScanner struct {
	id                 uuid.UUID
	patientID          uuid.UUID
	bookedByID         uuid.UUID
	professionalID     uuid.UUID
	clinicID           uuid.UUID
	procedureCode      string
	slotStart          time.Time
	slotEnd            time.Time
	status             string
	coverageType       *string
	agreementID        *uuid.UUID
	requiresAuthID     *string
	clinicalNotes      *string
	cancellationReason *string
	cancellationNote   *string
	cancelledAt        *time.Time
	cancelledByUserID  *uuid.UUID
	isLateCancellation bool
	createdAt          time.Time
	updatedAt          time.Time
	createdBy          uuid.UUID
	version            int64
	err                error
}

func (m *mockApptScanner) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	*(dest[0].(*uuid.UUID)) = m.id
	*(dest[1].(*uuid.UUID)) = m.patientID
	*(dest[2].(*uuid.UUID)) = m.bookedByID
	*(dest[3].(*uuid.UUID)) = m.professionalID
	*(dest[4].(*uuid.UUID)) = m.clinicID
	*(dest[5].(*string)) = m.procedureCode
	*(dest[6].(*time.Time)) = m.slotStart
	*(dest[7].(*time.Time)) = m.slotEnd
	*(dest[8].(*string)) = m.status
	*(dest[9].(**string)) = m.coverageType
	*(dest[10].(**uuid.UUID)) = m.agreementID
	*(dest[11].(**string)) = m.requiresAuthID
	*(dest[12].(**string)) = m.clinicalNotes
	*(dest[13].(**string)) = m.cancellationReason
	*(dest[14].(**string)) = m.cancellationNote
	*(dest[15].(**time.Time)) = m.cancelledAt
	*(dest[16].(**uuid.UUID)) = m.cancelledByUserID
	*(dest[17].(*bool)) = m.isLateCancellation
	*(dest[18].(*time.Time)) = m.createdAt
	*(dest[19].(*time.Time)) = m.updatedAt
	*(dest[20].(*uuid.UUID)) = m.createdBy
	*(dest[21].(*int64)) = m.version
	return nil
}

func baseApptScanner() *mockApptScanner {
	now := time.Now().UTC().Truncate(time.Second)
	return &mockApptScanner{
		id:             uuid.New(),
		patientID:      uuid.New(),
		bookedByID:     uuid.New(),
		professionalID: uuid.New(),
		clinicID:       uuid.New(),
		procedureCode:  "D0150",
		slotStart:      now.Add(24 * time.Hour),
		slotEnd:        now.Add(24*time.Hour + 30*time.Minute),
		status:         "Pending",
		createdAt:      now,
		updatedAt:      now,
		createdBy:      uuid.New(),
		version:        1,
	}
}

// ── mockScheduleScanner ───────────────────────────────────────────

type mockScheduleScanner struct {
	id                     uuid.UUID
	professionalID         uuid.UUID
	clinicID               uuid.UUID
	workingHoursJSON       []byte
	exceptionDaysJSON      []byte
	blockedSlotsJSON       []byte
	bookedSlotsJSON        []byte
	procedureDurationsJSON []byte
	isActive               bool
	updatedAt              time.Time
	version                int64
	err                    error
}

func (m *mockScheduleScanner) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	*(dest[0].(*uuid.UUID)) = m.id
	*(dest[1].(*uuid.UUID)) = m.professionalID
	*(dest[2].(*uuid.UUID)) = m.clinicID
	*(dest[3].(*[]byte)) = m.workingHoursJSON
	*(dest[4].(*[]byte)) = m.exceptionDaysJSON
	*(dest[5].(*[]byte)) = m.blockedSlotsJSON
	*(dest[6].(*[]byte)) = m.bookedSlotsJSON
	*(dest[7].(*[]byte)) = m.procedureDurationsJSON
	*(dest[8].(*bool)) = m.isActive
	*(dest[9].(*time.Time)) = m.updatedAt
	*(dest[10].(*int64)) = m.version
	return nil
}

func baseScheduleScanner() *mockScheduleScanner {
	return &mockScheduleScanner{
		id:             uuid.New(),
		professionalID: uuid.New(),
		clinicID:       uuid.New(),
		isActive:       true,
		updatedAt:      time.Now().UTC().Truncate(time.Second),
		version:        3,
	}
}

// ── mockTimeScanner (para ActiveStartTimesForDay) ─────────────────

type mockTimeScanner struct {
	t   time.Time
	err error
}

func (m *mockTimeScanner) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	*(dest[0].(*time.Time)) = m.t
	return nil
}

// ── funcRow (pgx.Row con función de scan configurable) ────────────

type funcRow struct {
	fn func(dest ...any) error
}

var _ pgx.Row = (*funcRow)(nil)

func (r *funcRow) Scan(dest ...any) error { return r.fn(dest...) }

func intRow(n int) *funcRow {
	return &funcRow{fn: func(dest ...any) error {
		*(dest[0].(*int)) = n
		return nil
	}}
}

func boolRow(b bool) *funcRow {
	return &funcRow{fn: func(dest ...any) error {
		*(dest[0].(*bool)) = b
		return nil
	}}
}

func errRow(err error) *funcRow {
	return &funcRow{fn: func(dest ...any) error { return err }}
}

// ── genericRows (pgx.Rows con []rowScanner) ───────────────────────

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
func errorRows(err error) *genericRows { return &genericRows{rowsErr: err} }

func apptRows(ss ...*mockApptScanner) *genericRows {
	rs := make([]rowScanner, len(ss))
	for i, s := range ss {
		rs[i] = s
	}
	return &genericRows{scanners: rs}
}

func scheduleRows(ss ...*mockScheduleScanner) *genericRows {
	rs := make([]rowScanner, len(ss))
	for i, s := range ss {
		rs[i] = s
	}
	return &genericRows{scanners: rs}
}

func timeRows(ts ...time.Time) *genericRows {
	rs := make([]rowScanner, len(ts))
	for i, t := range ts {
		rs[i] = &mockTimeScanner{t: t}
	}
	return &genericRows{scanners: rs}
}

// ── mockQuerier ───────────────────────────────────────────────────

type queryResp struct {
	rows pgx.Rows
	err  error
}

type mockQuerier struct {
	execTag   pgconn.CommandTag
	execErr   error
	rowRes    pgx.Row   // respuesta para QueryRow
	queries   []queryResp
	queryIdx  int
	sqls      []string
	lastArgs  []any
}

var _ dbQuerier = (*mockQuerier)(nil)

func (m *mockQuerier) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	m.sqls = append(m.sqls, sql)
	m.lastArgs = args
	return m.execTag, m.execErr
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
	return m.rowRes
}

func newQuerier(responses ...queryResp) *mockQuerier {
	return &mockQuerier{queries: responses}
}

func okQuery(rows pgx.Rows) queryResp  { return queryResp{rows: rows} }
func errQuery(err error) queryResp     { return queryResp{err: err} }

// ── helpers para construir repos con mock ─────────────────────────

func newApptRepo(q dbQuerier) *AppointmentPostgresRepository {
	return &AppointmentPostgresRepository{pool: q}
}

func newSchedRepo(q dbQuerier) *AvailabilitySchedulePostgresRepository {
	return &AvailabilitySchedulePostgresRepository{pool: q}
}

func newHoldRepo(q dbQuerier) *SlotHoldPostgresRepository {
	return &SlotHoldPostgresRepository{pool: q}
}

// ═══════════════════════════════════════════════════════════════════
// scanApptRow
// ═══════════════════════════════════════════════════════════════════

func TestScanApptRow_ValidMinimal(t *testing.T) {
	sc := baseApptScanner()
	appt, err := scanApptRow(sc)
	if err != nil {
		t.Fatalf("scanApptRow() error = %v", err)
	}
	if appt.ID() != sc.id {
		t.Errorf("ID = %v, quería %v", appt.ID(), sc.id)
	}
	if appt.PatientID() != sc.patientID {
		t.Errorf("PatientID = %v, quería %v", appt.PatientID(), sc.patientID)
	}
	if appt.ProfessionalID() != sc.professionalID {
		t.Errorf("ProfessionalID = %v, quería %v", appt.ProfessionalID(), sc.professionalID)
	}
	if appt.ProcedureCode() != sc.procedureCode {
		t.Errorf("ProcedureCode = %q, quería %q", appt.ProcedureCode(), sc.procedureCode)
	}
	if appt.Version() != sc.version {
		t.Errorf("Version = %d, quería %d", appt.Version(), sc.version)
	}
}

func TestScanApptRow_SlotTimesPreserved(t *testing.T) {
	sc := baseApptScanner()
	appt, err := scanApptRow(sc)
	if err != nil {
		t.Fatalf("scanApptRow() error = %v", err)
	}
	slot := appt.Slot()
	if !slot.Start.Equal(sc.slotStart) {
		t.Errorf("Slot.Start = %v, quería %v", slot.Start, sc.slotStart)
	}
	if !slot.End.Equal(sc.slotEnd) {
		t.Errorf("Slot.End = %v, quería %v", slot.End, sc.slotEnd)
	}
}

func TestScanApptRow_NullableFieldsNil(t *testing.T) {
	sc := baseApptScanner()
	appt, err := scanApptRow(sc)
	if err != nil {
		t.Fatalf("scanApptRow() error = %v", err)
	}
	if appt.CoverageType() != "" {
		t.Errorf("CoverageType = %q, quería vacío", appt.CoverageType())
	}
	if appt.AgreementID() != nil {
		t.Errorf("AgreementID = %v, quería nil", appt.AgreementID())
	}
	if appt.ClinicalNotes() != "" {
		t.Errorf("ClinicalNotes = %q, quería vacío", appt.ClinicalNotes())
	}
	if appt.CancelledAt() != nil {
		t.Errorf("CancelledAt = %v, quería nil", appt.CancelledAt())
	}
}

func TestScanApptRow_NullableFieldsPopulated(t *testing.T) {
	sc := baseApptScanner()
	coverage := "OSDE"
	authID := "AUTH-001"
	notes := "notas clínicas"
	agID := uuid.New()
	cancelledByID := uuid.New()
	ts := time.Now().UTC().Add(-time.Hour)
	reason := "patient_request"
	cancelNote := "el paciente llamó"

	sc.status = "Cancelled"
	sc.coverageType = &coverage
	sc.agreementID = &agID
	sc.requiresAuthID = &authID
	sc.clinicalNotes = &notes
	sc.cancellationReason = &reason
	sc.cancellationNote = &cancelNote
	sc.cancelledAt = &ts
	sc.cancelledByUserID = &cancelledByID
	sc.isLateCancellation = true

	appt, err := scanApptRow(sc)
	if err != nil {
		t.Fatalf("scanApptRow() error = %v", err)
	}
	if appt.CoverageType() != coverage {
		t.Errorf("CoverageType = %q, quería %q", appt.CoverageType(), coverage)
	}
	if appt.AgreementID() == nil || *appt.AgreementID() != agID {
		t.Errorf("AgreementID = %v, quería %v", appt.AgreementID(), agID)
	}
	if appt.ClinicalNotes() != notes {
		t.Errorf("ClinicalNotes = %q, quería %q", appt.ClinicalNotes(), notes)
	}
	if appt.CancelledAt() == nil || !appt.CancelledAt().Equal(ts) {
		t.Errorf("CancelledAt = %v, quería %v", appt.CancelledAt(), ts)
	}
	if appt.CancelledByUserID() == nil || *appt.CancelledByUserID() != cancelledByID {
		t.Errorf("CancelledByUserID = %v, quería %v", appt.CancelledByUserID(), cancelledByID)
	}
	if !appt.IsLateCancellation() {
		t.Error("IsLateCancellation = false, quería true")
	}
	if string(appt.CancellationReason()) != reason {
		t.Errorf("CancellationReason = %q, quería %q", appt.CancellationReason(), reason)
	}
}

func TestScanApptRow_ErrNoRows(t *testing.T) {
	sc := &mockApptScanner{err: pgx.ErrNoRows}
	_, err := scanApptRow(sc)
	if err == nil {
		t.Fatal("se esperaba error para ErrNoRows")
	}
	if err.Error() != "appointment not found" {
		t.Errorf("error = %q, quería %q", err.Error(), "appointment not found")
	}
}

func TestScanApptRow_GenericScanError(t *testing.T) {
	sentinel := errors.New("db connection lost")
	sc := &mockApptScanner{err: sentinel}
	_, err := scanApptRow(sc)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería el error sentinel", err)
	}
}

func TestScanApptRow_InvalidSlot_EndEqualsStart(t *testing.T) {
	sc := baseApptScanner()
	sc.slotEnd = sc.slotStart
	_, err := scanApptRow(sc)
	if err == nil {
		t.Fatal("se esperaba error con slot inválido (end == start)")
	}
}

func TestScanApptRow_InvalidStatus(t *testing.T) {
	sc := baseApptScanner()
	sc.status = "EstadoInexistente"
	_, err := scanApptRow(sc)
	if err == nil {
		t.Fatal("se esperaba error con status inválido")
	}
}

func TestScanApptRow_StatusesRoundTrip(t *testing.T) {
	statuses := []string{"Pending", "Confirmed", "InProgress", "Completed", "Cancelled", "NoShow"}
	for _, s := range statuses {
		sc := baseApptScanner()
		sc.status = s
		appt, err := scanApptRow(sc)
		if err != nil {
			t.Errorf("status %q: scanApptRow() error = %v", s, err)
			continue
		}
		if appt.Status().String() != s {
			t.Errorf("status %q: Status() = %q", s, appt.Status())
		}
	}
}

// ═══════════════════════════════════════════════════════════════════
// scanAvailabilitySchedule
// ═══════════════════════════════════════════════════════════════════

func TestScanAvailabilitySchedule_ValidEmptyJSON(t *testing.T) {
	sc := baseScheduleScanner()
	sched, err := scanAvailabilitySchedule(sc)
	if err != nil {
		t.Fatalf("scanAvailabilitySchedule() error = %v", err)
	}
	if sched.ID() != sc.id {
		t.Errorf("ID = %v, quería %v", sched.ID(), sc.id)
	}
	if sched.ProfessionalID() != sharedtypes.ProfessionalID(sc.professionalID) {
		t.Errorf("ProfessionalID = %v, quería %v", sched.ProfessionalID(), sc.professionalID)
	}
	if sched.ClinicID() != sharedtypes.ClinicID(sc.clinicID) {
		t.Errorf("ClinicID = %v, quería %v", sched.ClinicID(), sc.clinicID)
	}
	if !sched.IsActive() {
		t.Error("IsActive = false, quería true")
	}
	if sched.Version() != sc.version {
		t.Errorf("Version = %d, quería %d", sched.Version(), sc.version)
	}
	if len(sched.WorkingHours()) != 0 {
		t.Errorf("WorkingHours len = %d, quería 0", len(sched.WorkingHours()))
	}
}

func TestScanAvailabilitySchedule_WorkingHoursJSON(t *testing.T) {
	sc := baseScheduleScanner()
	sc.workingHoursJSON = []byte(`[
		{"weekday":1,"start_hour":8,"start_min":0,"end_hour":17,"end_min":30},
		{"weekday":3,"start_hour":9,"start_min":15,"end_hour":13,"end_min":0}
	]`)

	sched, err := scanAvailabilitySchedule(sc)
	if err != nil {
		t.Fatalf("scanAvailabilitySchedule() error = %v", err)
	}

	wh := sched.WorkingHours()
	if len(wh) != 2 {
		t.Fatalf("WorkingHours len = %d, quería 2", len(wh))
	}

	first := wh[0]
	if first.Weekday != time.Monday {
		t.Errorf("Weekday = %v, quería Monday", first.Weekday)
	}
	if first.StartHour != 8 || first.StartMin != 0 {
		t.Errorf("StartHour/Min = %d:%d, quería 8:0", first.StartHour, first.StartMin)
	}
	if first.EndHour != 17 || first.EndMin != 30 {
		t.Errorf("EndHour/Min = %d:%d, quería 17:30", first.EndHour, first.EndMin)
	}

	second := wh[1]
	if second.Weekday != time.Wednesday {
		t.Errorf("Weekday = %v, quería Wednesday", second.Weekday)
	}
	if second.StartHour != 9 || second.StartMin != 15 {
		t.Errorf("StartHour/Min = %d:%d, quería 9:15", second.StartHour, second.StartMin)
	}
}

func TestScanAvailabilitySchedule_ProcedureDurations(t *testing.T) {
	sc := baseScheduleScanner()
	sc.procedureDurationsJSON = []byte(`{"D0150":60,"D1110":30,"D7140":45}`)

	sched, err := scanAvailabilitySchedule(sc)
	if err != nil {
		t.Fatalf("scanAvailabilitySchedule() error = %v", err)
	}

	pd := sched.ProcedureDurations()
	if pd["D0150"] != 60 {
		t.Errorf("D0150 = %d, quería 60", pd["D0150"])
	}
	if pd["D1110"] != 30 {
		t.Errorf("D1110 = %d, quería 30", pd["D1110"])
	}
	if pd["D7140"] != 45 {
		t.Errorf("D7140 = %d, quería 45", pd["D7140"])
	}
}

func TestScanAvailabilitySchedule_NilProcedureDurationsReturnsEmptyMap(t *testing.T) {
	sc := baseScheduleScanner()
	sched, err := scanAvailabilitySchedule(sc)
	if err != nil {
		t.Fatalf("scanAvailabilitySchedule() error = %v", err)
	}
	pd := sched.ProcedureDurations()
	if pd == nil {
		t.Error("ProcedureDurations = nil, quería mapa vacío (no nil)")
	}
	if len(pd) != 0 {
		t.Errorf("ProcedureDurations len = %d, quería 0", len(pd))
	}
}

func TestScanAvailabilitySchedule_IsActiveFalse(t *testing.T) {
	sc := baseScheduleScanner()
	sc.isActive = false
	sched, err := scanAvailabilitySchedule(sc)
	if err != nil {
		t.Fatalf("scanAvailabilitySchedule() error = %v", err)
	}
	if sched.IsActive() {
		t.Error("IsActive = true, quería false")
	}
}

func TestScanAvailabilitySchedule_ErrNoRows(t *testing.T) {
	sc := &mockScheduleScanner{err: pgx.ErrNoRows}
	_, err := scanAvailabilitySchedule(sc)
	if err == nil {
		t.Fatal("se esperaba error para ErrNoRows")
	}
	if err.Error() != "schedule not found" {
		t.Errorf("error = %q, quería %q", err.Error(), "schedule not found")
	}
}

func TestScanAvailabilitySchedule_GenericScanError(t *testing.T) {
	sentinel := errors.New("timeout de la base de datos")
	sc := &mockScheduleScanner{err: sentinel}
	_, err := scanAvailabilitySchedule(sc)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería el error sentinel", err)
	}
}

func TestScanAvailabilitySchedule_InvalidWorkingHoursJSON_IgnoredGracefully(t *testing.T) {
	sc := baseScheduleScanner()
	sc.workingHoursJSON = []byte(`no-es-json`)
	sched, err := scanAvailabilitySchedule(sc)
	if err != nil {
		t.Fatalf("scanAvailabilitySchedule() no debe fallar con JSON inválido: %v", err)
	}
	if len(sched.WorkingHours()) != 0 {
		t.Errorf("WorkingHours len = %d, quería 0 tras JSON inválido", len(sched.WorkingHours()))
	}
}

func TestScanAvailabilitySchedule_UpdatedAtPreserved(t *testing.T) {
	sc := baseScheduleScanner()
	expected := time.Date(2026, 5, 20, 14, 30, 0, 0, time.UTC)
	sc.updatedAt = expected
	sched, err := scanAvailabilitySchedule(sc)
	if err != nil {
		t.Fatalf("scanAvailabilitySchedule() error = %v", err)
	}
	if !sched.UpdatedAt().Equal(expected) {
		t.Errorf("UpdatedAt = %v, quería %v", sched.UpdatedAt(), expected)
	}
}

// ═══════════════════════════════════════════════════════════════════
// jsonUnmarshal
// ═══════════════════════════════════════════════════════════════════

func TestJsonUnmarshal_NilData(t *testing.T) {
	var v map[string]int
	if err := jsonUnmarshal(nil, &v); err != nil {
		t.Errorf("jsonUnmarshal(nil) error = %v, quería nil", err)
	}
	if v != nil {
		t.Errorf("v fue modificado con nil data: %v", v)
	}
}

func TestJsonUnmarshal_EmptyData(t *testing.T) {
	var v map[string]int
	if err := jsonUnmarshal([]byte{}, &v); err != nil {
		t.Errorf("jsonUnmarshal([]) error = %v, quería nil", err)
	}
	if v != nil {
		t.Errorf("v fue modificado con data vacía: %v", v)
	}
}

func TestJsonUnmarshal_ValidJSON(t *testing.T) {
	data, _ := json.Marshal(map[string]int{"D0150": 60})
	var v map[string]int
	if err := jsonUnmarshal(data, &v); err != nil {
		t.Fatalf("jsonUnmarshal() error = %v", err)
	}
	if v["D0150"] != 60 {
		t.Errorf("v[D0150] = %d, quería 60", v["D0150"])
	}
}

func TestJsonUnmarshal_InvalidJSON(t *testing.T) {
	var v map[string]int
	err := jsonUnmarshal([]byte(`{no válido}`), &v)
	if err == nil {
		t.Fatal("se esperaba error con JSON inválido")
	}
}

// ═══════════════════════════════════════════════════════════════════
// nullString / derefString
// ═══════════════════════════════════════════════════════════════════

func TestNullString_EmptyReturnsNil(t *testing.T) {
	if got := nullString(""); got != nil {
		t.Errorf("nullString(\"\") = %v, quería nil", got)
	}
}

func TestNullString_NonEmptyReturnsPointer(t *testing.T) {
	s := "hola"
	got := nullString(s)
	if got == nil {
		t.Fatal("nullString(\"hola\") = nil, quería puntero")
	}
	if *got != s {
		t.Errorf("*nullString = %q, quería %q", *got, s)
	}
}

func TestNullString_DoesNotAliasInput(t *testing.T) {
	s := "original"
	got := nullString(s)
	s = "mutado"
	if *got != "original" {
		t.Errorf("nullString devolvió alias del string origen; *got = %q", *got)
	}
}

func TestDerefString_NilReturnsEmpty(t *testing.T) {
	if got := derefString(nil); got != "" {
		t.Errorf("derefString(nil) = %q, quería \"\"", got)
	}
}

func TestDerefString_NonNilReturnsValue(t *testing.T) {
	s := "contenido"
	if got := derefString(&s); got != s {
		t.Errorf("derefString(&s) = %q, quería %q", got, s)
	}
}

// ═══════════════════════════════════════════════════════════════════
// Constructores
// ═══════════════════════════════════════════════════════════════════

func TestNewAppointmentPostgresRepository_NotNil(t *testing.T) {
	if NewAppointmentPostgresRepository(nil) == nil {
		t.Fatal("NewAppointmentPostgresRepository(nil) = nil")
	}
}

func TestNewAvailabilitySchedulePostgresRepository_NotNil(t *testing.T) {
	if NewAvailabilitySchedulePostgresRepository(nil) == nil {
		t.Fatal("NewAvailabilitySchedulePostgresRepository(nil) = nil")
	}
}

func TestNewSlotHoldPostgresRepository_NotNil(t *testing.T) {
	if NewSlotHoldPostgresRepository(nil) == nil {
		t.Fatal("NewSlotHoldPostgresRepository(nil) = nil")
	}
}

// ═══════════════════════════════════════════════════════════════════
// AppointmentPostgresRepository
// ═══════════════════════════════════════════════════════════════════

func TestApptRepo_Save_Exitoso(t *testing.T) {
	sc := baseApptScanner()
	q := &mockQuerier{}
	// Construimos un Appointment via scanApptRow para pasarlo a Save
	appt, _ := scanApptRow(sc)
	if err := newApptRepo(q).Save(context.Background(), appt); err != nil {
		t.Errorf("Save() error = %v, quería nil", err)
	}
}

func TestApptRepo_Save_Error(t *testing.T) {
	sentinel := errors.New("db: constraint violation")
	q := &mockQuerier{execErr: sentinel}
	appt, _ := scanApptRow(baseApptScanner())
	err := newApptRepo(q).Save(context.Background(), appt)
	if !errors.Is(err, sentinel) {
		t.Errorf("Save() error = %v, quería sentinel", err)
	}
}

func TestApptRepo_Update_Exitoso(t *testing.T) {
	q := &mockQuerier{execTag: pgconn.NewCommandTag("UPDATE 1")}
	appt, _ := scanApptRow(baseApptScanner())
	if err := newApptRepo(q).Update(context.Background(), appt); err != nil {
		t.Errorf("Update() error = %v, quería nil", err)
	}
}

func TestApptRepo_Update_ExecError(t *testing.T) {
	sentinel := errors.New("db: deadlock")
	q := &mockQuerier{execErr: sentinel}
	appt, _ := scanApptRow(baseApptScanner())
	err := newApptRepo(q).Update(context.Background(), appt)
	if !errors.Is(err, sentinel) {
		t.Errorf("Update() error = %v, quería sentinel", err)
	}
}

func TestApptRepo_Update_RowsAffectedCero_RetornaConflict(t *testing.T) {
	q := &mockQuerier{execTag: pgconn.CommandTag{}} // RowsAffected() == 0
	appt, _ := scanApptRow(baseApptScanner())
	err := newApptRepo(q).Update(context.Background(), appt)
	if err == nil || !errors.As(err, new(interface{ Error() string })) {
		t.Fatalf("Update() error = %v, quería error de conflicto", err)
	}
	if err.Error() != "[CONFLICT] appointment modified concurrently" {
		t.Errorf("error = %q", err.Error())
	}
}

func TestApptRepo_FindByID_Exitoso(t *testing.T) {
	sc := baseApptScanner()
	q := &mockQuerier{rowRes: sc}
	appt, err := newApptRepo(q).FindByID(context.Background(), sharedtypes.AppointmentID(sc.id))
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if appt.ID() != sc.id {
		t.Errorf("ID = %v, quería %v", appt.ID(), sc.id)
	}
}

func TestApptRepo_FindByID_ErrNoRows(t *testing.T) {
	q := &mockQuerier{rowRes: &mockApptScanner{err: pgx.ErrNoRows}}
	_, err := newApptRepo(q).FindByID(context.Background(), sharedtypes.AppointmentID(uuid.New()))
	if err == nil || err.Error() != "appointment not found" {
		t.Errorf("FindByID() error = %v, quería 'appointment not found'", err)
	}
}

func TestApptRepo_FindByID_ScanError(t *testing.T) {
	sentinel := errors.New("scan: null byte")
	q := &mockQuerier{rowRes: &mockApptScanner{err: sentinel}}
	_, err := newApptRepo(q).FindByID(context.Background(), sharedtypes.AppointmentID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("FindByID() error = %v, quería sentinel", err)
	}
}

func TestApptRepo_FindActiveByPatient_SinResultados(t *testing.T) {
	q := newQuerier(okQuery(emptyRows()))
	items, err := newApptRepo(q).FindActiveByPatient(context.Background(), sharedtypes.PatientID(uuid.New()))
	if err != nil {
		t.Fatalf("FindActiveByPatient() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, quería 0", len(items))
	}
}

func TestApptRepo_FindActiveByPatient_ConResultado(t *testing.T) {
	sc := baseApptScanner()
	q := newQuerier(okQuery(apptRows(sc)))
	items, err := newApptRepo(q).FindActiveByPatient(context.Background(), sharedtypes.PatientID(uuid.New()))
	if err != nil {
		t.Fatalf("FindActiveByPatient() error = %v", err)
	}
	if len(items) != 1 || items[0].ID() != sc.id {
		t.Errorf("items = %v, quería 1 item con ID=%v", items, sc.id)
	}
}

func TestApptRepo_FindActiveByPatient_QueryError(t *testing.T) {
	sentinel := errors.New("db: timeout")
	q := newQuerier(errQuery(sentinel))
	_, err := newApptRepo(q).FindActiveByPatient(context.Background(), sharedtypes.PatientID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("FindActiveByPatient() error = %v, quería sentinel", err)
	}
}

func TestApptRepo_FindActiveByPatient_ScanError(t *testing.T) {
	sentinel := errors.New("scan: type mismatch")
	q := newQuerier(okQuery(apptRows(&mockApptScanner{err: sentinel})))
	_, err := newApptRepo(q).FindActiveByPatient(context.Background(), sharedtypes.PatientID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("FindActiveByPatient() scan error = %v, quería sentinel", err)
	}
}

func TestApptRepo_FindActiveByPatient_RowsErr(t *testing.T) {
	sentinel := errors.New("network: broken pipe")
	q := newQuerier(okQuery(errorRows(sentinel)))
	_, err := newApptRepo(q).FindActiveByPatient(context.Background(), sharedtypes.PatientID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("FindActiveByPatient() rows.Err = %v, quería sentinel", err)
	}
}

func TestApptRepo_FindByProfessionalAndDate_SinResultados(t *testing.T) {
	q := newQuerier(okQuery(emptyRows()))
	items, err := newApptRepo(q).FindByProfessionalAndDate(
		context.Background(),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
		time.Now(), time.Now().Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, quería 0", len(items))
	}
}

func TestApptRepo_FindByProfessionalAndDate_QueryError(t *testing.T) {
	sentinel := errors.New("db: conn reset")
	q := newQuerier(errQuery(sentinel))
	_, err := newApptRepo(q).FindByProfessionalAndDate(
		context.Background(),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
		time.Now(), time.Now().Add(24*time.Hour),
	)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestApptRepo_FindByClinicAndDate_SinResultados(t *testing.T) {
	q := newQuerier(okQuery(emptyRows()))
	items, err := newApptRepo(q).FindByClinicAndDate(
		context.Background(),
		sharedtypes.ClinicID(uuid.New()),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, quería 0", len(items))
	}
}

func TestApptRepo_FindByClinicAndDate_ConResultado(t *testing.T) {
	sc := baseApptScanner()
	q := newQuerier(okQuery(apptRows(sc)))
	items, err := newApptRepo(q).FindByClinicAndDate(
		context.Background(),
		sharedtypes.ClinicID(uuid.New()),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("len = %d, quería 1", len(items))
	}
}

func TestApptRepo_FindByClinicAndDate_QueryError(t *testing.T) {
	sentinel := errors.New("db: timeout")
	q := newQuerier(errQuery(sentinel))
	_, err := newApptRepo(q).FindByClinicAndDate(
		context.Background(),
		sharedtypes.ClinicID(uuid.New()),
		time.Now(),
	)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestApptRepo_CountActiveByPatient_Exitoso(t *testing.T) {
	q := &mockQuerier{rowRes: intRow(7)}
	count, err := newApptRepo(q).CountActiveByPatient(context.Background(), sharedtypes.PatientID(uuid.New()))
	if err != nil {
		t.Fatalf("CountActiveByPatient() error = %v", err)
	}
	if count != 7 {
		t.Errorf("count = %d, quería 7", count)
	}
}

func TestApptRepo_CountActiveByPatient_Error(t *testing.T) {
	sentinel := errors.New("db: conn lost")
	q := &mockQuerier{rowRes: errRow(sentinel)}
	_, err := newApptRepo(q).CountActiveByPatient(context.Background(), sharedtypes.PatientID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("CountActiveByPatient() error = %v, quería sentinel", err)
	}
}

// ═══════════════════════════════════════════════════════════════════
// AvailabilitySchedulePostgresRepository
// ═══════════════════════════════════════════════════════════════════

func TestSchedRepo_Save_RetornaNil(t *testing.T) {
	repo := NewAvailabilitySchedulePostgresRepository(nil)
	if err := repo.Save(context.Background(), &aggregate.AvailabilitySchedule{}); err != nil {
		t.Errorf("Save() = %v, quería nil", err)
	}
}

func TestSchedRepo_Update_RetornaNil(t *testing.T) {
	repo := NewAvailabilitySchedulePostgresRepository(nil)
	if err := repo.Update(context.Background(), &aggregate.AvailabilitySchedule{}); err != nil {
		t.Errorf("Update() = %v, quería nil", err)
	}
}

func TestSchedRepo_FindByProfessionalAndClinic_Exitoso(t *testing.T) {
	sc := baseScheduleScanner()
	q := &mockQuerier{rowRes: sc}
	sched, err := newSchedRepo(q).FindByProfessionalAndClinic(
		context.Background(),
		sharedtypes.ProfessionalID(sc.professionalID),
		sharedtypes.ClinicID(sc.clinicID),
	)
	if err != nil {
		t.Fatalf("FindByProfessionalAndClinic() error = %v", err)
	}
	if sched.ID() != sc.id {
		t.Errorf("ID = %v, quería %v", sched.ID(), sc.id)
	}
}

func TestSchedRepo_FindByProfessionalAndClinic_ErrNoRows(t *testing.T) {
	q := &mockQuerier{rowRes: &mockScheduleScanner{err: pgx.ErrNoRows}}
	_, err := newSchedRepo(q).FindByProfessionalAndClinic(
		context.Background(),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
	)
	if err == nil || err.Error() != "schedule not found" {
		t.Errorf("error = %v, quería 'schedule not found'", err)
	}
}

func TestSchedRepo_FindByProfessionalAndClinic_ScanError(t *testing.T) {
	sentinel := errors.New("db: type error")
	q := &mockQuerier{rowRes: &mockScheduleScanner{err: sentinel}}
	_, err := newSchedRepo(q).FindByProfessionalAndClinic(
		context.Background(),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
	)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestSchedRepo_FindByClinic_SinResultados(t *testing.T) {
	q := newQuerier(okQuery(emptyRows()))
	items, err := newSchedRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()))
	if err != nil {
		t.Fatalf("FindByClinic() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, quería 0", len(items))
	}
}

func TestSchedRepo_FindByClinic_ConResultado(t *testing.T) {
	sc := baseScheduleScanner()
	q := newQuerier(okQuery(scheduleRows(sc)))
	items, err := newSchedRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()))
	if err != nil {
		t.Fatalf("FindByClinic() error = %v", err)
	}
	if len(items) != 1 || items[0].ID() != sc.id {
		t.Errorf("items = %v, quería 1 item con ID=%v", items, sc.id)
	}
}

func TestSchedRepo_FindByClinic_QueryError(t *testing.T) {
	sentinel := errors.New("db: conn refused")
	q := newQuerier(errQuery(sentinel))
	_, err := newSchedRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("FindByClinic() error = %v, quería sentinel", err)
	}
}

func TestSchedRepo_FindByClinic_ScanError(t *testing.T) {
	sentinel := errors.New("scan: unexpected null")
	sc := &mockScheduleScanner{err: sentinel}
	q := newQuerier(okQuery(scheduleRows(sc)))
	_, err := newSchedRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("FindByClinic() scan error = %v, quería sentinel", err)
	}
}

func TestSchedRepo_FindByClinic_RowsErr(t *testing.T) {
	sentinel := errors.New("network: EOF")
	q := newQuerier(okQuery(errorRows(sentinel)))
	_, err := newSchedRepo(q).FindByClinic(context.Background(), sharedtypes.ClinicID(uuid.New()))
	if !errors.Is(err, sentinel) {
		t.Errorf("FindByClinic() rows.Err = %v, quería sentinel", err)
	}
}

// ═══════════════════════════════════════════════════════════════════
// SlotHoldPostgresRepository
// ═══════════════════════════════════════════════════════════════════

func newHold() *repository.SlotHold {
	now := time.Now().UTC()
	return &repository.SlotHold{
		ID:             uuid.New(),
		ProfessionalID: uuid.New(),
		ClinicID:       uuid.New(),
		SlotStart:      now.Add(time.Hour),
		SlotEnd:        now.Add(time.Hour + 30*time.Minute),
		HeldBy:         uuid.New(),
		HeldUntil:      now.Add(5 * time.Minute),
	}
}

func TestHoldRepo_Create_Exitoso(t *testing.T) {
	q := &mockQuerier{rowRes: boolRow(true)}
	if err := newHoldRepo(q).Create(context.Background(), newHold()); err != nil {
		t.Errorf("Create() error = %v, quería nil", err)
	}
}

func TestHoldRepo_Create_ExecError(t *testing.T) {
	sentinel := errors.New("db: insert failed")
	q := &mockQuerier{execErr: sentinel}
	err := newHoldRepo(q).Create(context.Background(), newHold())
	if !errors.Is(err, sentinel) {
		t.Errorf("Create() error = %v, quería sentinel", err)
	}
}

func TestHoldRepo_Create_QueryRowError(t *testing.T) {
	sentinel := errors.New("db: select failed")
	q := &mockQuerier{rowRes: errRow(sentinel)}
	err := newHoldRepo(q).Create(context.Background(), newHold())
	if !errors.Is(err, sentinel) {
		t.Errorf("Create() queryrow error = %v, quería sentinel", err)
	}
}

func TestHoldRepo_Create_SlotYaTomado_RetornaConflict(t *testing.T) {
	q := &mockQuerier{rowRes: boolRow(false)} // DO NOTHING disparó, hold no fue insertado
	err := newHoldRepo(q).Create(context.Background(), newHold())
	if err == nil {
		t.Fatal("Create() quería error de conflicto")
	}
	if err.Error() != "[CONFLICT] slot already held by another user" {
		t.Errorf("error = %q", err.Error())
	}
}

func TestHoldRepo_Release_Exitoso(t *testing.T) {
	q := &mockQuerier{}
	if err := newHoldRepo(q).Release(context.Background(), uuid.New()); err != nil {
		t.Errorf("Release() error = %v, quería nil", err)
	}
}

func TestHoldRepo_Release_Error(t *testing.T) {
	sentinel := errors.New("db: delete failed")
	q := &mockQuerier{execErr: sentinel}
	err := newHoldRepo(q).Release(context.Background(), uuid.New())
	if !errors.Is(err, sentinel) {
		t.Errorf("Release() error = %v, quería sentinel", err)
	}
}

func TestHoldRepo_ActiveStartTimesForDay_SinResultados(t *testing.T) {
	q := newQuerier(okQuery(emptyRows()))
	times, err := newHoldRepo(q).ActiveStartTimesForDay(
		context.Background(),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("ActiveStartTimesForDay() error = %v", err)
	}
	if len(times) != 0 {
		t.Errorf("len = %d, quería 0", len(times))
	}
}

func TestHoldRepo_ActiveStartTimesForDay_ConResultados(t *testing.T) {
	t1 := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	t2 := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	q := newQuerier(okQuery(timeRows(t1, t2)))
	times, err := newHoldRepo(q).ActiveStartTimesForDay(
		context.Background(),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("ActiveStartTimesForDay() error = %v", err)
	}
	if len(times) != 2 {
		t.Fatalf("len = %d, quería 2", len(times))
	}
	if !times[0].Equal(t1) {
		t.Errorf("times[0] = %v, quería %v", times[0], t1)
	}
	if !times[1].Equal(t2) {
		t.Errorf("times[1] = %v, quería %v", times[1], t2)
	}
}

func TestHoldRepo_ActiveStartTimesForDay_QueryError(t *testing.T) {
	sentinel := errors.New("db: timeout")
	q := newQuerier(errQuery(sentinel))
	_, err := newHoldRepo(q).ActiveStartTimesForDay(
		context.Background(),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
		time.Now(),
	)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestHoldRepo_ActiveStartTimesForDay_ScanError(t *testing.T) {
	sentinel := errors.New("scan: unexpected type")
	scanErr := &mockTimeScanner{err: sentinel}
	rows := &genericRows{scanners: []rowScanner{scanErr}}
	q := newQuerier(okQuery(rows))
	_, err := newHoldRepo(q).ActiveStartTimesForDay(
		context.Background(),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
		time.Now(),
	)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestHoldRepo_ActiveStartTimesForDay_RowsErr(t *testing.T) {
	sentinel := errors.New("network: broken pipe")
	q := newQuerier(okQuery(errorRows(sentinel)))
	_, err := newHoldRepo(q).ActiveStartTimesForDay(
		context.Background(),
		sharedtypes.ProfessionalID(uuid.New()),
		sharedtypes.ClinicID(uuid.New()),
		time.Now(),
	)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, quería sentinel", err)
	}
}

func TestHoldRepo_DeleteExpired_Exitoso(t *testing.T) {
	q := &mockQuerier{execTag: pgconn.NewCommandTag("DELETE 3")}
	n, err := newHoldRepo(q).DeleteExpired(context.Background())
	if err != nil {
		t.Fatalf("DeleteExpired() error = %v", err)
	}
	if n != 3 {
		t.Errorf("n = %d, quería 3", n)
	}
}

func TestHoldRepo_DeleteExpired_Error(t *testing.T) {
	sentinel := errors.New("db: conn lost")
	q := &mockQuerier{execErr: sentinel}
	_, err := newHoldRepo(q).DeleteExpired(context.Background())
	if !errors.Is(err, sentinel) {
		t.Errorf("DeleteExpired() error = %v, quería sentinel", err)
	}
}

func TestHoldRepo_DeleteExpired_CeroFilas(t *testing.T) {
	q := &mockQuerier{execTag: pgconn.NewCommandTag("DELETE 0")}
	n, err := newHoldRepo(q).DeleteExpired(context.Background())
	if err != nil {
		t.Fatalf("DeleteExpired() error = %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, quería 0", n)
	}
}

// ── compile-time interface checks ─────────────────────────────────

var _ rowScanner = (*mockApptScanner)(nil)
var _ scheduleScanner = (*mockScheduleScanner)(nil)
var _ dbQuerier = (*mockQuerier)(nil)

// ── helper: verify accessor presence on aggregate types ───────────

func TestApptAccessors_AllPresent(t *testing.T) {
	sc := baseApptScanner()
	appt, err := scanApptRow(sc)
	if err != nil {
		t.Fatalf("scanApptRow() error = %v", err)
	}
	_ = fmt.Sprintf("%v %v %v %v %v %q %v %v %q %q %v %v %v %v %d",
		appt.ID(), appt.PatientID(), appt.BookedByID(), appt.ProfessionalID(), appt.ClinicID(),
		appt.ProcedureCode(), appt.Slot(), appt.Status(),
		appt.CoverageType(), appt.ClinicalNotes(),
		appt.CancelledAt(), appt.CancelledByUserID(), appt.CreatedAt(), appt.UpdatedAt(),
		appt.Version(),
	)
}
