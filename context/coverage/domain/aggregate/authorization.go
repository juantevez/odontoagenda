// Package aggregate — AuthorizationRequest Aggregate Root.
package aggregate

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/event"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── AuthorizationRequest — Aggregate Root ─────────────────────────

// AuthorizationRequest gestiona el ciclo de vida de una solicitud de
// autorización a la prepaga para un procedimiento específico.
//
// Máquina de estados:
//   Pending → Approved | Rejected
//   Pending → Expired  (por job scheduler si supera el deadline)
//   Approved, Rejected, Expired → terminal (no transicionan)
//
// Invariante: una AuthorizationRequest Approved no puede pasar a Rejected.
type AuthorizationRequest struct {
	id                    uuid.UUID
	agreementID           uuid.UUID
	planID                uuid.UUID
	patientID             sharedtypes.PatientID
	patientMembershipNumber string
	procedureCode         string
	appointmentID         *uuid.UUID // puede no estar asignada todavía
	requestedAt           time.Time
	status                valueobject.AuthorizationStatus
	authorizationCode     *string  // provisto por la prepaga si Approved
	expiresAt             *time.Time
	rejectionReason       *string
	resolvedAt            *time.Time
	resolvedBy            *uuid.UUID
	version               int64
	pendingEvents         []event.DomainEvent
}

// NewAuthorizationRequest crea una nueva solicitud de autorización en estado Pending.
func NewAuthorizationRequest(
	agreementID uuid.UUID,
	planID uuid.UUID,
	patientID sharedtypes.PatientID,
	membershipNumber string,
	procedureCode string,
	appointmentID *uuid.UUID,
	expiresAt *time.Time,
) (*AuthorizationRequest, error) {
	if membershipNumber == "" {
		return nil, sharederrors.NewInvalidArgument("membership_number",
			"requerido para solicitar autorización")
	}
	if procedureCode == "" {
		return nil, sharederrors.NewInvalidArgument("procedure_code", "requerido")
	}

	now := time.Now().UTC()
	ar := &AuthorizationRequest{
		id:                    uuid.New(),
		agreementID:           agreementID,
		planID:                planID,
		patientID:             patientID,
		patientMembershipNumber: membershipNumber,
		procedureCode:         procedureCode,
		appointmentID:         appointmentID,
		requestedAt:           now,
		status:                valueobject.AuthorizationStatusPending,
		expiresAt:             expiresAt,
		version:               1,
		pendingEvents:         []event.DomainEvent{},
	}

	ar.pendingEvents = append(ar.pendingEvents, event.AuthorizationRequested{
		AuthorizationID: ar.id,
		AgreementID:     agreementID,
		PlanID:          planID,
		PatientID:       patientID,
		ProcedureCode:   procedureCode,
		AppointmentID:   appointmentID,
		OccurredAt:      now,
	})

	return ar, nil
}

// Approve aprueba la solicitud con el código de autorización de la prepaga.
func (ar *AuthorizationRequest) Approve(authorizationCode string, resolvedBy uuid.UUID) error {
	if ar.status.IsTerminal() {
		return sharederrors.NewPrecondition("authorization_pending",
			fmt.Sprintf("no se puede aprobar una solicitud en estado '%s'", ar.status))
	}

	now := time.Now().UTC()
	ar.status = valueobject.AuthorizationStatusApproved
	ar.authorizationCode = &authorizationCode
	ar.resolvedAt = &now
	ar.resolvedBy = &resolvedBy
	ar.version++

	ar.pendingEvents = append(ar.pendingEvents, event.AuthorizationResolved{
		AuthorizationID:   ar.id,
		PatientID:         ar.patientID,
		ProcedureCode:     ar.procedureCode,
		AppointmentID:     ar.appointmentID,
		Status:            string(ar.status),
		AuthorizationCode: ar.authorizationCode,
		OccurredAt:        now,
	})
	return nil
}

// Reject rechaza la solicitud con un motivo.
func (ar *AuthorizationRequest) Reject(reason string, resolvedBy uuid.UUID) error {
	if ar.status == valueobject.AuthorizationStatusApproved {
		return sharederrors.NewPrecondition("cannot_reject_approved",
			"no se puede rechazar una autorización ya aprobada")
	}
	if ar.status.IsTerminal() {
		return sharederrors.NewPrecondition("authorization_pending",
			fmt.Sprintf("no se puede rechazar una solicitud en estado '%s'", ar.status))
	}

	now := time.Now().UTC()
	ar.status = valueobject.AuthorizationStatusRejected
	ar.rejectionReason = &reason
	ar.resolvedAt = &now
	ar.resolvedBy = &resolvedBy
	ar.version++

	ar.pendingEvents = append(ar.pendingEvents, event.AuthorizationResolved{
		AuthorizationID: ar.id,
		PatientID:       ar.patientID,
		ProcedureCode:   ar.procedureCode,
		AppointmentID:   ar.appointmentID,
		Status:          string(ar.status),
		RejectionReason: ar.rejectionReason,
		OccurredAt:      now,
	})
	return nil
}

