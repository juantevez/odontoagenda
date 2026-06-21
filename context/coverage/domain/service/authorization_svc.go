// Package service — AuthorizationService Domain Service.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/coverage/domain/repository"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

const defaultAuthorizationDeadlineHours = 48

// ── AuthorizationService — Domain Service ────────────────────────

// AuthorizationService gestiona el ciclo de vida de las autorizaciones.
// Separa la lógica de creación, resolución y expiración del Aggregate,
// ya que requiere consultar el repositorio para verificar duplicados.
type AuthorizationService struct {
	authRepo repository.AuthorizationRepository
	logger   *slog.Logger
}

func NewAuthorizationService(authRepo repository.AuthorizationRepository) *AuthorizationService {
	return &AuthorizationService{
		authRepo: authRepo,
		logger:   slog.Default().With("service", "AuthorizationService"),
	}
}

// RequestAuthorization crea una nueva AuthorizationRequest si no hay una activa.
// Verifica que no exista una solicitud Pending o Approved para el mismo
// paciente + procedimiento (evita duplicados).
func (s *AuthorizationService) RequestAuthorization(
	ctx context.Context,
	agreementID, planID uuid.UUID,
	patientID sharedtypes.PatientID,
	membershipNumber, procedureCode string,
	appointmentID *uuid.UUID,
) (*aggregate.AuthorizationRequest, error) {

	// Verificar duplicado: ¿ya hay una Pending para este paciente + procedimiento?
	existing, err := s.authRepo.FindPendingByPatient(ctx, patientID, procedureCode)
	if err == nil && existing != nil {
		return nil, sharederrors.NewAlreadyExists(
			"AuthorizationRequest",
			"patient_id+procedure_code",
			fmt.Sprintf("%s/%s", patientID, procedureCode),
		)
	}

	// Calcular deadline: 48 hs desde ahora.
	deadline := time.Now().UTC().Add(defaultAuthorizationDeadlineHours * time.Hour)

	ar, err := aggregate.NewAuthorizationRequest(
		agreementID, planID,
		patientID, membershipNumber, procedureCode,
		appointmentID, &deadline,
	)
	if err != nil {
		return nil, err
	}

	if err := s.authRepo.Save(ctx, ar); err != nil {
		return nil, fmt.Errorf("AuthorizationService: save: %w", err)
	}

	s.logger.InfoContext(ctx, "autorización solicitada",
		"authorization_id", ar.ID(),
		"patient_id", patientID,
		"procedure_code", procedureCode,
	)
	return ar, nil
}

// ResolveAuthorization aprueba o rechaza una solicitud de autorización.
// resolvedStatus debe ser Approved o Rejected.
func (s *AuthorizationService) ResolveAuthorization(
	ctx context.Context,
	authorizationID uuid.UUID,
	resolvedStatus valueobject.AuthorizationStatus,
	authorizationCode string, // solo si Approved
	rejectionReason string,   // solo si Rejected
	resolvedBy uuid.UUID,
) (*aggregate.AuthorizationRequest, error) {

	ar, err := s.authRepo.FindByID(ctx, authorizationID)
	if err != nil {
		return nil, err
	}

	switch resolvedStatus {
	case valueobject.AuthorizationStatusApproved:
		if authorizationCode == "" {
			return nil, sharederrors.NewInvalidArgument("authorization_code",
				"requerido al aprobar una autorización")
		}
		if err := ar.Approve(authorizationCode, resolvedBy); err != nil {
			return nil, err
		}
	case valueobject.AuthorizationStatusRejected:
		if rejectionReason == "" {
			return nil, sharederrors.NewInvalidArgument("rejection_reason",
				"requerido al rechazar una autorización")
		}
		if err := ar.Reject(rejectionReason, resolvedBy); err != nil {
			return nil, err
		}
	default:
		return nil, sharederrors.NewInvalidArgument("status",
			fmt.Sprintf("estado inválido para resolución: '%s' (Approved o Rejected)", resolvedStatus))
	}

	if err := s.authRepo.Update(ctx, ar); err != nil {
		return nil, fmt.Errorf("AuthorizationService: update: %w", err)
	}

	s.logger.InfoContext(ctx, "autorización resuelta",
		"authorization_id", authorizationID,
		"status", resolvedStatus,
		"resolved_by", resolvedBy,
	)
	return ar, nil
}

// ExpireStaleAuthorizations marca como Expired todas las autorizaciones
// Pending que superaron su expiresAt. Llamado por el job scheduler cada hora.
// Retorna la cantidad de autorizaciones expiradas.
func (s *AuthorizationService) ExpireStaleAuthorizations(ctx context.Context) (int, error) {
	stale, err := s.authRepo.FindExpired(ctx, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("AuthorizationService: find expired: %w", err)
	}

	expired := 0
	for _, ar := range stale {
		if err := ar.Expire(); err != nil {
			s.logger.WarnContext(ctx, "no se pudo expirar autorización",
				"authorization_id", ar.ID(), "error", err)
			continue
		}
		if err := s.authRepo.Update(ctx, ar); err != nil {
			s.logger.WarnContext(ctx, "error persistiendo expiración",
				"authorization_id", ar.ID(), "error", err)
			continue
		}
		expired++
	}

	if expired > 0 {
		s.logger.InfoContext(ctx, "autorizaciones expiradas por job scheduler",
			"count", expired)
	}
	return expired, nil
}
