package aggregate_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── helpers ──────────────────────────────────────────────────────

func validFullName(t *testing.T) sharedvo.FullName {
	t.Helper()
	n, err := sharedvo.NewFullName("Juan Perez")
	if err != nil {
		t.Fatalf("setup: NewFullName: %v", err)
	}
	return n
}

func validBirthDate(t *testing.T) valueobject.BirthDate {
	t.Helper()
	bd, err := valueobject.NewBirthDate(1990, 1, 1)
	if err != nil {
		t.Fatalf("setup: NewBirthDate: %v", err)
	}
	return bd
}

func minorBirthDate(t *testing.T) valueobject.BirthDate {
	t.Helper()
	bd, err := valueobject.NewBirthDateFromTime(time.Now().AddDate(-10, 0, 0))
	if err != nil {
		t.Fatalf("setup: NewBirthDateFromTime: %v", err)
	}
	return bd
}

func validGender(t *testing.T) valueobject.Gender {
	t.Helper()
	g, err := valueobject.ParseGender("M")
	if err != nil {
		t.Fatalf("setup: ParseGender: %v", err)
	}
	return g
}

func validNationalID(t *testing.T) sharedvo.NationalID {
	t.Helper()
	id, err := sharedvo.NewNationalID(sharedvo.DocDNI, "12345678")
	if err != nil {
		t.Fatalf("setup: NewNationalID: %v", err)
	}
	return id
}

func validPhone(t *testing.T) sharedvo.PhoneNumber {
	t.Helper()
	p, err := sharedvo.NewPhoneNumber("+5491112345678")
	if err != nil {
		t.Fatalf("setup: NewPhoneNumber: %v", err)
	}
	return p
}

func newTestPatient(t *testing.T) *aggregate.Patient {
	t.Helper()
	p, err := aggregate.NewPatient(
		nil, validFullName(t), validBirthDate(t), validGender(t),
		validNationalID(t), validPhone(t), nil,
	)
	if err != nil {
		t.Fatalf("setup: NewPatient: %v", err)
	}
	p.PendingEvents() // limpiar evento del constructor
	return p
}

func domainCode(t *testing.T, err error) sharederrors.ErrorCode {
	t.Helper()
	de, ok := sharederrors.As(err)
	if !ok {
		t.Fatalf("se esperaba *DomainError, se obtuvo %T: %v", err, err)
	}
	return de.Code
}

func newCoverage(t *testing.T, patientID sharedtypes.PatientID, covType valueobject.CoverageType) *coverage.PatientCoverage {
	t.Helper()
	provider := ""
	if covType != valueobject.CoverageTypePrivate {
		provider = "Proveedor Test"
	}
	c, err := coverage.NewPatientCoverage(
		patientID, covType, nil, provider, "PLAN1", "MEMBER1",
		time.Now(), nil, uuid.New(),
	)
	if err != nil {
		t.Fatalf("setup: NewPatientCoverage: %v", err)
	}
	return c
}

// ── NewPatient ────────────────────────────────────────────────────

