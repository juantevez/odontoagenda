package query_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/application/query"
	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	"github.com/juantevez/odontoagenda/context/patient/domain/repository"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── mock ─────────────────────────────────────────────────────────

type mockPatientRepo struct {
	patients     map[sharedtypes.PatientID]*aggregate.Patient
	searchResult []*aggregate.Patient
	findByIDErr  error
	searchErr    error
}

var _ repository.PatientRepository = (*mockPatientRepo)(nil)

func newMockRepo() *mockPatientRepo {
	return &mockPatientRepo{patients: make(map[sharedtypes.PatientID]*aggregate.Patient)}
}

func (m *mockPatientRepo) Save(_ context.Context, p *aggregate.Patient) error {
	m.patients[p.ID()] = p
	return nil
}
func (m *mockPatientRepo) Update(_ context.Context, p *aggregate.Patient) error {
	m.patients[p.ID()] = p
	return nil
}
func (m *mockPatientRepo) FindByID(_ context.Context, id sharedtypes.PatientID) (*aggregate.Patient, error) {
	if m.findByIDErr != nil {
		return nil, m.findByIDErr
	}
	p, ok := m.patients[id]
	if !ok {
		return nil, sharederrors.NewNotFound("Patient", id.String())
	}
	return p, nil
}
func (m *mockPatientRepo) FindByNationalID(_ context.Context, _ sharedvo.NationalID) (*aggregate.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", "")
}
func (m *mockPatientRepo) FindByUserID(_ context.Context, _ sharedtypes.UserID) (*aggregate.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", "")
}
func (m *mockPatientRepo) Search(_ context.Context, _ string, page sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	if m.searchErr != nil {
		return sharedtypes.PagedResult[*aggregate.Patient]{}, m.searchErr
	}
	return sharedtypes.NewPagedResult(m.searchResult, int64(len(m.searchResult)), page), nil
}
func (m *mockPatientRepo) FindNearClinic(_ context.Context, _ sharedtypes.ClinicID, _ float64, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	return sharedtypes.PagedResult[*aggregate.Patient]{}, nil
}
func (m *mockPatientRepo) ExistsByNationalID(_ context.Context, _ sharedvo.NationalID) (bool, error) {
	return false, nil
}
func (m *mockPatientRepo) FindPotentialDuplicates(_ context.Context, _ string, _ string) ([]*aggregate.Patient, error) {
	return nil, nil
}
func (m *mockPatientRepo) Archive(_ context.Context, _ sharedtypes.PatientID, _ string, _ sharedtypes.UserID) error {
	return nil
}

// ── helpers ──────────────────────────────────────────────────────

// minimalPatient crea un paciente adulto sin ningún dato opcional.
func minimalPatient(t *testing.T) *aggregate.Patient {
	t.Helper()
	name, _ := sharedvo.NewFullName("Ana Lopez")
	bd, _ := valueobject.NewBirthDate(1985, 6, 15)
	gender, _ := valueobject.ParseGender("F")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "87654321")
	phone, _ := sharedvo.NewPhoneNumber("+5491112345678")

	p, err := aggregate.NewPatient(nil, name, bd, gender, docID, phone, nil)
	if err != nil {
		t.Fatalf("setup: NewPatient: %v", err)
	}
	p.PendingEvents()
	return p
}