// Expire marca la solicitud como expirada (llamado por job scheduler).
func (ar *AuthorizationRequest) Expire() error {
	if !ar.status.IsPending() {
		return sharederrors.NewPrecondition("authorization_pending",
			fmt.Sprintf("no se puede expirar una solicitud en estado '%s'", ar.status))
	}

	now := time.Now().UTC()
	ar.status = valueobject.AuthorizationStatusExpired
	ar.resolvedAt = &now
	ar.version++

	ar.pendingEvents = append(ar.pendingEvents, event.AuthorizationExpired{
		AuthorizationID: ar.id,
		PatientID:       ar.patientID,
		AppointmentID:   ar.appointmentID,
		OccurredAt:      now,
	})
	return nil
}

// AssignAppointment vincula la autorización a un Appointment una vez creado.
func (ar *AuthorizationRequest) AssignAppointment(appointmentID uuid.UUID) {
	ar.appointmentID = &appointmentID
	ar.version++
}

// BumpVersion incrementa la versión tras persistencia exitosa.
func (ar *AuthorizationRequest) BumpVersion() { ar.version++ }

// PendingEvents retorna y limpia los eventos pendientes.
func (ar *AuthorizationRequest) PendingEvents() []event.DomainEvent {
	evts := ar.pendingEvents
	ar.pendingEvents = nil
	return evts
}

// Getters
func (ar *AuthorizationRequest) ID() uuid.UUID                              { return ar.id }
func (ar *AuthorizationRequest) AgreementID() uuid.UUID                    { return ar.agreementID }
func (ar *AuthorizationRequest) PlanID() uuid.UUID                         { return ar.planID }
func (ar *AuthorizationRequest) PatientID() sharedtypes.PatientID          { return ar.patientID }
func (ar *AuthorizationRequest) MembershipNumber() string                  { return ar.patientMembershipNumber }
func (ar *AuthorizationRequest) ProcedureCode() string                     { return ar.procedureCode }
func (ar *AuthorizationRequest) AppointmentID() *uuid.UUID                 { return ar.appointmentID }
func (ar *AuthorizationRequest) RequestedAt() time.Time                    { return ar.requestedAt }
func (ar *AuthorizationRequest) Status() valueobject.AuthorizationStatus   { return ar.status }
func (ar *AuthorizationRequest) AuthorizationCode() *string                { return ar.authorizationCode }
func (ar *AuthorizationRequest) ExpiresAt() *time.Time                     { return ar.expiresAt }
func (ar *AuthorizationRequest) RejectionReason() *string                  { return ar.rejectionReason }
func (ar *AuthorizationRequest) ResolvedAt() *time.Time                    { return ar.resolvedAt }
func (ar *AuthorizationRequest) ResolvedBy() *uuid.UUID                    { return ar.resolvedBy }
func (ar *AuthorizationRequest) Version() int64                            { return ar.version }

// ReconstituteAuthorization reconstruye desde persistencia sin disparar eventos.
func ReconstituteAuthorization(
	id, agreementID, planID uuid.UUID,
	patientID sharedtypes.PatientID,
	membershipNumber, procedureCode string,
	appointmentID *uuid.UUID,
	requestedAt time.Time,
	status valueobject.AuthorizationStatus,
	authorizationCode *string,
	expiresAt *time.Time,
	rejectionReason *string,
	resolvedAt *time.Time,
	resolvedBy *uuid.UUID,
	version int64,
) *AuthorizationRequest {
	return &AuthorizationRequest{
		id:                    id,
		agreementID:           agreementID,
		planID:                planID,
		patientID:             patientID,
		patientMembershipNumber: membershipNumber,
		procedureCode:         procedureCode,
		appointmentID:         appointmentID,
		requestedAt:           requestedAt,
		status:                status,
		authorizationCode:     authorizationCode,
		expiresAt:             expiresAt,
		rejectionReason:       rejectionReason,
		resolvedAt:            resolvedAt,
		resolvedBy:            resolvedBy,
		version:               version,
		pendingEvents:         []event.DomainEvent{},
	}
}
