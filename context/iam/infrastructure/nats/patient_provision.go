// Package nats contiene los adaptadores de entrada async del bounded context IAM.
//
// PatientProvisionSubscriber escucha el evento user.registered publicado por el
// BC IAM cuando se registra un usuario con rol 'paciente', y orquesta la creación
// automática del Patient en el BC Patient + la actualización del linked_id en el User.
//
// Flujo:
//   POST /auth/register (IAM) → publica user.registered
//   → PatientProvisionSubscriber.handleUserRegistered()
//   → RegisterPatientHandler.Handle() → persiste Patient
//   → userRepo.SetLinkedID(userID, patientID) → actualiza iam.users.linked_id
//
// Esto resuelve el bug de diseño documentado: un usuario 'paciente' que se
// auto-registra queda automáticamente vinculado a su registro Patient.
package nats

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	patientcmd "github.com/juantevez/odontoagenda/context/patient/application/command"
	iamrepo "github.com/juantevez/odontoagenda/context/iam/domain/repository"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
)

// PatientProvisionSubscriber registra el consumer que vincula IAM → Patient.
type PatientProvisionSubscriber struct {
	bus                    pkgevents.Bus
	registerPatientHandler *patientcmd.RegisterPatientHandler
	userRepo               iamrepo.UserRepository
	logger                 *slog.Logger
}

func NewPatientProvisionSubscriber(
	bus pkgevents.Bus,
	registerPatient *patientcmd.RegisterPatientHandler,
	userRepo iamrepo.UserRepository,
) *PatientProvisionSubscriber {
	return &PatientProvisionSubscriber{
		bus:                    bus,
		registerPatientHandler: registerPatient,
		userRepo:               userRepo,
		logger:                 slog.Default().With("component", "iam.nats.patient_provision"),
	}
}

// RegisterAll registra el consumer user.registered en el stream IAM_EVENTS.
func (s *PatientProvisionSubscriber) RegisterAll(ctx context.Context) error {
	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "IAM_EVENTS",
		Subject:      "user.registered",
		ConsumerName: "iam-provision-patient",
		MaxRetries:   5,
		RetryBackoff: 10 * time.Second,
	}, s.handleUserRegistered); err != nil {
		return fmt.Errorf("PatientProvisionSubscriber.RegisterAll: %w", err)
	}

	s.logger.InfoContext(ctx, "IAM patient provision subscriber registrado")
	return nil
}

// ── Payload ACL ───────────────────────────────────────────────────

// userRegisteredPayload es la estructura del evento user.registered
// publicado por el Aggregate User en context/iam/domain/event/events.go.
type userRegisteredPayload struct {
	UserID     string     `json:"user_id"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	LinkedID   *string    `json:"linked_id,omitempty"`
	OccurredAt time.Time  `json:"occurred_at"`
}

// ── Handler ───────────────────────────────────────────────────────

// handleUserRegistered procesa el evento. Solo actúa si role == "paciente".
func (s *PatientProvisionSubscriber) handleUserRegistered(ctx context.Context, env pkgevents.Envelope) error {
	payload, err := pkgevents.UnmarshalPayload[userRegisteredPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando user.registered",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	// Solo procesamos usuarios con rol 'paciente'.
	if payload.Role != "paciente" {
		return nil
	}

	// Si ya tiene linked_id, el Patient ya fue creado (idempotencia).
	if payload.LinkedID != nil && *payload.LinkedID != "" {
		s.logger.InfoContext(ctx, "user ya tiene linked_id, omitiendo provisión",
			"user_id", payload.UserID, "linked_id", *payload.LinkedID)
		return nil
	}

	userID, err := uuid.Parse(payload.UserID)
	if err != nil {
		s.logger.ErrorContext(ctx, "user_id inválido en user.registered",
			"raw", payload.UserID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	// Construir un RegisterPatientCommand mínimo a partir del email.
	// Los datos personales completos los ingresará el paciente después
	// a través de la app (editar perfil). Por ahora creamos un Patient
	// con los datos mínimos disponibles en el evento.
	//
	// NOTA: el email viene del registro en IAM.
	// El nombre, DNI y teléfono son placeholders que el paciente
	// deberá completar en su primera sesión.
	cmd := patientcmd.RegisterPatientCommand{
		// Datos mínimos: usamos el email como nombre provisional.
		// El paciente los completará vía PUT /patients/{id}/profile.
		FullName:           extractNameFromEmail(payload.Email),
		BirthDate:          time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), // placeholder
		Gender:             "NS", // No Especificado — pendiente de completar
		DocType:            "DNI",
		DocNumber:          "PENDIENTE-" + userID.String()[:8], // placeholder único
		Phone:              "+5400000000000",                    // placeholder
		SkipDuplicateCheck: true,                               // evitar falsos duplicados con placeholders
		UserID:             &userID,
	}

	result, err := s.registerPatientHandler.Handle(ctx, cmd)
	if err != nil {
		s.logger.ErrorContext(ctx, "error creando Patient para usuario recién registrado",
			"user_id", userID, "error", err)
		return err // retry
	}

	if result.PatientID == uuid.Nil {
		// Hubo DUPLICATE_WARNING pero skip_duplicate_check=true — no debería ocurrir.
		s.logger.WarnContext(ctx, "RegisterPatient no devolvió PatientID",
			"user_id", userID)
		return nil
	}

	// Actualizar linked_id en iam.users.
	patientIDCopy := result.PatientID
	if err := s.linkUserToPatient(ctx, userID, patientIDCopy); err != nil {
		s.logger.ErrorContext(ctx, "error actualizando linked_id en User",
			"user_id", userID, "patient_id", patientIDCopy, "error", err)
		return err // retry — el Patient ya fue creado pero el link falló
	}

	s.logger.InfoContext(ctx, "Patient provisionado y vinculado al User",
		"user_id", userID,
		"patient_id", patientIDCopy,
		"event_id", env.EventID,
	)
	return nil
}

// linkUserToPatient actualiza linked_id y linked_type en iam.users.
func (s *PatientProvisionSubscriber) linkUserToPatient(ctx context.Context, userID uuid.UUID, patientID uuid.UUID) error {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("linkUserToPatient: find user: %w", err)
	}

	// SetLinkedID actualiza el campo linked_id y linked_type en el aggregate User.
	user.SetLinkedID(&patientID, "patient")

	if err := s.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("linkUserToPatient: update user: %w", err)
	}
	return nil
}

// extractNameFromEmail intenta obtener la parte local del email como nombre provisional.
// Ejemplo: "juan.perez@gmail.com" → "juan.perez"
func extractNameFromEmail(email string) string {
	for i, c := range email {
		if c == '@' && i > 0 {
			local := email[:i]
			if len(local) >= 2 {
				return local
			}
		}
	}
	// Fallback: si el email está mal formado, usamos un nombre genérico.
	return "Paciente"
}
