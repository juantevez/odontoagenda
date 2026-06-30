// White-box: package nats para acceder a handleAppointmentCompleted
// y appointmentCompletedPayload, que son unexported.
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/application/command"
	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/repository"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── mocks ─────────────────────────────────────────────────────────

type mockPatientRepo struct {
	patients    map[sharedtypes.PatientID]*aggregate.Patient
	findByIDErr error
	updateErr   error
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
	if m.updateErr != nil {
		return m.updateErr
	}
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
func (m *mockPatientRepo) Search(_ context.Context, _ string, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	return sharedtypes.PagedResult[*aggregate.Patient]{}, nil
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

type mockBus struct {
	subscribeErr error
}

var _ pkgevents.Bus = (*mockBus)(nil)

func (m *mockBus) Publish(_ context.Context, _ pkgevents.DomainEvent) error { return nil }
func (m *mockBus) Subscribe(_ context.Context, _ pkgevents.SubscribeOptions, _ pkgevents.Handler) error {
	return m.subscribeErr
}
func (m *mockBus) Close() error { return nil }

// ── helpers ──────────────────────────────────────────────────────

// buildEnvelope construye un Envelope con el payload dado serializado como JSON.
func buildEnvelope(t *testing.T, payload appointmentCompletedPayload) pkgevents.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("setup: marshal payload: %v", err)
	}
	return pkgevents.Envelope{
		EventID:   uuid.New().String(),
		EventType: "appointment.completed",
		Payload:   json.RawMessage(raw),
	}
}

// addPatient crea un Patient en el mock repo y devuelve su ID.
func addPatient(t *testing.T, repo *mockPatientRepo) sharedtypes.PatientID {
	t.Helper()
	name, _ := sharedvo.NewFullName("Juan Perez")
	bd, _ := valueobject.NewBirthDate(1990, 1, 1)
	g, _ := valueobject.ParseGender("M")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "12345678")
	phone, _ := sharedvo.NewPhoneNumber("+5491112345678")
	p, err := aggregate.NewPatient(nil, name, bd, g, docID, phone, nil)
	if err != nil {
		t.Fatalf("setup: NewPatient: %v", err)
	}
	p.PendingEvents()
	repo.patients[p.ID()] = p
	return p.ID()
}

func newSubscriber(repo *mockPatientRepo) (*PatientEventSubscriber, *mockBus) {
	bus := &mockBus{}
	handler := command.NewRecordCompletedVisitHandler(repo)
	return NewPatientEventSubscriber(bus, handler), bus
}

// ── RegisterAll ───────────────────────────────────────────────────

func TestRegisterAll(t *testing.T) {
	t.Run("registra el consumer y retorna nil", func(t *testing.T) {
		sub, _ := newSubscriber(newMockRepo())
		if err := sub.RegisterAll(context.Background()); err != nil {
			t.Errorf("RegisterAll() error = %v", err)
		}
	})

	t.Run("propaga el error si Subscribe falla", func(t *testing.T) {
		repo := newMockRepo()
		bus := &mockBus{subscribeErr: errors.New("nats unavailable")}
		handler := command.NewRecordCompletedVisitHandler(repo)
		sub := NewPatientEventSubscriber(bus, handler)

		if err := sub.RegisterAll(context.Background()); !errors.Is(err, bus.subscribeErr) {
			t.Errorf("err = %v, se esperaba que envolviera %v", err, bus.subscribeErr)
		}
	})
}

// ── handleAppointmentCompleted ────────────────────────────────────