func TestNewPatient(t *testing.T) {
	t.Run("crea paciente con campos y valores por defecto correctos", func(t *testing.T) {
		fullName := validFullName(t)
		bd := validBirthDate(t)
		gender := validGender(t)
		natID := validNationalID(t)
		phone := validPhone(t)
		userID := uuid.New()
		createdBy := uuid.New()

		p, err := aggregate.NewPatient(&userID, fullName, bd, gender, natID, phone, &createdBy)
		if err != nil {
			t.Fatalf("NewPatient() error = %v", err)
		}

		if p.ID() == (sharedtypes.PatientID{}) {
			t.Error("ID vacío")
		}
		if p.UserID() == nil || *p.UserID() != userID {
			t.Errorf("UserID = %v, se esperaba %v", p.UserID(), userID)
		}
		if p.FullName() != fullName {
			t.Error("FullName no coincide")
		}
		if p.ContactInfo().Phone != phone {
			t.Error("Phone no coincide en ContactInfo")
		}
		if p.Version() != 1 {
			t.Errorf("Version = %d, se esperaba 1", p.Version())
		}
		if p.CreatedBy() == nil || *p.CreatedBy() != createdBy {
			t.Error("CreatedBy no coincide")
		}
		if len(p.Coverages()) != 0 {
			t.Error("Coverages debería estar vacío")
		}
		if len(p.MedicalAlerts()) != 0 {
			t.Error("MedicalAlerts debería estar vacío")
		}
		if p.DentalHistory() == nil {
			t.Fatal("DentalHistory no debería ser nil")
		}
		if p.DentalHistory().VisitCount() != 0 {
			t.Error("VisitCount inicial debería ser 0")
		}
		if p.Preferences().PreferredTimeOfDay != valueobject.TimeOfDayAny {
			t.Errorf("PreferredTimeOfDay = %v, se esperaba %v", p.Preferences().PreferredTimeOfDay, valueobject.TimeOfDayAny)
		}
		if p.Preferences().CommunicationChannel != valueobject.ChannelWhatsApp {
			t.Errorf("CommunicationChannel = %v, se esperaba %v", p.Preferences().CommunicationChannel, valueobject.ChannelWhatsApp)
		}
	})

	t.Run("agrega evento PatientRegistered pendiente", func(t *testing.T) {
		p, err := aggregate.NewPatient(nil, validFullName(t), validBirthDate(t), validGender(t), validNationalID(t), validPhone(t), nil)
		if err != nil {
			t.Fatalf("NewPatient() error = %v", err)
		}
		evts := p.PendingEvents()
		if len(evts) != 1 {
			t.Fatalf("len(events) = %d, se esperaba 1", len(evts))
		}
		if evts[0].EventType() != "patient.registered" {
			t.Errorf("EventType = %q", evts[0].EventType())
		}
	})

	t.Run("PendingEvents limpia el slice tras leerlo", func(t *testing.T) {
		p, _ := aggregate.NewPatient(nil, validFullName(t), validBirthDate(t), validGender(t), validNationalID(t), validPhone(t), nil)
		p.PendingEvents()
		if evts := p.PendingEvents(); len(evts) != 0 {
			t.Errorf("segunda llamada debería estar vacía, obtuvo %d", len(evts))
		}
	})

	t.Run("UserID nil para paciente creado por staff sin cuenta IAM", func(t *testing.T) {
		p, err := aggregate.NewPatient(nil, validFullName(t), validBirthDate(t), validGender(t), validNationalID(t), validPhone(t), nil)
		if err != nil {
			t.Fatalf("NewPatient() error = %v", err)
		}
		if p.UserID() != nil {
			t.Error("UserID debería ser nil")
		}
	})
}

// ── Reconstitute ──────────────────────────────────────────────────

func TestReconstitute(t *testing.T) {
	t.Run("reconstruye sin disparar eventos", func(t *testing.T) {
		id := sharedtypes.PatientID(uuid.New())
		fullName := validFullName(t)
		bd := validBirthDate(t)
		gender := validGender(t)
		natID := validNationalID(t)
		contactInfo := aggregate.ContactInfo{Phone: validPhone(t)}
		geo := &aggregate.GeoPoint{Latitude: -34.6, Longitude: -58.4}
		history := aggregate.ReconstituteDentalHistory(uuid.New(), id, nil, valueobject.RiskLevelLow, 0, nil, time.Now(), "")
		prefs := aggregate.PatientPreferences{PreferredTimeOfDay: valueobject.TimeOfDayAny, CommunicationChannel: valueobject.ChannelEmail}
		now := time.Now()
		createdBy := uuid.New()

		p := aggregate.Reconstitute(
			id, nil, fullName, bd, gender, natID, contactInfo, geo,
			nil, nil, history, prefs, now, now, &createdBy, 7,
		)

		if p.ID() != id {
			t.Errorf("ID = %v, se esperaba %v", p.ID(), id)
		}
		if p.Version() != 7 {
			t.Errorf("Version = %d, se esperaba 7", p.Version())
		}
		if p.HomeLocation() != geo {
			t.Error("HomeLocation no coincide")
		}
		if evts := p.PendingEvents(); len(evts) != 0 {
			t.Errorf("Reconstitute no debe generar eventos, obtuvo %d", len(evts))
		}
	})
}

// ── UpdateContactInfo ─────────────────────────────────────────────

