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
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
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

// ── scanApptRow ───────────────────────────────────────────────────

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
	sc.slotEnd = sc.slotStart // end == start → NewTimeSlot falla
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

// ── scanAvailabilitySchedule ──────────────────────────────────────

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
	// procedureDurationsJSON es nil/vacío

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
	// jsonUnmarshal ignora el error — el schedule se construye con 0 working hours
	sc := baseScheduleScanner()
	sc.workingHoursJSON = []byte(`no-es-json`)

	sched, err := scanAvailabilitySchedule(sc)
	if err != nil {
		t.Fatalf("scanAvailabilitySchedule() no debe fallar con JSON inválido en working hours: %v", err)
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

// ── jsonUnmarshal ─────────────────────────────────────────────────

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

// ── nullString ────────────────────────────────────────────────────

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

// ── derefString ───────────────────────────────────────────────────

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

// ── constructores ─────────────────────────────────────────────────

func TestNewAppointmentPostgresRepository_NotNil(t *testing.T) {
	repo := NewAppointmentPostgresRepository(nil)
	if repo == nil {
		t.Fatal("NewAppointmentPostgresRepository(nil) = nil")
	}
}

func TestNewAvailabilitySchedulePostgresRepository_NotNil(t *testing.T) {
	repo := NewAvailabilitySchedulePostgresRepository(nil)
	if repo == nil {
		t.Fatal("NewAvailabilitySchedulePostgresRepository(nil) = nil")
	}
}

func TestNewSlotHoldPostgresRepository_NotNil(t *testing.T) {
	repo := NewSlotHoldPostgresRepository(nil)
	if repo == nil {
		t.Fatal("NewSlotHoldPostgresRepository(nil) = nil")
	}
}

// ── AvailabilitySchedulePostgresRepository stubs ──────────────────

func TestAvailabilitySchedulePostgresRepository_Save_ReturnsNil(t *testing.T) {
	repo := NewAvailabilitySchedulePostgresRepository(nil)
	err := repo.Save(context.TODO(), &aggregate.AvailabilitySchedule{})
	if err != nil {
		t.Errorf("Save() = %v, quería nil", err)
	}
}

func TestAvailabilitySchedulePostgresRepository_Update_ReturnsNil(t *testing.T) {
	repo := NewAvailabilitySchedulePostgresRepository(nil)
	err := repo.Update(context.TODO(), &aggregate.AvailabilitySchedule{})
	if err != nil {
		t.Errorf("Update() = %v, quería nil", err)
	}
}

// ── compile-time interface checks ─────────────────────────────────

var _ rowScanner = (*mockApptScanner)(nil)
var _ scheduleScanner = (*mockScheduleScanner)(nil)

// ── helper: verify accessor presence on aggregate types ───────────

func TestApptAccessors_AllPresent(t *testing.T) {
	sc := baseApptScanner()
	appt, err := scanApptRow(sc)
	if err != nil {
		t.Fatalf("scanApptRow() error = %v", err)
	}
	// Ejercitar todos los accessors para asegurar que no paniquen
	_ = fmt.Sprintf("%v %v %v %v %v %q %v %v %q %q %v %v %v %v %d",
		appt.ID(), appt.PatientID(), appt.BookedByID(), appt.ProfessionalID(), appt.ClinicID(),
		appt.ProcedureCode(), appt.Slot(), appt.Status(),
		appt.CoverageType(), appt.ClinicalNotes(),
		appt.CancelledAt(), appt.CancelledByUserID(), appt.CreatedAt(), appt.UpdatedAt(),
		appt.Version(),
	)
}