func TestHandleAppointmentCompleted(t *testing.T) {
	t.Run("procesa el evento exitosamente y registra la visita", func(t *testing.T) {
		repo := newMockRepo()
		patientID := addPatient(t, repo)
		sub, _ := newSubscriber(repo)

		env := buildEnvelope(t, appointmentCompletedPayload{
			AppointmentID:  uuid.New().String(),
			PatientID:      patientID.String(),
			ProfessionalID: uuid.New().String(),
			ClinicID:       uuid.New().String(),
			ProcedureCode:  "D1110",
			Description:    "Limpieza dental",
			CompletedAt:    time.Now().UTC().Format(time.RFC3339),
		})

		if err := sub.handleAppointmentCompleted(context.Background(), env); err != nil {
			t.Fatalf("handleAppointmentCompleted() error = %v", err)
		}
		// Verificar que la visita quedó registrada en el historial.
		p := repo.patients[patientID]
		if p.DentalHistory().VisitCount() != 1 {
			t.Errorf("VisitCount = %d, se esperaba 1", p.DentalHistory().VisitCount())
		}
	})

	t.Run("retorna ErrSkipRetry con payload JSON inválido", func(t *testing.T) {
		sub, _ := newSubscriber(newMockRepo())
		env := pkgevents.Envelope{
			EventID: uuid.New().String(),
			Payload: json.RawMessage(`{invalid json`),
		}

		err := sub.handleAppointmentCompleted(context.Background(), env)
		if !errors.Is(err, pkgevents.ErrSkipRetry) {
			t.Errorf("err = %v, se esperaba ErrSkipRetry", err)
		}
	})

	t.Run("retorna ErrSkipRetry con patient_id inválido", func(t *testing.T) {
		sub, _ := newSubscriber(newMockRepo())
		env := buildEnvelope(t, appointmentCompletedPayload{
			PatientID:   "no-es-uuid",
			CompletedAt: time.Now().UTC().Format(time.RFC3339),
		})

		err := sub.handleAppointmentCompleted(context.Background(), env)
		if !errors.Is(err, pkgevents.ErrSkipRetry) {
			t.Errorf("err = %v, se esperaba ErrSkipRetry", err)
		}
	})

	t.Run("professional_id y clinic_id inválidos se ignoran (uuid.Nil)", func(t *testing.T) {
		repo := newMockRepo()
		patientID := addPatient(t, repo)
		sub, _ := newSubscriber(repo)

		env := buildEnvelope(t, appointmentCompletedPayload{
			PatientID:      patientID.String(),
			ProfessionalID: "no-uuid",
			ClinicID:       "tampoco-uuid",
			ProcedureCode:  "D1110",
			CompletedAt:    time.Now().UTC().Format(time.RFC3339),
		})

		if err := sub.handleAppointmentCompleted(context.Background(), env); err != nil {
			t.Errorf("error = %v, se esperaba nil (UUIDs inválidos se ignoran con zero value)", err)
		}
	})

	t.Run("completed_at inválido cae en time.Now() y sigue procesando", func(t *testing.T) {
		repo := newMockRepo()
		patientID := addPatient(t, repo)
		sub, _ := newSubscriber(repo)

		env := buildEnvelope(t, appointmentCompletedPayload{
			PatientID:     patientID.String(),
			ProcedureCode: "D1110",
			CompletedAt:   "formato-invalido",
		})

		if err := sub.handleAppointmentCompleted(context.Background(), env); err != nil {
			t.Errorf("error = %v, se esperaba nil (fecha inválida usa time.Now())", err)
		}
	})

	t.Run("retorna error para retry si RecordCompletedVisit falla", func(t *testing.T) {
		sub, _ := newSubscriber(newMockRepo()) // paciente no existe en repo

		env := buildEnvelope(t, appointmentCompletedPayload{
			PatientID:     uuid.New().String(), // no existe en el repo
			ProcedureCode: "D1110",
			CompletedAt:   time.Now().UTC().Format(time.RFC3339),
		})

		err := sub.handleAppointmentCompleted(context.Background(), env)
		if err == nil {
			t.Fatal("se esperaba error para triggear el retry")
		}
		if errors.Is(err, pkgevents.ErrSkipRetry) {
			t.Error("el error debería permitir retry, no ser ErrSkipRetry")
		}
	})
}