// richPatient crea un paciente adulto con todos los datos opcionales poblados:
//   - Cobertura activa con ValidUntil
//   - Alerta médica crítica
//   - Una visita registrada
//   - Clínica preferida en preferencias
//   - Email, WhatsApp y teléfono de emergencia
func richPatient(t *testing.T) *aggregate.Patient {
	t.Helper()
	name, _ := sharedvo.NewFullName("Carlos Gomez")
	bd, _ := valueobject.NewBirthDate(1990, 3, 20)
	gender, _ := valueobject.ParseGender("M")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "12345678")
	phone, _ := sharedvo.NewPhoneNumber("+5491112345678")

	p, err := aggregate.NewPatient(nil, name, bd, gender, docID, phone, nil)
	if err != nil {
		t.Fatalf("setup: NewPatient: %v", err)
	}
	p.PendingEvents()

	// Cobertura con ValidUntil y CoPayPercent.
	validUntil := time.Now().Add(365 * 24 * time.Hour)
	cov, _ := coverage.NewPatientCoverage(
		p.ID(), valueobject.CoverageTypeExtPrepaid, nil,
		"OSDE", "310", "123456789",
		time.Now(), &validUntil, uuid.New(),
	)
	pct := 20
	_ = cov.SetCoPayPercent(pct)
	_ = p.AddCoverage(cov, uuid.New())
	p.PendingEvents()

	// Alerta crítica de staff.
	_ = p.AddMedicalAlertByStaff(
		valueobject.AlertTypeAllergy, valueobject.AlertSeverityCritical,
		"Alergia grave a penicilina", uuid.New(),
	)
	p.PendingEvents()

	// Visita registrada → LastVisitDate y MainTreatments no vacíos.
	p.RecordCompletedVisit(aggregate.TreatmentSummary{
		ProcedureCode:  "D1110",
		Description:    "Limpieza dental",
		PerformedAt:    time.Now().Add(-30 * 24 * time.Hour),
		ClinicID:       uuid.New(),
		ProfessionalID: uuid.New(),
	}, "event-abc")

	// Preferencias con clínica favorita.
	clinicID := uuid.New()
	p.UpdatePreferences(aggregate.PatientPreferences{
		PreferredClinicID: &clinicID,
	})

	// Información de contacto completa.
	email, _ := sharedvo.NewEmail("carlos@example.com")
	wa, _ := sharedvo.NewPhoneNumber("+5491198765432")
	ep, _ := sharedvo.NewPhoneNumber("+5491100000000")
	p.UpdateContactInfo(aggregate.ContactInfo{
		Phone:          phone,
		Email:          &email,
		WhatsApp:       &wa,
		EmergencyName:  "María Gomez",
		EmergencyPhone: &ep,
	})

	return p
}

// minorPatient crea un paciente menor de edad (10 años).
func minorPatient(t *testing.T) *aggregate.Patient {
	t.Helper()
	name, _ := sharedvo.NewFullName("Lucas Perez")
	bd, _ := valueobject.NewBirthDateFromTime(time.Now().AddDate(-10, 0, 0))
	gender, _ := valueobject.ParseGender("M")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "11223344")
	phone, _ := sharedvo.NewPhoneNumber("+5491112345678")

	p, err := aggregate.NewPatient(nil, name, bd, gender, docID, phone, nil)
	if err != nil {
		t.Fatalf("setup: NewPatient: %v", err)
	}
	p.PendingEvents()
	return p
}

func save(t *testing.T, repo *mockPatientRepo, p *aggregate.Patient) {
	t.Helper()
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatalf("setup: Save: %v", err)
	}
}

// ── GetPatientByIDHandler ─────────────────────────────────────────

