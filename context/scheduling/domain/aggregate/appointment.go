package aggregate

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/event"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── Appointment — Aggregate Root ──────────────────────────────────

// Appointment es el Aggregate Root de la reserva de cita.
//
// Invariantes:
//   - Solo puede transicionar entre estados según la máquina de estados definida.
//   - Una cita cancelada o completada no puede modificarse.
//   - BookedByPatientID puede diferir de PatientID (reserva familiar).
//   - La cancelación tardía (<CancellationFreeHours) queda registrada para Billing.
//   - Version para optimistic locking: crucial en reservas concurrentes.
type Appointment struct {
	id             sharedtypes.AppointmentID
	patientID      sharedtypes.PatientID      // quién recibe la atención
	bookedByID     sharedtypes.PatientID      // quién hizo la reserva (puede ser guardian)
	professionalID sharedtypes.ProfessionalID
	clinicID       sharedtypes.ClinicID
	procedureCode  string
	slot           valueobject.TimeSlot
	status         valueobject.AppointmentStatus

	// Contexto de cobertura (capturado al momento de la reserva)
	coverageType    string
	agreementID     *uuid.UUID
	requiresAuthID  *string  // código de autorización si fue requerida

	// Notas clínicas (agregadas al completar)
	clinicalNotes string

	// Cancelación
	cancellationReason  valueobject.CancellationReason
	cancellationNote    string
	cancelledAt         *time.Time
	cancelledByUserID   *uuid.UUID
	isLateCancellation  bool // true si canceló dentro del período de penalización

	// Auditoría
	createdAt time.Time
	updatedAt time.Time
	createdBy uuid.UUID

	// Optimistic locking — fundamental para prevención de doble reserva
	version int64

	pendingEvents []event.DomainEvent
}

// ── Máquina de estados ────────────────────────────────────────────

// validTransitions define las transiciones de estado permitidas.
var validTransitions = map[valueobject.AppointmentStatus][]valueobject.AppointmentStatus{
	valueobject.StatusPending:    {valueobject.StatusConfirmed, valueobject.StatusCancelled},
	valueobject.StatusConfirmed:  {valueobject.StatusInProgress, valueobject.StatusCancelled, valueobject.StatusNoShow},
	valueobject.StatusInProgress: {valueobject.StatusCompleted, valueobject.StatusCancelled},
	valueobject.StatusCompleted:  {}, // terminal
	valueobject.StatusCancelled:  {}, // terminal
	valueobject.StatusNoShow:     {}, // terminal
}

func canTransition(from, to valueobject.AppointmentStatus) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// ── Constructor ───────────────────────────────────────────────────

// NewAppointment crea una nueva cita en estado inicial.
// El estado inicial depende de si requiere autorización de cobertura:
//   - Sin autorización requerida → StatusConfirmed
//   - Con autorización requerida → StatusPending
func NewAppointment(
	patientID sharedtypes.PatientID,
	bookedByID sharedtypes.PatientID,
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	procedureCode string,
	slot valueobject.TimeSlot,
	coverageType string,
	agreementID *uuid.UUID,
	requiresAuthorization bool,
	createdBy uuid.UUID,
) (*Appointment, error) {
	if strings.TrimSpace(procedureCode) == "" {
		return nil, sharederrors.NewInvalidArgument("procedure_code", "requerido")
	}

	initialStatus := valueobject.StatusConfirmed
	if requiresAuthorization {
		initialStatus = valueobject.StatusPending
	}

	id := sharedtypes.NewID()
	now := time.Now().UTC()

	appt := &Appointment{
		id:             id,
		patientID:      patientID,
		bookedByID:     bookedByID,
		professionalID: professionalID,
		clinicID:       clinicID,
		procedureCode:  procedureCode,
		slot:           slot,
		status:         initialStatus,
		coverageType:   coverageType,
		agreementID:    agreementID,
		createdAt:      now,
		updatedAt:      now,
		createdBy:      createdBy,
		version:        1,
		pendingEvents:  []event.DomainEvent{},
	}

	appt.pendingEvents = append(appt.pendingEvents, event.AppointmentBooked{
		AppointmentID:  id,
		PatientID:      patientID,
		BookedByID:     bookedByID,
		ProfessionalID: professionalID,
		ClinicID:       clinicID,
		ProcedureCode:  procedureCode,
		SlotStart:      slot.Start,
		SlotEnd:        slot.End,
		Status:         string(initialStatus),
		CoverageType:   coverageType,
		OccurredAt:     now,
	})

	return appt, nil
}

