// Package command contiene los Command Handlers del bounded context Patient.
// Cada handler orquesta un caso de uso de escritura sin contener lógica de negocio.
package command

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	"github.com/juantevez/odontoagenda/context/patient/domain/repository"
	"github.com/juantevez/odontoagenda/context/patient/domain/service"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── RegisterPatient ───────────────────────────────────────────────

type RegisterPatientCommand struct {
	UserID         *uuid.UUID // nil si lo crea el staff sin cuenta IAM previa
	FullName       string
	BirthDate      time.Time
	Gender         string
	DocType        string
	DocNumber      string
	Phone          string
	Email          string // opcional
	EmergencyName  string // opcional
	EmergencyPhone string // opcional
	CreatedBy      *uuid.UUID
	// SkipDuplicateCheck permite al staff registrar igualmente si detecta duplicados.
	SkipDuplicateCheck bool
}

type RegisterPatientResult struct {
	PatientID           sharedtypes.PatientID
	DuplicateCandidates []service.DuplicateCandidate // no vacío si hay potenciales duplicados
}

type RegisterPatientHandler struct {
	repo     repository.PatientRepository
	detector *service.DuplicateDetector
	eventBus events.Bus
	logger   *slog.Logger
}

func NewRegisterPatientHandler(
	repo repository.PatientRepository,
	detector *service.DuplicateDetector,
	eventBus events.Bus,
) *RegisterPatientHandler {
	return &RegisterPatientHandler{
		repo:     repo,
		detector: detector,
		eventBus: eventBus,
		logger:   slog.Default().With("handler", "RegisterPatient"),
	}
}

func (h *RegisterPatientHandler) Handle(ctx context.Context, cmd RegisterPatientCommand) (RegisterPatientResult, error) {
	// 1. Construir Value Objects (validación temprana).
	fullName, err := sharedvo.NewFullName(cmd.FullName)
	if err != nil {
		return RegisterPatientResult{}, sharederrors.NewInvalidArgument("full_name", err.Error())
	}

	birthDate, err := valueobject.NewBirthDateFromTime(cmd.BirthDate)
	if err != nil {
		return RegisterPatientResult{}, sharederrors.NewInvalidArgument("birth_date", err.Error())
	}

	gender, err := valueobject.ParseGender(cmd.Gender)
	if err != nil {
		return RegisterPatientResult{}, sharederrors.NewInvalidArgument("gender", err.Error())
	}

	docType, err := sharedvo.NewNationalID(sharedvo.DocumentType(cmd.DocType), cmd.DocNumber)
	if err != nil {
		return RegisterPatientResult{}, sharederrors.NewInvalidArgument("national_id", err.Error())
	}

	phone, err := sharedvo.NewPhoneNumber(cmd.Phone)
	if err != nil {
		return RegisterPatientResult{}, sharederrors.NewInvalidArgument("phone", err.Error())
	}

	// 2. Detección de duplicados (antes de persistir).
	if !cmd.SkipDuplicateCheck {
		result, err := h.detector.Detect(ctx, cmd.FullName, cmd.Phone, docType)
		if err != nil {
			// ErrAlreadyExists = NationalID exacto → no permitimos continuar.
			if sharederrors.IsCode(err, sharederrors.ErrAlreadyExists) {
				return RegisterPatientResult{}, err
			}
			return RegisterPatientResult{}, sharederrors.NewInternal(err)
		}
		if result.HasDuplicates {
			// Hay posibles duplicados pero no certeros: devolvemos candidatos
			// para que el caller (HTTP handler) presente la advertencia al usuario.
			return RegisterPatientResult{
				DuplicateCandidates: result.Candidates,
			}, nil
		}
	}

	// 3. Crear el aggregate.
	patient, err := aggregate.NewPatient(
		cmd.UserID, fullName, birthDate, gender, docType, phone, cmd.CreatedBy,
	)
	if err != nil {
		return RegisterPatientResult{}, err
	}

	// 4. Info de contacto adicional opcional.
	contactInfo := patient.ContactInfo()
	if cmd.Email != "" {
		email, err := sharedvo.NewEmail(cmd.Email)
		if err != nil {
			return RegisterPatientResult{}, sharederrors.NewInvalidArgument("email", err.Error())
		}
		contactInfo.Email = &email
	}
	if cmd.EmergencyName != "" {
		contactInfo.EmergencyName = cmd.EmergencyName
	}
	if cmd.EmergencyPhone != "" {
		ep, err := sharedvo.NewPhoneNumber(cmd.EmergencyPhone)
		if err != nil {
			return RegisterPatientResult{}, sharederrors.NewInvalidArgument("emergency_phone", err.Error())
		}
		contactInfo.EmergencyPhone = &ep
	}
	patient.UpdateContactInfo(contactInfo)

	// 5. Persistir.
	if err := h.repo.Save(ctx, patient); err != nil {
		return RegisterPatientResult{}, fmt.Errorf("RegisterPatient: save: %w", err)
	}

	// 6. Publicar eventos.
	h.publishPendingEvents(ctx, patient)

	h.logger.InfoContext(ctx, "paciente registrado",
		"patient_id", patient.ID(),
		"full_name", cmd.FullName,
	)

	return RegisterPatientResult{PatientID: patient.ID()}, nil
}

