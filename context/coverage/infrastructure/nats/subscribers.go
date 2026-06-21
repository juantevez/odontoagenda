// Package nats contiene los adaptadores de entrada async del bounded context Coverage.
// Consume eventos de Patient BC para mantener sincronizadas las afiliaciones.
package nats

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/repository"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// CoverageEventSubscriber registra todos los consumers NATS del BC Coverage.
type CoverageEventSubscriber struct {
	bus             pkgevents.Bus
	affiliationRepo repository.PatientAffiliationRepository
	authRepo        repository.AuthorizationRepository
	logger          *slog.Logger
}

func NewCoverageEventSubscriber(
	bus pkgevents.Bus,
	affiliationRepo repository.PatientAffiliationRepository,
	authRepo repository.AuthorizationRepository,
) *CoverageEventSubscriber {
	return &CoverageEventSubscriber{
		bus:             bus,
		affiliationRepo: affiliationRepo,
		authRepo:        authRepo,
		logger:          slog.Default().With("component", "coverage.nats"),
	}
}

// RegisterAll registra todos los consumers del BC Coverage.
func (s *CoverageEventSubscriber) RegisterAll(ctx context.Context) error {

	// patient.coverage.updated → sincronizar afiliaciones
	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "PATIENT_EVENTS",
		Subject:      "patient.coverage.updated",
		ConsumerName: "coverage-patient-coverage-updated",
		MaxRetries:   5,
		RetryBackoff: 10 * time.Second,
	}, s.handlePatientCoverageUpdated); err != nil {
		return err
	}

	// patient.archived → cancelar autorizaciones pendientes
	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "PATIENT_EVENTS",
		Subject:      "patient.archived",
		ConsumerName: "coverage-patient-archived",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handlePatientArchived); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "coverage subscribers registrados")
	return nil
}

// ── Payloads ACL ──────────────────────────────────────────────────

// patientCoverageUpdatedPayload es el payload del evento patient.coverage.updated
// publicado por el BC Patient.
type patientCoverageUpdatedPayload struct {
	PatientID    string    `json:"patient_id"`
	CoverageID   string    `json:"coverage_id"`
	CoverageType string    `json:"coverage_type"`
	AgreementID  *string   `json:"agreement_id,omitempty"`
	Action       string    `json:"action"` // "added" | "suspended" | "expired"
	OccurredAt   time.Time `json:"occurred_at"`
	// Campos adicionales para sincronizar la afiliación en Coverage.
	// En el MVP estos campos pueden no venir en el evento original;
	// en ese caso Coverage hace un best-effort con lo disponible.
	PlanID           *string   `json:"plan_id,omitempty"`
	MembershipNumber string    `json:"membership_number,omitempty"`
	ValidFrom        *string   `json:"valid_from,omitempty"` // YYYY-MM-DD
}

type patientArchivedPayload struct {
	PatientID  string    `json:"patient_id"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}

// ── Handlers ──────────────────────────────────────────────────────

func (s *CoverageEventSubscriber) handlePatientCoverageUpdated(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[patientCoverageUpdatedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando patient.coverage.updated",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	patientID, err := uuid.Parse(p.PatientID)
	if err != nil {
		s.logger.ErrorContext(ctx, "patient_id inválido", "raw", p.PatientID)
		return pkgevents.ErrSkipRetry
	}

	switch p.Action {
	case "added":
		return s.syncAffiliationAdded(ctx, sharedtypes.PatientID(patientID), p)
	case "suspended", "expired":
		return s.suspendAffiliation(ctx, sharedtypes.PatientID(patientID))
	default:
		s.logger.WarnContext(ctx, "acción desconocida en patient.coverage.updated",
			"action", p.Action, "event_id", env.EventID)
		return nil
	}
}

func (s *CoverageEventSubscriber) syncAffiliationAdded(
	ctx context.Context,
	patientID sharedtypes.PatientID,
	p patientCoverageUpdatedPayload,
) error {
	// Si no vienen planID ni agreementID en el evento, no podemos sincronizar.
	// El BC Patient debería enriquecer el evento en el futuro.
	if p.AgreementID == nil || p.PlanID == nil {
		s.logger.WarnContext(ctx, "patient.coverage.updated sin agreement_id o plan_id, omitiendo sincronización de afiliación",
			"patient_id", patientID)
		return nil
	}

	agreementID, err := uuid.Parse(*p.AgreementID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}
	planID, err := uuid.Parse(*p.PlanID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	affiliatedSince := time.Now().UTC()
	if p.ValidFrom != nil {
		if t, err := time.Parse("2006-01-02", *p.ValidFrom); err == nil {
			affiliatedSince = t
		}
	}

	affiliation := repository.PatientAffiliation{
		ID:               uuid.New(),
		PatientID:        patientID,
		AgreementID:      agreementID,
		PlanID:           planID,
		MembershipNumber: p.MembershipNumber,
		AffiliatedSince:  affiliatedSince,
		Status:           valueobject.AffiliationStatusActive,
		CreatedAt:        time.Now().UTC(),
	}

	if err := s.affiliationRepo.Upsert(ctx, affiliation); err != nil {
		s.logger.ErrorContext(ctx, "error sincronizando afiliación",
			"patient_id", patientID, "plan_id", planID, "error", err)
		return err
	}

	s.logger.InfoContext(ctx, "afiliación sincronizada",
		"patient_id", patientID,
		"agreement_id", agreementID,
		"plan_id", planID,
	)
	return nil
}

func (s *CoverageEventSubscriber) suspendAffiliation(
	ctx context.Context,
	patientID sharedtypes.PatientID,
) error {
	if err := s.affiliationRepo.SuspendByPatient(ctx, patientID); err != nil {
		s.logger.ErrorContext(ctx, "error suspendiendo afiliaciones del paciente",
			"patient_id", patientID, "error", err)
		return err
	}
	s.logger.InfoContext(ctx, "afiliaciones suspendidas",
		"patient_id", patientID)
	return nil
}

func (s *CoverageEventSubscriber) handlePatientArchived(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[patientArchivedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando patient.archived",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	patientID, err := uuid.Parse(p.PatientID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	// Suspender todas las afiliaciones activas del paciente.
	if err := s.affiliationRepo.SuspendByPatient(ctx, sharedtypes.PatientID(patientID)); err != nil {
		s.logger.ErrorContext(ctx, "error suspendiendo afiliaciones de paciente archivado",
			"patient_id", patientID, "error", err)
		return err
	}

	// En el MVP: no cancelamos las autorizaciones pendientes automáticamente.
	// El staff las verá en la cola de pendientes y las resolverá manualmente.
	s.logger.InfoContext(ctx, "paciente archivado: afiliaciones suspendidas",
		"patient_id", patientID)
	return nil
}