// ReconstituteAppointment reconstruye desde persistencia sin disparar eventos.
func ReconstituteAppointment(
	id sharedtypes.AppointmentID,
	patientID, bookedByID sharedtypes.PatientID,
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	procedureCode string,
	slot valueobject.TimeSlot,
	status valueobject.AppointmentStatus,
	coverageType string,
	agreementID *uuid.UUID,
	requiresAuthID *string,
	clinicalNotes string,
	cancellationReason valueobject.CancellationReason,
	cancellationNote string,
	cancelledAt *time.Time,
	cancelledByUserID *uuid.UUID,
	isLateCancellation bool,
	createdAt, updatedAt time.Time,
	createdBy uuid.UUID,
	version int64,
) *Appointment {
	return &Appointment{
		id:                 id,
		patientID:          patientID,
		bookedByID:         bookedByID,
		professionalID:     professionalID,
		clinicID:           clinicID,
		procedureCode:      procedureCode,
		slot:               slot,
		status:             status,
		coverageType:       coverageType,
		agreementID:        agreementID,
		requiresAuthID:     requiresAuthID,
		clinicalNotes:      clinicalNotes,
		cancellationReason: cancellationReason,
		cancellationNote:   cancellationNote,
		cancelledAt:        cancelledAt,
		cancelledByUserID:  cancelledByUserID,
		isLateCancellation: isLateCancellation,
		createdAt:          createdAt,
		updatedAt:          updatedAt,
		createdBy:          createdBy,
		version:            version,
		pendingEvents:      []event.DomainEvent{},
	}
}

// ── Transiciones de estado ────────────────────────────────────────

// Confirm confirma una cita pendiente (ej: autorización de prepaga recibida).
func (a *Appointment) Confirm(authorizationCode *string) error {
	if !canTransition(a.status, valueobject.StatusConfirmed) {
		return sharederrors.NewPrecondition("invalid_transition",
			fmt.Sprintf("no se puede confirmar una cita en estado '%s'", a.status))
	}
	a.requiresAuthID = authorizationCode
	a.status = valueobject.StatusConfirmed
	a.updatedAt = time.Now().UTC()
	a.version++

	a.pendingEvents = append(a.pendingEvents, event.AppointmentConfirmed{
		AppointmentID:  a.id,
		PatientID:      a.patientID,
		ProfessionalID: a.professionalID,
		ClinicID:       a.clinicID,
		SlotStart:      a.slot.Start,
		OccurredAt:     time.Now().UTC(),
	})
	return nil
}

// CheckIn registra que el paciente llegó y la atención comenzó.
func (a *Appointment) CheckIn() error {
	if !canTransition(a.status, valueobject.StatusInProgress) {
		return sharederrors.NewPrecondition("invalid_transition",
			fmt.Sprintf("no se puede iniciar una cita en estado '%s'", a.status))
	}
	a.status = valueobject.StatusInProgress
	a.updatedAt = time.Now().UTC()
	a.version++
	a.pendingEvents = append(a.pendingEvents, event.AppointmentCheckedIn{
		AppointmentID:  a.id,
		PatientID:      a.patientID,
		ProfessionalID: a.professionalID,
		ClinicID:       a.clinicID,
		SlotStart:      a.slot.Start,
		OccurredAt:     a.updatedAt,
	})
	return nil
}

// Complete finaliza la cita con notas clínicas opcionales.
func (a *Appointment) Complete(clinicalNotes string, completedBy uuid.UUID) error {
	if !canTransition(a.status, valueobject.StatusCompleted) {
		return sharederrors.NewPrecondition("invalid_transition",
			fmt.Sprintf("no se puede completar una cita en estado '%s'", a.status))
	}
	a.status = valueobject.StatusCompleted
	a.clinicalNotes = strings.TrimSpace(clinicalNotes)
	a.updatedAt = time.Now().UTC()
	a.version++

	a.pendingEvents = append(a.pendingEvents, event.AppointmentCompleted{
		AppointmentID:  a.id,
		PatientID:      a.patientID,
		ProfessionalID: a.professionalID,
		ClinicID:       a.clinicID,
		ProcedureCode:  a.procedureCode,
		SlotStart:      a.slot.Start,
		SlotEnd:        a.slot.End,
		CompletedBy:    completedBy,
		OccurredAt:     time.Now().UTC(),
	})
	return nil
}

