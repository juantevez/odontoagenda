// Package query contiene los Query Handlers del bounded context Billing.
package query

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/billing/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/billing/domain/repository"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── DTOs ─────────────────────────────────────────────────────────

type QuoteDTO struct {
	ID                  string       `json:"id"`
	AppointmentID       string       `json:"appointment_id"`
	PatientID           string       `json:"patient_id"`
	ClinicID            string       `json:"clinic_id"`
	ProcedureCode       string       `json:"procedure_code"`
	ProcedureDesc       string       `json:"procedure_description"`
	Status              string       `json:"status"`
	ArancelCents        int64        `json:"arancel_cents"`
	CoveragePercent     int          `json:"coverage_percent"`
	CoverageAmountCents int64        `json:"coverage_amount_cents"`
	CoPayType           string       `json:"co_pay_type"`
	CoPayAmountCents    int64        `json:"co_pay_amount_cents"`
	PendingAmountCents  int64        `json:"pending_amount_cents"`
	TotalPaidCents      int64        `json:"total_paid_cents"`
	CoverageType        string       `json:"coverage_type,omitempty"`
	AgreementID         *string      `json:"agreement_id,omitempty"`
	RequiresAuthorization bool       `json:"requires_authorization"`
	AuthorizationCode   *string      `json:"authorization_code,omitempty"`
	PendingCoverageCheck bool        `json:"pending_coverage_check"`
	SlotStart           string       `json:"slot_start"`
	SlotEnd             string       `json:"slot_end"`
	Payments            []PaymentDTO `json:"payments"`
	LateFees            []LateFeeDTO `json:"late_fees"`
	CreatedAt           string       `json:"created_at"`
	UpdatedAt           string       `json:"updated_at"`
}

type PaymentDTO struct {
	ID                string  `json:"id"`
	AmountCents       int64   `json:"amount_cents"`
	PaymentMethod     string  `json:"payment_method"`
	Status            string  `json:"status"`
	ExternalReference *string `json:"external_reference,omitempty"`
	PaidAt            *string `json:"paid_at,omitempty"`
	ReceiptNumber     *string `json:"receipt_number,omitempty"`
	Notes             string  `json:"notes,omitempty"`
	CreatedAt         string  `json:"created_at"`
}