func TestUpdateContactInfo(t *testing.T) {
	p := newTestPatient(t)
	before := p.UpdatedAt()
	time.Sleep(time.Millisecond)

	email, _ := sharedvo.NewEmail("nuevo@example.com")
	newInfo := aggregate.ContactInfo{
		Phone: validPhone(t),
		Email: &email,
	}
	p.UpdateContactInfo(newInfo)

	if p.ContactInfo().Email == nil {
		t.Error("Email debería estar seteado")
	}
	if !p.UpdatedAt().After(before) {
		t.Error("UpdatedAt debería avanzar")
	}
}

// ── SetHomeLocation ───────────────────────────────────────────────

func TestSetHomeLocation(t *testing.T) {
	t.Run("coordenadas válidas se aceptan", func(t *testing.T) {
		p := newTestPatient(t)
		if err := p.SetHomeLocation(-34.6037, -58.3816); err != nil {
			t.Fatalf("SetHomeLocation() error = %v", err)
		}
		if p.HomeLocation() == nil {
			t.Fatal("HomeLocation no debería ser nil")
		}
		if p.HomeLocation().Latitude != -34.6037 {
			t.Errorf("Latitude = %v", p.HomeLocation().Latitude)
		}
	})

	t.Run("rechaza latitud fuera de rango", func(t *testing.T) {
		p := newTestPatient(t)
		err := p.SetHomeLocation(91, 0)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza longitud fuera de rango", func(t *testing.T) {
		p := newTestPatient(t)
		err := p.SetHomeLocation(0, 181)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})
}

// ── UpdatePreferences ─────────────────────────────────────────────

func TestUpdatePreferences(t *testing.T) {
	p := newTestPatient(t)
	clinicID := sharedtypes.ClinicID(uuid.New())

	p.UpdatePreferences(aggregate.PatientPreferences{
		PreferredClinicID:    &clinicID,
		PreferredTimeOfDay:   valueobject.TimeOfDayMorning,
		CommunicationChannel: valueobject.ChannelSMS,
	})

	if p.Preferences().PreferredClinicID == nil || *p.Preferences().PreferredClinicID != clinicID {
		t.Error("PreferredClinicID no coincide")
	}

	evts := p.PendingEvents()
	if len(evts) != 1 || evts[0].EventType() != "patient.preferences.updated" {
		t.Errorf("se esperaba evento 'patient.preferences.updated', got %+v", evts)
	}
}

// ── AddCoverage ───────────────────────────────────────────────────

func TestAddCoverage(t *testing.T) {
	t.Run("agrega cobertura nueva exitosamente", func(t *testing.T) {
		p := newTestPatient(t)
		c := newCoverage(t, p.ID(), valueobject.CoverageTypePrivate)

		if err := p.AddCoverage(c, uuid.New()); err != nil {
			t.Fatalf("AddCoverage() error = %v", err)
		}
		if len(p.Coverages()) != 1 {
			t.Errorf("len(Coverages) = %d, se esperaba 1", len(p.Coverages()))
		}
		evts := p.PendingEvents()
		if len(evts) != 1 || evts[0].EventType() != "patient.coverage.updated" {
			t.Errorf("se esperaba evento 'patient.coverage.updated', got %+v", evts)
		}
	})

	t.Run("rechaza segunda cobertura activa del mismo tipo", func(t *testing.T) {
		p := newTestPatient(t)
		c1 := newCoverage(t, p.ID(), valueobject.CoverageTypePrivate)
		_ = p.AddCoverage(c1, uuid.New())

		c2 := newCoverage(t, p.ID(), valueobject.CoverageTypePrivate)
		err := p.AddCoverage(c2, uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})

	t.Run("permite coberturas activas de tipos distintos", func(t *testing.T) {
		p := newTestPatient(t)
		c1 := newCoverage(t, p.ID(), valueobject.CoverageTypePrivate)
		c2 := newCoverage(t, p.ID(), valueobject.CoverageTypeObraSocial)

		if err := p.AddCoverage(c1, uuid.New()); err != nil {
			t.Fatalf("AddCoverage(c1) error = %v", err)
		}
		if err := p.AddCoverage(c2, uuid.New()); err != nil {
			t.Fatalf("AddCoverage(c2) error = %v", err)
		}
		if len(p.Coverages()) != 2 {
			t.Errorf("len(Coverages) = %d, se esperaba 2", len(p.Coverages()))
		}
	})
}

// ── SuspendCoverage ───────────────────────────────────────────────

func TestSuspendCoverage(t *testing.T) {
	t.Run("suspende una cobertura activa", func(t *testing.T) {
		p := newTestPatient(t)
		c := newCoverage(t, p.ID(), valueobject.CoverageTypePrivate)
		_ = p.AddCoverage(c, uuid.New())
		p.PendingEvents()

		if err := p.SuspendCoverage(c.ID(), "fraude", uuid.New()); err != nil {
			t.Fatalf("SuspendCoverage() error = %v", err)
		}
		if c.Status() != coverage.CoverageStatusSuspended {
			t.Errorf("Status = %v, se esperaba Suspended", c.Status())
		}
		evts := p.PendingEvents()
		if len(evts) != 1 || evts[0].EventType() != "patient.coverage.updated" {
			t.Errorf("se esperaba evento de coverage, got %+v", evts)
		}
	})

	t.Run("falla si el coverageID no existe", func(t *testing.T) {
		p := newTestPatient(t)
		err := p.SuspendCoverage(uuid.New(), "test", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrNotFound)
		}
	})

	t.Run("propaga error si la cobertura ya está suspendida", func(t *testing.T) {
		p := newTestPatient(t)
		c := newCoverage(t, p.ID(), valueobject.CoverageTypePrivate)
		_ = p.AddCoverage(c, uuid.New())
		_ = p.SuspendCoverage(c.ID(), "primera", uuid.New())

		err := p.SuspendCoverage(c.ID(), "segunda", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error al suspender dos veces")
		}
	})
}