// Cancel cancela la cita.
// Calcula automáticamente si es cancelación tardía basándose en
// las horas de anticipación respecto al inicio del slot.
func (a *Appointment) Cancel(
	reason valueobject.CancellationReason,
	note string,
	cancelledBy uuid.UUID,
	cancellationFreeHours int,
) error {
	if !canTransition(a.status, valueobject.StatusCancelled) {
		return sharederrors.NewPrecondition("invalid_transition",
			fmt.Sprintf("no se puede cancelar una cita en estado '%s'", a.status))
	}
	if !reason.IsValid() {
		return sharederrors.NewInvalidArgument("cancellation_reason", "motivo inválido")
	}

	now := time.Now().UTC()
	hoursUntilSlot := a.slot.Start.Sub(now).Hours()
	isLate := hoursUntilSlot < float64(cancellationFreeHours) && reason != valueobject.CancelBySystem

	a.status = valueobject.StatusCancelled
	a.cancellationReason = reason
	a.cancellationNote = strings.TrimSpace(note)
	a.cancelledAt = &now
	a.cancelledByUserID = &cancelledBy
	a.isLateCancellation = isLate
	a.updatedAt = now
	a.version++

	a.pendingEvents = append(a.pendingEvents, event.AppointmentCancelled{
		AppointmentID:      a.id,
		PatientID:          a.patientID,
		ProfessionalID:     a.professionalID,
		ClinicID:           a.clinicID,
		SlotStart:          a.slot.Start,
		Reason:             string(reason),
		IsLateCancellation: isLate,
		CancelledBy:        cancelledBy,
		OccurredAt:         now,
	})
	return nil
}

// MarkNoShow registra que el paciente no se presentó.
func (a *Appointment) MarkNoShow(markedBy uuid.UUID) error {
	if !canTransition(a.status, valueobject.StatusNoShow) {
		return sharederrors.NewPrecondition("invalid_transition",
			fmt.Sprintf("no se puede marcar como no-show una cita en estado '%s'", a.status))
	}
	a.status = valueobject.StatusNoShow
	a.updatedAt = time.Now().UTC()
	a.version++

	a.pendingEvents = append(a.pendingEvents, event.AppointmentNoShow{
		AppointmentID:  a.id,
		PatientID:      a.patientID,
		ProfessionalID: a.professionalID,
		ClinicID:       a.clinicID,
		SlotStart:      a.slot.Start,
		MarkedBy:       markedBy,
		OccurredAt:     time.Now().UTC(),
	})
	return nil
}

// Reschedule crea una nueva cita con los datos actualizados y cancela la actual.
// Retorna la nueva cita; la cancelación debe persistirse por el handler.
func (a *Appointment) Reschedule(
	newSlot valueobject.TimeSlot,
	cancelledBy uuid.UUID,
	cancellationFreeHours int,
) (*Appointment, error) {
	// Primero cancelar la cita actual (rescheduling = cancel + re-book)
	if err := a.Cancel(valueobject.CancelByStaff, "reagendamiento", cancelledBy, cancellationFreeHours); err != nil {
		return nil, err
	}

	// La nueva cita se crea desde la saga BookAppointmentSaga.
	// Este método solo cancela la actual y sirve como punto de entrada semántico.
	return nil, nil
}

// ── Getters ───────────────────────────────────────────────────────

func (a *Appointment) ID() sharedtypes.AppointmentID             { return a.id }
func (a *Appointment) PatientID() sharedtypes.PatientID          { return a.patientID }
func (a *Appointment) BookedByID() sharedtypes.PatientID         { return a.bookedByID }
func (a *Appointment) ProfessionalID() sharedtypes.ProfessionalID { return a.professionalID }
func (a *Appointment) ClinicID() sharedtypes.ClinicID            { return a.clinicID }
func (a *Appointment) ProcedureCode() string                     { return a.procedureCode }
func (a *Appointment) Slot() valueobject.TimeSlot                { return a.slot }
func (a *Appointment) Status() valueobject.AppointmentStatus     { return a.status }
func (a *Appointment) CoverageType() string                      { return a.coverageType }
func (a *Appointment) AgreementID() *uuid.UUID                   { return a.agreementID }
func (a *Appointment) RequiresAuthID() *string                   { return a.requiresAuthID }
func (a *Appointment) ClinicalNotes() string                     { return a.clinicalNotes }
func (a *Appointment) CancellationReason() valueobject.CancellationReason { return a.cancellationReason }
func (a *Appointment) CancellationNote() string                  { return a.cancellationNote }
func (a *Appointment) CancelledAt() *time.Time                   { return a.cancelledAt }
func (a *Appointment) CancelledByUserID() *uuid.UUID             { return a.cancelledByUserID }
func (a *Appointment) IsLateCancellation() bool                  { return a.isLateCancellation }
func (a *Appointment) CreatedAt() time.Time                      { return a.createdAt }
func (a *Appointment) UpdatedAt() time.Time                      { return a.updatedAt }
func (a *Appointment) CreatedBy() uuid.UUID                      { return a.createdBy }
func (a *Appointment) Version() int64                            { return a.version }

func (a *Appointment) PendingEvents() []event.DomainEvent {
	evts := a.pendingEvents
	a.pendingEvents = nil
	return evts
}