// ── AddCoverage ───────────────────────────────────────────────────

type AddCoverageCommand struct {
	PatientID        sharedtypes.PatientID
	CoverageType     string
	AgreementID      *uuid.UUID
	ProviderName     string
	PlanCode         string
	MembershipNumber string
	ValidFrom        time.Time
	ValidUntil       *time.Time
	CoPayPercent     *int
	CoPayFixed       *int64
	AddedBy          uuid.UUID // debe ser staff (enforced por middleware)
}

type AddCoverageHandler struct {
	repo        repository.PatientRepository
	historyRepo repository.CoverageHistoryRepository
	eventBus    events.Bus
	logger      *slog.Logger
}

func NewAddCoverageHandler(
	repo repository.PatientRepository,
	historyRepo repository.CoverageHistoryRepository,
	eventBus events.Bus,
) *AddCoverageHandler {
	return &AddCoverageHandler{
		repo:        repo,
		historyRepo: historyRepo,
		eventBus:    eventBus,
		logger:      slog.Default().With("handler", "AddCoverage"),
	}
}

func (h *AddCoverageHandler) Handle(ctx context.Context, cmd AddCoverageCommand) (uuid.UUID, error) {
	// 1. Cargar paciente.
	patient, err := h.repo.FindByID(ctx, cmd.PatientID)
	if err != nil {
		return uuid.Nil, err
	}

	// 2. Construir cobertura.
	covType, err := valueobject.ParseCoverageType(cmd.CoverageType)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("coverage_type", err.Error())
	}

	cov, err := coverage.NewPatientCoverage(
		cmd.PatientID, covType,
		cmd.AgreementID, cmd.ProviderName, cmd.PlanCode, cmd.MembershipNumber,
		cmd.ValidFrom, cmd.ValidUntil, cmd.AddedBy,
	)
	if err != nil {
		return uuid.Nil, err
	}

	// 3. Aplicar copago si viene en el comando.
	if cmd.CoPayPercent != nil {
		if err := cov.SetCoPayPercent(*cmd.CoPayPercent); err != nil {
			return uuid.Nil, err
		}
	} else if cmd.CoPayFixed != nil {
		if err := cov.SetCoPayFixed(*cmd.CoPayFixed); err != nil {
			return uuid.Nil, err
		}
	}

	// 4. Registrar cobertura anterior para el historial.
	previousCov := patient.ActiveCoverage()

	// 5. Agregar cobertura al aggregate (valida invariante de unicidad por tipo).
	if err := patient.AddCoverage(cov, cmd.AddedBy); err != nil {
		return uuid.Nil, err
	}

	// 6. Persistir patient.
	if err := h.repo.Update(ctx, patient); err != nil {
		return uuid.Nil, fmt.Errorf("AddCoverage: update: %w", err)
	}

	// 7. Registrar en el historial (append-only).
	historyEntry := buildHistoryEntry(patient.ID(), previousCov, cov, cmd.AddedBy)
	if err := h.historyRepo.Append(ctx, historyEntry); err != nil {
		h.logger.WarnContext(ctx, "error guardando historial de cobertura", "error", err)
	}

	// 8. Publicar eventos.
	h.publishPendingEvents(ctx, patient)

	return cov.ID(), nil
}