// ── ActiveCoverage ────────────────────────────────────────────────

func TestActiveCoverage(t *testing.T) {
	t.Run("retorna nil si no hay coberturas", func(t *testing.T) {
		p := newTestPatient(t)
		if p.ActiveCoverage() != nil {
			t.Error("se esperaba nil")
		}
	})

	t.Run("retorna la cobertura de mayor prioridad entre las activas", func(t *testing.T) {
		p := newTestPatient(t)
		private := newCoverage(t, p.ID(), valueobject.CoverageTypePrivate)
		obraSocial := newCoverage(t, p.ID(), valueobject.CoverageTypeObraSocial)
		corporate := newCoverage(t, p.ID(), valueobject.CoverageTypeCorporate)

		_ = p.AddCoverage(private, uuid.New())
		_ = p.AddCoverage(corporate, uuid.New())
		_ = p.AddCoverage(obraSocial, uuid.New())

		active := p.ActiveCoverage()
		if active == nil {
			t.Fatal("se esperaba una cobertura activa")
		}
		if active.CoverageType() != valueobject.CoverageTypeObraSocial {
			t.Errorf("CoverageType = %v, se esperaba ObraSocial (mayor prioridad)", active.CoverageType())
		}
	})

	t.Run("ignora coberturas suspendidas al calcular prioridad", func(t *testing.T) {
		p := newTestPatient(t)
		private := newCoverage(t, p.ID(), valueobject.CoverageTypePrivate)
		obraSocial := newCoverage(t, p.ID(), valueobject.CoverageTypeObraSocial)

		_ = p.AddCoverage(private, uuid.New())
		_ = p.AddCoverage(obraSocial, uuid.New())
		_ = p.SuspendCoverage(obraSocial.ID(), "test", uuid.New())

		active := p.ActiveCoverage()
		if active == nil {
			t.Fatal("se esperaba la cobertura Private activa")
		}
		if active.CoverageType() != valueobject.CoverageTypePrivate {
			t.Errorf("CoverageType = %v, se esperaba Private (la única activa)", active.CoverageType())
		}
	})
}

// ── AddMedicalAlertByStaff ────────────────────────────────────────

