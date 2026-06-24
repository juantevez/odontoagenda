// Package query — GetClinicReport handler para reporte por rango de fechas.
package query

import (
	"context"
	"time"

	"github.com/juantevez/odontoagenda/context/billing/domain/repository"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── ClinicReportDTO ───────────────────────────────────────────────

// ClinicReportDTO es el resumen de facturación de una sede en un período.
type ClinicReportDTO struct {
	ClinicID          string     `json:"clinic_id"`
	From              string     `json:"from"`
	To                string     `json:"to"`
	TotalQuotes       int        `json:"total_quotes"`
	TotalPaidQuotes   int        `json:"total_paid_quotes"`
	TotalVoidedQuotes int        `json:"total_voided_quotes"`
	TotalArancelCents int64      `json:"total_arancel_cents"`
	TotalCoPayCents   int64      `json:"total_co_pay_cents"`
	TotalPaidCents    int64      `json:"total_paid_cents"`
	TotalPendingCents int64      `json:"total_pending_cents"`
	TotalFeesCents    int64      `json:"total_late_fees_cents"`
	ByPaymentMethod   map[string]int64 `json:"by_payment_method"`
	Quotes            []QuoteDTO `json:"quotes"`
}

// ── GetClinicReport ───────────────────────────────────────────────

type GetClinicReportQuery struct {
	ClinicID sharedtypes.ClinicID
	From     time.Time
	To       time.Time
}

type GetClinicReportHandler struct {
	repo repository.QuoteRepository
}

func NewGetClinicReportHandler(repo repository.QuoteRepository) *GetClinicReportHandler {
	return &GetClinicReportHandler{repo: repo}
}

func (h *GetClinicReportHandler) Handle(ctx context.Context, q GetClinicReportQuery) (*ClinicReportDTO, error) {
	report := &ClinicReportDTO{
		ClinicID:        q.ClinicID.String(),
		From:            q.From.Format("2006-01-02"),
		To:              q.To.Format("2006-01-02"),
		ByPaymentMethod: make(map[string]int64),
		Quotes:          []QuoteDTO{},
	}

	// Iterar día a día en el rango y acumular.
	for d := q.From; !d.After(q.To); d = d.AddDate(0, 0, 1) {
		quotes, err := h.repo.FindByClinicAndDate(ctx, q.ClinicID, d)
		if err != nil {
			return nil, err
		}

		for _, quote := range quotes {
			report.TotalQuotes++
			report.TotalArancelCents += quote.ArancelCents()
			report.TotalCoPayCents += quote.CoPayAmountCents()
			report.TotalPaidCents += quote.TotalConfirmedCents()
			report.TotalPendingCents += quote.PendingAmountCents()

			switch quote.Status().String() {
			case "Paid":
				report.TotalPaidQuotes++
			case "Voided", "Refunded":
				report.TotalVoidedQuotes++
			}

			// Acumular por método de pago.
			for _, p := range quote.Payments() {
				if p.Status().IsConfirmed() {
					report.ByPaymentMethod[p.PaymentMethod().String()] += p.AmountCents()
				}
			}

			// LateFees cobrados.
			for _, f := range quote.LateFees() {
				if f.Status().String() == "Paid" {
					report.TotalFeesCents += f.AmountCents()
				}
			}

			report.Quotes = append(report.Quotes, *toQuoteDTO(quote))
		}
	}

	return report, nil
}