// ── AddMedicalAlert ───────────────────────────────────────────────

type AddMedicalAlertCommand struct {
	PatientID      sharedtypes.PatientID
	AlertType      string
	Severity       string // ignorado si IsSelfReported=true (siempre Warning)
	Description    string
	IsSelfReported bool
	RequestedBy    uuid.UUID // ID del usuario que hace la request
	RequestedRole  string    // "paciente" | staff roles
}

type AddMedicalAlertHandler struct {
	repo     repository.PatientRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewAddMedicalAlertHandler(repo repository.PatientRepository, eventBus events.Bus) *AddMedicalAlertHandler {
	return &AddMedicalAlertHandler{
		repo:     repo,
		eventBus: eventBus,
		logger:   slog.Default().With("handler", "AddMedicalAlert"),
	}
}

func (h *AddMedicalAlertHandler) Handle(ctx context.Context, cmd AddMedicalAlertCommand) (uuid.UUID, error) {
	patient, err := h.repo.FindByID(ctx, cmd.PatientID)
	if err != nil {
		return uuid.Nil, err
	}

	alertType, err := valueobject.ParseAlertType(cmd.AlertType)
	if err != nil {
		return uuid.Nil, sharederrors.NewInvalidArgument("alert_type", err.Error())
	}

	var alertID uuid.UUID

	if cmd.IsSelfReported || cmd.RequestedRole == "paciente" {
		// El paciente agrega su propia alerta (siempre Warning, nunca Critical).
		if err := patient.AddSelfReportedAlert(alertType, cmd.Description, cmd.RequestedBy); err != nil {
			return uuid.Nil, err
		}
		// Obtenemos el ID del último alert agregado.
		alerts := patient.MedicalAlerts()
		alertID = alerts[len(alerts)-1].ID()
	} else {
		// Staff agrega la alerta con la severidad especificada.
		severity, err := valueobject.ParseAlertSeverity(cmd.Severity)
		if err != nil {
			return uuid.Nil, sharederrors.NewInvalidArgument("severity", err.Error())
		}
		if err := patient.AddMedicalAlertByStaff(alertType, severity, cmd.Description, cmd.RequestedBy); err != nil {
			return uuid.Nil, err
		}
		alerts := patient.MedicalAlerts()
		alertID = alerts[len(alerts)-1].ID()
	}

	if err := h.repo.Update(ctx, patient); err != nil {
		return uuid.Nil, fmt.Errorf("AddMedicalAlert: update: %w", err)
	}

	h.publishPendingEvents(ctx, patient)

	return alertID, nil
}

// ── RecordCompletedVisit (subscriber de AppointmentCompleted) ─────

// RecordCompletedVisitCommand se construye desde el evento AppointmentCompleted
// publicado por el contexto Scheduling.
type RecordCompletedVisitCommand struct {
	PatientID      sharedtypes.PatientID
	ProcedureCode  string
	Description    string
	PerformedAt    time.Time
	ClinicID       sharedtypes.ClinicID
	ProfessionalID sharedtypes.ProfessionalID
	SourceEventID  string
}

type RecordCompletedVisitHandler struct {
	repo   repository.PatientRepository
	logger *slog.Logger
}

func NewRecordCompletedVisitHandler(repo repository.PatientRepository) *RecordCompletedVisitHandler {
	return &RecordCompletedVisitHandler{
		repo:   repo,
		logger: slog.Default().With("handler", "RecordCompletedVisit"),
	}
}

func (h *RecordCompletedVisitHandler) Handle(ctx context.Context, cmd RecordCompletedVisitCommand) error {
	patient, err := h.repo.FindByID(ctx, cmd.PatientID)
	if err != nil {
		return err
	}

	patient.RecordCompletedVisit(aggregate.TreatmentSummary{
		ProcedureCode:  cmd.ProcedureCode,
		Description:    cmd.Description,
		PerformedAt:    cmd.PerformedAt,
		ClinicID:       cmd.ClinicID,
		ProfessionalID: cmd.ProfessionalID,
	}, cmd.SourceEventID)

	if err := h.repo.Update(ctx, patient); err != nil {
		return fmt.Errorf("RecordCompletedVisit: update: %w", err)
	}

	h.logger.InfoContext(ctx, "visita registrada en historial",
		"patient_id", cmd.PatientID,
		"procedure_code", cmd.ProcedureCode,
	)
	return nil
}

// ── MergePatients ─────────────────────────────────────────────────

type MergePatientsCommand struct {
	TargetPatientID sharedtypes.PatientID // el que sobrevive
	SourcePatientID sharedtypes.PatientID // el que se archiva
	MergedBy        uuid.UUID
}

type MergePatientsHandler struct {
	repo     repository.PatientRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewMergePatientsHandler(repo repository.PatientRepository, eventBus events.Bus) *MergePatientsHandler {
	return &MergePatientsHandler{
		repo:     repo,
		eventBus: eventBus,
		logger:   slog.Default().With("handler", "MergePatients"),
	}
}

func (h *MergePatientsHandler) Handle(ctx context.Context, cmd MergePatientsCommand) error {
	if cmd.TargetPatientID == cmd.SourcePatientID {
		return sharederrors.NewInvalidArgument("source_patient_id", "no puede ser igual al target")
	}

	// Archivar el paciente fuente.
	if err := h.repo.Archive(ctx, cmd.SourcePatientID,
		fmt.Sprintf("fusionado con patient_id=%s", cmd.TargetPatientID),
		cmd.MergedBy,
	); err != nil {
		return fmt.Errorf("MergePatients: archive source: %w", err)
	}

	// El evento notifica a scheduling y billing para reasignar.
	// (No modificamos el target aggregate aquí; la fusión de datos
	// puede ser un proceso más complejo según el caso de uso).

	h.logger.InfoContext(ctx, "pacientes fusionados",
		"target", cmd.TargetPatientID,
		"source", cmd.SourcePatientID,
	)
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────

func (h *RegisterPatientHandler) publishPendingEvents(ctx context.Context, p *aggregate.Patient) {
	for _, evt := range p.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}

func (h *AddCoverageHandler) publishPendingEvents(ctx context.Context, p *aggregate.Patient) {
	for _, evt := range p.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}

func (h *AddMedicalAlertHandler) publishPendingEvents(ctx context.Context, p *aggregate.Patient) {
	for _, evt := range p.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(), "error", err)
		}
	}
}

func buildHistoryEntry(
	patientID sharedtypes.PatientID,
	previous *coverage.PatientCoverage,
	next *coverage.PatientCoverage,
	changedBy uuid.UUID,
) coverage.CoverageHistoryEntry {
	entry := coverage.CoverageHistoryEntry{
		ID:        uuid.New(),
		PatientID: patientID,
		NewType:   next.CoverageType(),
		NewStatus: coverage.CoverageStatusActive,
		Reason:    "nueva cobertura agregada",
		ChangedAt: time.Now().UTC(),
		ChangedBy: changedBy,
	}
	if previous != nil {
		t := previous.CoverageType()
		s := previous.Status()
		entry.PreviousType = &t
		entry.PreviousStatus = &s
	}
	return entry
}