type LateFeeDTO struct {
	ID           string  `json:"id"`
	FeeType      string  `json:"fee_type"`
	AmountCents  int64   `json:"amount_cents"`
	Status       string  `json:"status"`
	WaivedReason string  `json:"waived_reason,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// PatientAccountDTO es el resumen de la cuenta del paciente.
type PatientAccountDTO struct {
	PatientID          string     `json:"patient_id"`
	TotalPendingCents  int64      `json:"total_pending_cents"`
	TotalPaidCents     int64      `json:"total_paid_cents"`
	TotalLateFeesCents int64      `json:"total_late_fees_cents"`
	ActiveQuotes       []QuoteDTO `json:"active_quotes"`
}

// DailyReportDTO es el resumen de caja del día.
type DailyReportDTO struct {
	ClinicID        string     `json:"clinic_id"`
	Date            string     `json:"date"`
	TotalQuotes     int        `json:"total_quotes"`
	TotalPaidCents  int64      `json:"total_paid_cents"`
	TotalPendingCents int64    `json:"total_pending_cents"`
	TotalFeesCents  int64      `json:"total_late_fees_cents"`
	Quotes          []QuoteDTO `json:"quotes"`
}

// ── GetQuoteByID ──────────────────────────────────────────────────

type GetQuoteByIDQuery struct {
	QuoteID uuid.UUID
}

type GetQuoteByIDHandler struct {
	repo repository.QuoteRepository
}

func NewGetQuoteByIDHandler(repo repository.QuoteRepository) *GetQuoteByIDHandler {
	return &GetQuoteByIDHandler{repo: repo}
}

func (h *GetQuoteByIDHandler) Handle(ctx context.Context, q GetQuoteByIDQuery) (*QuoteDTO, error) {
	quote, err := h.repo.FindByID(ctx, q.QuoteID)
	if err != nil {
		return nil, err
	}
	return toQuoteDTO(quote), nil
}

// ── GetQuoteByAppointment ─────────────────────────────────────────

type GetQuoteByAppointmentQuery struct {
	AppointmentID uuid.UUID
}

type GetQuoteByAppointmentHandler struct {
	repo repository.QuoteRepository
}

func NewGetQuoteByAppointmentHandler(repo repository.QuoteRepository) *GetQuoteByAppointmentHandler {
	return &GetQuoteByAppointmentHandler{repo: repo}
}

func (h *GetQuoteByAppointmentHandler) Handle(ctx context.Context, q GetQuoteByAppointmentQuery) (*QuoteDTO, error) {
	quote, err := h.repo.FindByAppointmentID(ctx, q.AppointmentID)
	if err != nil {
		return nil, err
	}
	return toQuoteDTO(quote), nil
}

// ── GetPatientAccount ─────────────────────────────────────────────

type GetPatientAccountQuery struct {
	PatientID sharedtypes.PatientID
	Page      sharedtypes.Page
}

type GetPatientAccountHandler struct {
	repo repository.QuoteRepository
}

func NewGetPatientAccountHandler(repo repository.QuoteRepository) *GetPatientAccountHandler {
	return &GetPatientAccountHandler{repo: repo}
}

func (h *GetPatientAccountHandler) Handle(ctx context.Context, q GetPatientAccountQuery) (*PatientAccountDTO, error) {
	result, err := h.repo.FindActiveByPatient(ctx, q.PatientID, q.Page)
	if err != nil {
		return nil, err
	}

	account := &PatientAccountDTO{
		PatientID:    q.PatientID.String(),
		ActiveQuotes: make([]QuoteDTO, 0, len(result.Items)),
	}

	for _, quote := range result.Items {
		account.TotalPendingCents += quote.PendingAmountCents()
		account.TotalPaidCents += quote.TotalConfirmedCents()
		for _, fee := range quote.LateFees() {
			if fee.Status() == "Pending" {
				account.TotalLateFeesCents += fee.AmountCents()
			}
		}
		account.ActiveQuotes = append(account.ActiveQuotes, *toQuoteDTO(quote))
	}

	return account, nil
}

// ── GetPatientQuotes ──────────────────────────────────────────────

type GetPatientQuotesQuery struct {
	PatientID sharedtypes.PatientID
	Page      sharedtypes.Page
}

type GetPatientQuotesHandler struct {
	repo repository.QuoteRepository
}

func NewGetPatientQuotesHandler(repo repository.QuoteRepository) *GetPatientQuotesHandler {
	return &GetPatientQuotesHandler{repo: repo}
}

func (h *GetPatientQuotesHandler) Handle(ctx context.Context, q GetPatientQuotesQuery) (sharedtypes.PagedResult[*QuoteDTO], error) {
	result, err := h.repo.FindByPatient(ctx, q.PatientID, q.Page)
	if err != nil {
		return sharedtypes.PagedResult[*QuoteDTO]{}, err
	}

	dtos := make([]*QuoteDTO, len(result.Items))
	for i, quote := range result.Items {
		dtos[i] = toQuoteDTO(quote)
	}
	return sharedtypes.NewPagedResult(dtos, result.Total, q.Page), nil
}

// ── GetDailyReport ────────────────────────────────────────────────

type GetDailyReportQuery struct {
	ClinicID sharedtypes.ClinicID
	Date     time.Time
}

type GetDailyReportHandler struct {
	repo repository.QuoteRepository
}

func NewGetDailyReportHandler(repo repository.QuoteRepository) *GetDailyReportHandler {
	return &GetDailyReportHandler{repo: repo}
}

func (h *GetDailyReportHandler) Handle(ctx context.Context, q GetDailyReportQuery) (*DailyReportDTO, error) {
	quotes, err := h.repo.FindByClinicAndDate(ctx, q.ClinicID, q.Date)
	if err != nil {
		return nil, err
	}

	report := &DailyReportDTO{
		ClinicID:    q.ClinicID.String(),
		Date:        q.Date.Format("2006-01-02"),
		TotalQuotes: len(quotes),
		Quotes:      make([]QuoteDTO, 0, len(quotes)),
	}

	for _, quote := range quotes {
		report.TotalPaidCents += quote.TotalConfirmedCents()
		report.TotalPendingCents += quote.PendingAmountCents()
		for _, fee := range quote.LateFees() {
			if fee.Status() == "Pending" || fee.Status() == "Paid" {
				report.TotalFeesCents += fee.AmountCents()
			}
		}
		report.Quotes = append(report.Quotes, *toQuoteDTO(quote))
	}

	return report, nil
}

// ── DTO mappers ───────────────────────────────────────────────────

func toQuoteDTO(q *aggregate.Quote) *QuoteDTO {
	dto := &QuoteDTO{
		ID:                  q.ID().String(),
		AppointmentID:       q.AppointmentID().String(),
		PatientID:           q.PatientID().String(),
		ClinicID:            q.ClinicID().String(),
		ProcedureCode:       q.ProcedureCode(),
		ProcedureDesc:       q.ProcedureDesc(),
		Status:              q.Status().String(),
		ArancelCents:        q.ArancelCents(),
		CoveragePercent:     q.CoveragePercent(),
		CoverageAmountCents: q.CoverageAmountCents(),
		CoPayType:           q.CoPayType().String(),
		CoPayAmountCents:    q.CoPayAmountCents(),
		PendingAmountCents:  q.PendingAmountCents(),
		TotalPaidCents:      q.TotalConfirmedCents(),
		CoverageType:        q.CoverageType(),
		RequiresAuthorization: q.RequiresAuthorization(),
		AuthorizationCode:   q.AuthorizationCode(),
		PendingCoverageCheck: q.PendingCoverageCheck(),
		SlotStart:           q.SlotStart().Format(time.RFC3339),
		SlotEnd:             q.SlotEnd().Format(time.RFC3339),
		CreatedAt:           q.CreatedAt().Format(time.RFC3339),
		UpdatedAt:           q.UpdatedAt().Format(time.RFC3339),
		Payments:            make([]PaymentDTO, 0),
		LateFees:            make([]LateFeeDTO, 0),
	}

	if q.AgreementID() != nil {
		s := q.AgreementID().String()
		dto.AgreementID = &s
	}

	for _, p := range q.Payments() {
		pd := PaymentDTO{
			ID:            p.ID().String(),
			AmountCents:   p.AmountCents(),
			PaymentMethod: p.PaymentMethod().String(),
			Status:        p.Status().String(),
			ExternalReference: p.ExternalReference(),
			ReceiptNumber: p.ReceiptNumber(),
			Notes:         p.Notes(),
			CreatedAt:     p.CreatedAt().Format(time.RFC3339),
		}
		if p.PaidAt() != nil {
			s := p.PaidAt().Format(time.RFC3339)
			pd.PaidAt = &s
		}
		dto.Payments = append(dto.Payments, pd)
	}

	for _, f := range q.LateFees() {
		dto.LateFees = append(dto.LateFees, LateFeeDTO{
			ID:           f.ID().String(),
			FeeType:      f.FeeType().String(),
			AmountCents:  f.AmountCents(),
			Status:       f.Status().String(),
			WaivedReason: f.WaivedReason(),
			CreatedAt:    f.CreatedAt().Format(time.RFC3339),
		})
	}

	return dto
}