func TestGetPatientByIDHandler(t *testing.T) {
	t.Run("retorna DTO completo para paciente mínimo sin datos opcionales", func(t *testing.T) {
		repo := newMockRepo()
		p := minimalPatient(t)
		save(t, repo, p)

		h := query.NewGetPatientByIDHandler(repo)
		dto, err := h.Handle(context.Background(), query.GetPatientByIDQuery{PatientID: p.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}

		if dto.ID != p.ID().String() {
			t.Errorf("ID = %q, se esperaba %q", dto.ID, p.ID().String())
		}
		if dto.FullName != "Ana Lopez" {
			t.Errorf("FullName = %q", dto.FullName)
		}
		if dto.IsMinor {
			t.Error("paciente de 1985 no debería ser menor")
		}
		if dto.ActiveCoverage != nil {
			t.Error("ActiveCoverage debería ser nil para paciente sin cobertura")
		}
		if len(dto.ActiveAlerts) != 0 {
			t.Errorf("ActiveAlerts count = %d, se esperaba 0", len(dto.ActiveAlerts))
		}
		// ContactInfo sin campos opcionales.
		if dto.ContactInfo.Email != nil {
			t.Error("Email debería ser nil")
		}
		if dto.ContactInfo.WhatsApp != nil {
			t.Error("WhatsApp debería ser nil")
		}
		if dto.ContactInfo.EmergencyPhone != nil {
			t.Error("EmergencyPhone debería ser nil")
		}
		// DentalHistory vacío.
		if dto.DentalHistory.LastVisitDate != nil {
			t.Error("LastVisitDate debería ser nil para paciente sin visitas")
		}
		if len(dto.DentalHistory.MainTreatments) != 0 {
			t.Errorf("MainTreatments count = %d, se esperaba 0", len(dto.DentalHistory.MainTreatments))
		}
		// Preferences sin clínica preferida.
		if dto.Preferences.PreferredClinicID != nil {
			t.Error("PreferredClinicID debería ser nil")
		}
	})

	t.Run("retorna DTO completo para paciente con todos los campos opcionales", func(t *testing.T) {
		repo := newMockRepo()
		p := richPatient(t)
		save(t, repo, p)

		h := query.NewGetPatientByIDHandler(repo)
		dto, err := h.Handle(context.Background(), query.GetPatientByIDQuery{PatientID: p.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}

		// Cobertura activa con ValidUntil.
		if dto.ActiveCoverage == nil {
			t.Fatal("se esperaba ActiveCoverage != nil")
		}
		if dto.ActiveCoverage.ValidUntil == nil {
			t.Error("ValidUntil debería estar seteado")
		}
		if dto.ActiveCoverage.CoPayPercent == nil || *dto.ActiveCoverage.CoPayPercent != 20 {
			t.Errorf("CoPayPercent = %v, se esperaba 20", dto.ActiveCoverage.CoPayPercent)
		}

		// Alertas activas.
		if len(dto.ActiveAlerts) == 0 {
			t.Error("se esperaba al menos una alerta activa")
		}
		if dto.ActiveAlerts[0].Severity != "Critical" {
			t.Errorf("Severity = %q, se esperaba 'Critical'", dto.ActiveAlerts[0].Severity)
		}

		// ContactInfo completo.
		if dto.ContactInfo.Email == nil {
			t.Error("Email debería estar seteado")
		}
		if dto.ContactInfo.WhatsApp == nil {
			t.Error("WhatsApp debería estar seteado")
		}
		if dto.ContactInfo.EmergencyPhone == nil {
			t.Error("EmergencyPhone debería estar seteado")
		}

		// DentalHistory con visita.
		if dto.DentalHistory.LastVisitDate == nil {
			t.Error("LastVisitDate debería estar seteado tras RecordCompletedVisit")
		}
		if len(dto.DentalHistory.MainTreatments) == 0 {
			t.Error("se esperaba al menos un tratamiento registrado")
		}
		if dto.DentalHistory.MainTreatments[0].ProcedureCode != "D1110" {
			t.Errorf("ProcedureCode = %q, se esperaba 'D1110'", dto.DentalHistory.MainTreatments[0].ProcedureCode)
		}

		// Preferences con clínica.
		if dto.Preferences.PreferredClinicID == nil {
			t.Error("PreferredClinicID debería estar seteado")
		}
	})

	t.Run("propaga el error si el paciente no existe", func(t *testing.T) {
		repo := newMockRepo()
		repo.findByIDErr = sharederrors.NewNotFound("Patient", uuid.New().String())
		h := query.NewGetPatientByIDHandler(repo)

		_, err := h.Handle(context.Background(), query.GetPatientByIDQuery{PatientID: uuid.New()})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
}

// ── SearchPatientsHandler ─────────────────────────────────────────

func TestSearchPatientsHandler(t *testing.T) {
	t.Run("retorna resultados paginados con DTOs correctos", func(t *testing.T) {
		repo := newMockRepo()
		// rich: tiene cobertura y alertas → CoverageType y HasAlerts set
		repo.searchResult = []*aggregate.Patient{richPatient(t)}

		h := query.NewSearchPatientsHandler(repo)
		page := sharedtypes.NewPage(10, 0)
		result, err := h.Handle(context.Background(), query.SearchPatientsQuery{
			Query: "Carlos",
			Page:  page,
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.Total != 1 {
			t.Errorf("Total = %d, se esperaba 1", result.Total)
		}
		if len(result.Items) != 1 {
			t.Fatalf("len(Items) = %d, se esperaba 1", len(result.Items))
		}
		dto := result.Items[0]
		if dto.FullName != "Carlos Gomez" {
			t.Errorf("FullName = %q", dto.FullName)
		}
		if dto.CoverageType == nil {
			t.Error("CoverageType debería estar seteado para paciente con cobertura")
		}
		if !dto.HasAlerts {
			t.Error("HasAlerts debería ser true para paciente con alertas activas")
		}
	})

	t.Run("retorna DTO de paciente mínimo sin cobertura ni alertas", func(t *testing.T) {
		repo := newMockRepo()
		repo.searchResult = []*aggregate.Patient{minimalPatient(t)}

		h := query.NewSearchPatientsHandler(repo)
		result, err := h.Handle(context.Background(), query.SearchPatientsQuery{
			Page: sharedtypes.NewPage(10, 0),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		dto := result.Items[0]
		if dto.CoverageType != nil {
			t.Error("CoverageType debería ser nil para paciente sin cobertura")
		}
		if dto.HasAlerts {
			t.Error("HasAlerts debería ser false para paciente sin alertas")
		}
	})

	t.Run("retorna resultado vacío sin error cuando no hay pacientes", func(t *testing.T) {
		repo := newMockRepo()
		h := query.NewSearchPatientsHandler(repo)

		result, err := h.Handle(context.Background(), query.SearchPatientsQuery{
			Query: "Inexistente",
			Page:  sharedtypes.NewPage(10, 0),
		})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.Total != 0 || len(result.Items) != 0 {
			t.Errorf("se esperaba resultado vacío, got Total=%d Items=%d", result.Total, len(result.Items))
		}
	})

	t.Run("propaga el error del repositorio", func(t *testing.T) {
		repo := newMockRepo()
		repo.searchErr = errors.New("db timeout")
		h := query.NewSearchPatientsHandler(repo)

		_, err := h.Handle(context.Background(), query.SearchPatientsQuery{
			Page: sharedtypes.NewPage(10, 0),
		})
		if !errors.Is(err, repo.searchErr) {
			t.Errorf("err = %v, se esperaba %v", err, repo.searchErr)
		}
	})
}

// ── GetPatientForBookingHandler ───────────────────────────────────

func TestGetPatientForBookingHandler(t *testing.T) {
	t.Run("DTO de booking con alerta crítica, cobertura y clínica preferida", func(t *testing.T) {
		repo := newMockRepo()
		p := richPatient(t)
		save(t, repo, p)

		h := query.NewGetPatientForBookingHandler(repo)
		dto, err := h.Handle(context.Background(), query.GetPatientForBookingQuery{PatientID: p.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}

		if dto.ID != p.ID().String() {
			t.Errorf("ID = %q", dto.ID)
		}
		if dto.IsMinor {
			t.Error("paciente adulto no debería ser menor")
		}
		if len(dto.CriticalAlerts) == 0 {
			t.Error("se esperaba al menos una alerta crítica")
		}
		if dto.ActiveCoverage == nil {
			t.Error("se esperaba ActiveCoverage != nil")
		}
		if dto.ActiveCoverage.ValidUntil == nil {
			t.Error("ValidUntil debería estar seteado en la cobertura")
		}
		if dto.PreferredClinic == nil {
			t.Error("PreferredClinic debería estar seteado")
		}
	})

	t.Run("DTO de booking mínimo: sin alertas críticas, sin cobertura, sin clínica preferida", func(t *testing.T) {
		repo := newMockRepo()
		p := minimalPatient(t)
		save(t, repo, p)

		h := query.NewGetPatientForBookingHandler(repo)
		dto, err := h.Handle(context.Background(), query.GetPatientForBookingQuery{PatientID: p.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}

		if len(dto.CriticalAlerts) != 0 {
			t.Errorf("CriticalAlerts count = %d, se esperaba 0", len(dto.CriticalAlerts))
		}
		if dto.ActiveCoverage != nil {
			t.Error("ActiveCoverage debería ser nil")
		}
		if dto.PreferredClinic != nil {
			t.Error("PreferredClinic debería ser nil")
		}
	})

	t.Run("IsMinor es true para paciente menor de edad", func(t *testing.T) {
		repo := newMockRepo()
		p := minorPatient(t)
		save(t, repo, p)

		h := query.NewGetPatientForBookingHandler(repo)
		dto, err := h.Handle(context.Background(), query.GetPatientForBookingQuery{PatientID: p.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if !dto.IsMinor {
			t.Error("IsMinor = false para paciente de 10 años, se esperaba true")
		}
	})

	t.Run("cobertura sin ValidUntil → campo omitido en DTO", func(t *testing.T) {
		repo := newMockRepo()
		p := minimalPatient(t)
		// Agregar cobertura sin ValidUntil.
		cov, _ := coverage.NewPatientCoverage(
			p.ID(), valueobject.CoverageTypePrivate, nil, "", "", "",
			time.Now(), nil, uuid.New(),
		)
		_ = p.AddCoverage(cov, uuid.New())
		p.PendingEvents()
		save(t, repo, p)

		h := query.NewGetPatientForBookingHandler(repo)
		dto, err := h.Handle(context.Background(), query.GetPatientForBookingQuery{PatientID: p.ID()})
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if dto.ActiveCoverage == nil {
			t.Fatal("se esperaba ActiveCoverage")
		}
		if dto.ActiveCoverage.ValidUntil != nil {
			t.Error("ValidUntil debería ser nil cuando no se pasó")
		}
	})

	t.Run("propaga el error si el paciente no existe", func(t *testing.T) {
		repo := newMockRepo()
		repo.findByIDErr = sharederrors.NewNotFound("Patient", uuid.New().String())
		h := query.NewGetPatientForBookingHandler(repo)

		_, err := h.Handle(context.Background(), query.GetPatientForBookingQuery{PatientID: uuid.New()})
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})
}
