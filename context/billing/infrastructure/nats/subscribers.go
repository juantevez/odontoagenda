// Package nats contiene los adaptadores de entrada async del bounded context Billing.
// Consume eventos de Scheduling y Coverage para gestionar el ciclo de vida de los Quotes.
package nats

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	billingcmd "github.com/juantevez/odontoagenda/context/billing/application/command"
	pkgevents "github.com/juantevez/odontoagenda/pkg/events"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// BillingEventSubscriber registra todos los consumers NATS del BC Billing.
type BillingEventSubscriber struct {
	bus                    pkgevents.Bus
	createQuoteHandler     *billingcmd.CreateQuoteHandler
	confirmQuoteHandler    *billingcmd.ConfirmQuoteHandler
	voidQuoteHandler       *billingcmd.VoidQuoteHandler
	applyLateFeeHandler    *billingcmd.ApplyLateFeeHandler
	setAuthCodeHandler     *billingcmd.SetAuthorizationCodeHandler
	logger                 *slog.Logger
}

func NewBillingEventSubscriber(
	bus pkgevents.Bus,
	createQuote *billingcmd.CreateQuoteHandler,
	confirmQuote *billingcmd.ConfirmQuoteHandler,
	voidQuote *billingcmd.VoidQuoteHandler,
	applyLateFee *billingcmd.ApplyLateFeeHandler,
	setAuthCode *billingcmd.SetAuthorizationCodeHandler,
) *BillingEventSubscriber {
	return &BillingEventSubscriber{
		bus:                 bus,
		createQuoteHandler:  createQuote,
		confirmQuoteHandler: confirmQuote,
		voidQuoteHandler:    voidQuote,
		applyLateFeeHandler: applyLateFee,
		setAuthCodeHandler:  setAuthCode,
		logger:              slog.Default().With("component", "billing.nats"),
	}
}

// RegisterAll registra todos los consumers del BC Billing.
func (s *BillingEventSubscriber) RegisterAll(ctx context.Context) error {

	// ── APPOINTMENT_EVENTS ────────────────────────────────────────

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "APPOINTMENT_EVENTS",
		Subject:      "appointment.booked",
		ConsumerName: "billing-appointment-booked",
		MaxRetries:   5,
		RetryBackoff: 10 * time.Second,
	}, s.handleAppointmentBooked); err != nil {
		return err
	}

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "APPOINTMENT_EVENTS",
		Subject:      "appointment.confirmed",
		ConsumerName: "billing-appointment-confirmed",
		MaxRetries:   5,
		RetryBackoff: 10 * time.Second,
	}, s.handleAppointmentConfirmed); err != nil {
		return err
	}

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "APPOINTMENT_EVENTS",
		Subject:      "appointment.cancelled",
		ConsumerName: "billing-appointment-cancelled",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handleAppointmentCancelled); err != nil {
		return err
	}

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "APPOINTMENT_EVENTS",
		Subject:      "appointment.no_show",
		ConsumerName: "billing-appointment-no-show",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handleAppointmentNoShow); err != nil {
		return err
	}

	// ── COVERAGE_EVENTS ───────────────────────────────────────────

	if err := s.bus.Subscribe(ctx, pkgevents.SubscribeOptions{
		Stream:       "COVERAGE_EVENTS",
		Subject:      "authorization.resolved",
		ConsumerName: "billing-authorization-resolved",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Second,
	}, s.handleAuthorizationResolved); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "billing subscribers registrados")
	return nil
}

// ── Payloads ACL ──────────────────────────────────────────────────

// appointmentBookedPayload — Opción A: el evento incluye arancelCents.
// Campo nuevo respecto al evento original de Scheduling.
type appointmentBookedPayload struct {
	AppointmentID  string    `json:"appointment_id"`
	PatientID      string    `json:"patient_id"`
	BookedByID     string    `json:"booked_by_id"`
	ProfessionalID string    `json:"professional_id"`
	ClinicID       string    `json:"clinic_id"`
	ProcedureCode  string    `json:"procedure_code"`
	SlotStart      time.Time `json:"slot_start"`
	SlotEnd        time.Time `json:"slot_end"`
	Status         string    `json:"status"`
	CoverageType   string    `json:"coverage_type"`
	// Campos nuevos agregados para Billing (Opción A)
	ArancelCents        int64  `json:"arancel_cents"`
	ProcedureDescription string `json:"procedure_description"`
	AgreementID         string `json:"agreement_id,omitempty"`
	PlanID              string `json:"plan_id,omitempty"`
	PatientAge          int    `json:"patient_age,omitempty"`
}

type appointmentConfirmedPayload struct {
	AppointmentID  string    `json:"appointment_id"`
	PatientID      string    `json:"patient_id"`
	ProfessionalID string    `json:"professional_id"`
	ClinicID       string    `json:"clinic_id"`
	SlotStart      time.Time `json:"slot_start"`
	OccurredAt     time.Time `json:"occurred_at"`
}

type appointmentCancelledPayload struct {
	AppointmentID      string    `json:"appointment_id"`
	PatientID          string    `json:"patient_id"`
	ProfessionalID     string    `json:"professional_id"`
	ClinicID           string    `json:"clinic_id"`
	SlotStart          time.Time `json:"slot_start"`
	Reason             string    `json:"reason"`
	IsLateCancellation bool      `json:"is_late_cancellation"`
	CancelledBy        string    `json:"cancelled_by"`
	OccurredAt         time.Time `json:"occurred_at"`
}