func TestAddMedicalAlertByStaff(t *testing.T) {
	t.Run("agrega alerta crítica creada por staff", func(t *testing.T) {
		p := newTestPatient(t)
		staffID := uuid.New()

		err := p.AddMedicalAlertByStaff(valueobject.AlertTypeAllergy, valueobject.AlertSeverityCritical, "Alergia grave", staffID)
		if err != nil {
			t.Fatalf("AddMedicalAlertByStaff() error = %v", err)
		}
		alerts := p.MedicalAlerts()
		if len(alerts) != 1 {
			t.Fatalf("len(MedicalAlerts) = %d, se esperaba 1", len(alerts))
		}
		a := alerts[0]
		if a.Severity() != valueobject.AlertSeverityCritical {
			t.Errorf("Severity = %v, se esperaba Critical", a.Severity())
		}
		if a.AlertType() != valueobject.AlertTypeAllergy {
			t.Errorf("AlertType = %v, se esperaba Allergy", a.AlertType())
		}
		if a.Description() != "Alergia grave" {
			t.Errorf("Description = %q", a.Description())
		}
		if a.IsSelfReported() {
			t.Error("IsSelfReported debería ser false")
		}
		if a.CreatedBy() != staffID {
			t.Error("CreatedBy no coincide")
		}
		if a.CreatedAt().IsZero() {
			t.Error("CreatedAt no debería ser cero")
		}
		if !a.IsActive() {
			t.Error("la alerta recién creada debería estar activa")
		}

		evts := p.PendingEvents()
		if len(evts) != 1 || evts[0].EventType() != "patient.medical_alert.added" {
			t.Errorf("se esperaba evento de alerta, got %+v", evts)
		}
	})

	t.Run("rechaza descripción vacía", func(t *testing.T) {
		p := newTestPatient(t)
		err := p.AddMedicalAlertByStaff(valueobject.AlertTypeAllergy, valueobject.AlertSeverityCritical, "   ", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})
}

// ── AddSelfReportedAlert ──────────────────────────────────────────

func TestAddSelfReportedAlert(t *testing.T) {
	t.Run("agrega alerta auto-reportada siempre con severidad Warning", func(t *testing.T) {
		p := newTestPatient(t)
		patientUserID := uuid.New()

		err := p.AddSelfReportedAlert(valueobject.AlertTypeMedication, "Tomo ibuprofeno", patientUserID)
		if err != nil {
			t.Fatalf("AddSelfReportedAlert() error = %v", err)
		}
		alerts := p.MedicalAlerts()
		if len(alerts) != 1 {
			t.Fatalf("len = %d, se esperaba 1", len(alerts))
		}
		a := alerts[0]
		if a.Severity() != valueobject.AlertSeverityWarning {
			t.Errorf("Severity = %v, se esperaba Warning (el paciente nunca puede crear Critical)", a.Severity())
		}
		if !a.IsSelfReported() {
			t.Error("IsSelfReported debería ser true")
		}
	})

	t.Run("rechaza descripción vacía", func(t *testing.T) {
		p := newTestPatient(t)
		err := p.AddSelfReportedAlert(valueobject.AlertTypeOther, "", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})
}

// ── RevokeAlert ───────────────────────────────────────────────────

func TestRevokeAlert(t *testing.T) {
	t.Run("revoca una alerta activa", func(t *testing.T) {
		p := newTestPatient(t)
		_ = p.AddMedicalAlertByStaff(valueobject.AlertTypeAllergy, valueobject.AlertSeverityWarning, "test", uuid.New())
		alertID := p.MedicalAlerts()[0].ID()
		revokedBy := uuid.New()

		if err := p.RevokeAlert(alertID, revokedBy); err != nil {
			t.Fatalf("RevokeAlert() error = %v", err)
		}
		a := p.MedicalAlerts()[0]
		if a.IsActive() {
			t.Error("la alerta debería estar inactiva")
		}
		if a.RevokedAt() == nil {
			t.Error("RevokedAt debería estar seteado")
		}
	})

	t.Run("falla si la alerta no existe", func(t *testing.T) {
		p := newTestPatient(t)
		err := p.RevokeAlert(uuid.New(), uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrNotFound)
		}
	})

	t.Run("falla si la alerta ya está revocada", func(t *testing.T) {
		p := newTestPatient(t)
		_ = p.AddMedicalAlertByStaff(valueobject.AlertTypeAllergy, valueobject.AlertSeverityWarning, "test", uuid.New())
		alertID := p.MedicalAlerts()[0].ID()
		_ = p.RevokeAlert(alertID, uuid.New())

		err := p.RevokeAlert(alertID, uuid.New())
		if err == nil {
			t.Fatal("se esperaba error al revocar dos veces")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})
}

// ── ActiveAlerts ──────────────────────────────────────────────────

func TestActiveAlerts(t *testing.T) {
	p := newTestPatient(t)
	_ = p.AddMedicalAlertByStaff(valueobject.AlertTypeOther, valueobject.AlertSeverityInfo, "info alert", uuid.New())
	_ = p.AddMedicalAlertByStaff(valueobject.AlertTypeAllergy, valueobject.AlertSeverityCritical, "critical alert", uuid.New())
	_ = p.AddSelfReportedAlert(valueobject.AlertTypeMedication, "warning alert (self)", uuid.New())

	t.Run("sin filtro retorna todas las alertas activas", func(t *testing.T) {
		alerts := p.ActiveAlerts(nil)
		if len(alerts) != 3 {
			t.Errorf("len = %d, se esperaba 3", len(alerts))
		}
	})

	t.Run("filtro Warning retorna Warning y Critical", func(t *testing.T) {
		sev := valueobject.AlertSeverityWarning
		alerts := p.ActiveAlerts(&sev)
		if len(alerts) != 2 {
			t.Errorf("len = %d, se esperaba 2", len(alerts))
		}
	})

	t.Run("filtro Critical retorna solo Critical", func(t *testing.T) {
		sev := valueobject.AlertSeverityCritical
		alerts := p.ActiveAlerts(&sev)
		if len(alerts) != 1 {
			t.Fatalf("len = %d, se esperaba 1", len(alerts))
		}
		if alerts[0].Severity() != valueobject.AlertSeverityCritical {
			t.Errorf("Severity = %v", alerts[0].Severity())
		}
	})

	t.Run("alertas revocadas se excluyen", func(t *testing.T) {
		alertID := p.MedicalAlerts()[1].ID() // la critical
		_ = p.RevokeAlert(alertID, uuid.New())

		alerts := p.ActiveAlerts(nil)
		if len(alerts) != 2 {
			t.Errorf("len = %d, se esperaba 2 tras revocar una", len(alerts))
		}
	})
}

// ── RecordCompletedVisit / UpdateRiskLevel ────────────────────────

func TestRecordCompletedVisit(t *testing.T) {
	t.Run("registra una visita y actualiza el resumen", func(t *testing.T) {
		p := newTestPatient(t)
		treatment := aggregate.TreatmentSummary{
			ProcedureCode:  "D1110",
			Description:    "Limpieza",
			PerformedAt:    time.Now(),
			ClinicID:       uuid.New(),
			ProfessionalID: uuid.New(),
		}

		p.RecordCompletedVisit(treatment, "event-1")

		if p.DentalHistory().VisitCount() != 1 {
			t.Errorf("VisitCount = %d, se esperaba 1", p.DentalHistory().VisitCount())
		}
		if p.DentalHistory().LastVisitDate() == nil {
			t.Error("LastVisitDate debería estar seteado")
		}
		if len(p.DentalHistory().MainTreatments()) != 1 {
			t.Errorf("len(MainTreatments) = %d, se esperaba 1", len(p.DentalHistory().MainTreatments()))
		}
		if p.DentalHistory().UpdatedAt().IsZero() {
			t.Error("UpdatedAt no debería ser cero")
		}
	})

	t.Run("mantiene solo los últimos 20 tratamientos", func(t *testing.T) {
		p := newTestPatient(t)
		for i := 0; i < 25; i++ {
			p.RecordCompletedVisit(aggregate.TreatmentSummary{
				ProcedureCode: "D" + uuid.New().String()[:4],
				PerformedAt:   time.Now(),
			}, "event")
		}
		if got := len(p.DentalHistory().MainTreatments()); got != 20 {
			t.Errorf("len(MainTreatments) = %d, se esperaba 20 (tope)", got)
		}
		if p.DentalHistory().VisitCount() != 25 {
			t.Errorf("VisitCount = %d, se esperaba 25 (no topea)", p.DentalHistory().VisitCount())
		}
	})
}

func TestUpdateRiskLevel(t *testing.T) {
	p := newTestPatient(t)
	if p.DentalHistory().RiskLevel() != valueobject.RiskLevelLow {
		t.Fatalf("RiskLevel inicial = %v, se esperaba Low", p.DentalHistory().RiskLevel())
	}

	p.UpdateRiskLevel(valueobject.RiskLevelHigh, uuid.New())

	if p.DentalHistory().RiskLevel() != valueobject.RiskLevelHigh {
		t.Errorf("RiskLevel = %v, se esperaba High", p.DentalHistory().RiskLevel())
	}
}

// ── IsMinor ───────────────────────────────────────────────────────

func TestIsMinor(t *testing.T) {
	t.Run("false para paciente adulto", func(t *testing.T) {
		p := newTestPatient(t)
		if p.IsMinor() {
			t.Error("se esperaba false")
		}
	})

	t.Run("true para paciente menor de edad", func(t *testing.T) {
		p, err := aggregate.NewPatient(nil, validFullName(t), minorBirthDate(t), validGender(t), validNationalID(t), validPhone(t), nil)
		if err != nil {
			t.Fatalf("NewPatient() error = %v", err)
		}
		if !p.IsMinor() {
			t.Error("se esperaba true para paciente de 10 años")
		}
	})
}

// ── ReconstituteMedicalAlert / ReconstituteDentalHistory ──────────

func TestReconstituteMedicalAlert(t *testing.T) {
	id := uuid.New()
	createdBy := uuid.New()
	revokedBy := uuid.New()
	createdAt := time.Now().Add(-time.Hour)
	revokedAt := time.Now()

	a := aggregate.ReconstituteMedicalAlert(
		id, valueobject.AlertTypeAllergy, valueobject.AlertSeverityCritical,
		"Penicilina", false, false, createdBy, createdAt, &revokedAt, &revokedBy,
	)

	if a.ID() != id {
		t.Errorf("ID = %v", a.ID())
	}
	if a.IsActive() {
		t.Error("IsActive debería ser false")
	}
	if a.RevokedAt() == nil || !a.RevokedAt().Equal(revokedAt) {
		t.Error("RevokedAt no coincide")
	}
	if a.CreatedBy() != createdBy {
		t.Error("CreatedBy no coincide")
	}
}

func TestReconstituteDentalHistory(t *testing.T) {
	id := uuid.New()
	patientID := sharedtypes.PatientID(uuid.New())
	lastVisit := time.Now()
	treatments := []aggregate.TreatmentSummary{{ProcedureCode: "D1110"}}

	h := aggregate.ReconstituteDentalHistory(
		id, patientID, &lastVisit, valueobject.RiskLevelMedium, 5, treatments,
		time.Now(), "appointment.completed",
	)

	if h.ID() != id {
		t.Errorf("ID = %v", h.ID())
	}
	if h.RiskLevel() != valueobject.RiskLevelMedium {
		t.Errorf("RiskLevel = %v", h.RiskLevel())
	}
	if h.VisitCount() != 5 {
		t.Errorf("VisitCount = %d, se esperaba 5", h.VisitCount())
	}
	if len(h.MainTreatments()) != 1 {
		t.Errorf("len(MainTreatments) = %d, se esperaba 1", len(h.MainTreatments()))
	}
	if h.UpdatedByEvent() != "appointment.completed" {
		t.Errorf("UpdatedByEvent = %q", h.UpdatedByEvent())
	}
}

// ── Getters (smoke test) ───────────────────────────────────────────

func TestPatientGetters(t *testing.T) {
	p := newTestPatient(t)

	if p.FullName().String() != "Juan Perez" {
		t.Errorf("FullName = %q", p.FullName().String())
	}
	if p.Gender() != valueobject.Gender("M") {
		t.Errorf("Gender = %v", p.Gender())
	}
	if p.NationalID().Number != "12345678" {
		t.Errorf("NationalID.Number = %q", p.NationalID().Number)
	}
	if p.BirthDate().String() != "1990-01-01" {
		t.Errorf("BirthDate = %q, se esperaba '1990-01-01'", p.BirthDate().String())
	}
	if p.CreatedAt().IsZero() {
		t.Error("CreatedAt no debería ser cero")
	}
	if p.UpdatedAt().IsZero() {
		t.Error("UpdatedAt no debería ser cero")
	}
	if p.HomeLocation() != nil {
		t.Error("HomeLocation debería ser nil por defecto")
	}
}