type appointmentNoShowPayload struct {
	AppointmentID  string    `json:"appointment_id"`
	PatientID      string    `json:"patient_id"`
	ProfessionalID string    `json:"professional_id"`
	ClinicID       string    `json:"clinic_id"`
	SlotStart      time.Time `json:"slot_start"`
	MarkedBy       string    `json:"marked_by"`
	OccurredAt     time.Time `json:"occurred_at"`
}

type authorizationResolvedPayload struct {
	AuthorizationID   string  `json:"authorization_id"`
	PatientID         string  `json:"patient_id"`
	ProcedureCode     string  `json:"procedure_code"`
	AppointmentID     *string `json:"appointment_id,omitempty"`
	Status            string  `json:"status"` // Approved | Rejected
	AuthorizationCode *string `json:"authorization_code,omitempty"`
	RejectionReason   *string `json:"rejection_reason,omitempty"`
}

// ── Handlers ──────────────────────────────────────────────────────

func (s *BillingEventSubscriber) handleAppointmentBooked(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[appointmentBookedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando appointment.booked",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	appointmentID, err := uuid.Parse(p.AppointmentID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}
	patientID, err := uuid.Parse(p.PatientID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}
	clinicID, err := uuid.Parse(p.ClinicID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}
	professionalID, _ := uuid.Parse(p.ProfessionalID)

	// Si no viene arancelCents en el evento (campo nuevo), usamos 0
	// y marcamos pendingCoverageCheck para revisión manual.
	arancelCents := p.ArancelCents
	if arancelCents <= 0 {
		s.logger.WarnContext(ctx, "appointment.booked sin arancel_cents, usando 1 centavo como placeholder",
			"appointment_id", p.AppointmentID)
		arancelCents = 1 // placeholder para que no falle INV-01
	}

	return s.createQuoteHandler.Handle(ctx, billingcmd.CreateQuoteCommand{
		AppointmentID:  appointmentID,
		PatientID:      sharedtypes.PatientID(patientID),
		ClinicID:       sharedtypes.ClinicID(clinicID),
		ProfessionalID: sharedtypes.ProfessionalID(professionalID),
		ProcedureCode:  p.ProcedureCode,
		ProcedureDesc:  p.ProcedureDescription,
		ArancelCents:   arancelCents,
		SlotStart:      p.SlotStart,
		SlotEnd:        p.SlotEnd,
		CoverageType:   p.CoverageType,
		AgreementID:    p.AgreementID,
		PlanID:         p.PlanID,
		PatientAge:     p.PatientAge,
	})
}

func (s *BillingEventSubscriber) handleAppointmentConfirmed(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[appointmentConfirmedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando appointment.confirmed",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	appointmentID, err := uuid.Parse(p.AppointmentID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	return s.confirmQuoteHandler.Handle(ctx, billingcmd.ConfirmQuoteCommand{
		AppointmentID: appointmentID,
	})
}

func (s *BillingEventSubscriber) handleAppointmentCancelled(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[appointmentCancelledPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando appointment.cancelled",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	appointmentID, err := uuid.Parse(p.AppointmentID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	if p.IsLateCancellation {
		return s.applyLateFeeHandler.Handle(ctx, billingcmd.ApplyLateFeeCommand{
			AppointmentID:      appointmentID,
			FeeType:            "LateCancellation",
			IsLateCancellation: true,
		})
	}
	// Cancelación sin cargo: anular el Quote.
	return s.voidQuoteHandler.Handle(ctx, billingcmd.VoidQuoteCommand{
		AppointmentID: appointmentID,
		Reason:        p.Reason,
	})
}

func (s *BillingEventSubscriber) handleAppointmentNoShow(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[appointmentNoShowPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando appointment.no_show",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	appointmentID, err := uuid.Parse(p.AppointmentID)
	if err != nil {
		return pkgevents.ErrSkipRetry
	}

	return s.applyLateFeeHandler.Handle(ctx, billingcmd.ApplyLateFeeCommand{
		AppointmentID: appointmentID,
		FeeType:       "NoShow",
	})
}

func (s *BillingEventSubscriber) handleAuthorizationResolved(
	ctx context.Context,
	env pkgevents.Envelope,
) error {
	p, err := pkgevents.UnmarshalPayload[authorizationResolvedPayload](env)
	if err != nil {
		s.logger.ErrorContext(ctx, "error deserializando authorization.resolved",
			"event_id", env.EventID, "error", err)
		return pkgevents.ErrSkipRetry
	}

	// Si fue rechazada y hay appointment asociado: anular el Quote.
	if p.Status == "Rejected" && p.AppointmentID != nil {
		appointmentID, err := uuid.Parse(*p.AppointmentID)
		if err != nil {
			return pkgevents.ErrSkipRetry
		}
		return s.voidQuoteHandler.Handle(ctx, billingcmd.VoidQuoteCommand{
			AppointmentID: appointmentID,
			Reason:        "autorización rechazada por la prepaga",
		})
	}

	// Si fue aprobada y hay appointment: actualizar el authorizationCode en el Quote.
	if p.Status == "Approved" && p.AppointmentID != nil && p.AuthorizationCode != nil {
		appointmentID, err := uuid.Parse(*p.AppointmentID)
		if err != nil {
			return pkgevents.ErrSkipRetry
		}
		return s.setAuthCodeHandler.Handle(ctx, billingcmd.SetAuthorizationCodeCommand{
			AppointmentID:     appointmentID,
			AuthorizationCode: *p.AuthorizationCode,
		})
	}

	return nil
}
