// Package http — handler de reportes y exportación CSV (Fase 6).
package http

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	billingqry "github.com/juantevez/odontoagenda/context/billing/application/query"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── ReportHandler ─────────────────────────────────────────────────

type ReportHandler struct {
	getDailyReport  *billingqry.GetDailyReportHandler
	getClinicReport *billingqry.GetClinicReportHandler
	logger          *slog.Logger
}

func NewReportHandler(
	getDailyReport *billingqry.GetDailyReportHandler,
	getClinicReport *billingqry.GetClinicReportHandler,
) *ReportHandler {
	return &ReportHandler{
		getDailyReport:  getDailyReport,
		getClinicReport: getClinicReport,
		logger:          slog.Default().With("adapter", "billing.report.http"),
	}
}

// ── GET /billing/reports/daily ────────────────────────────────────
// Reporte de caja del día para una sede.
// ?clinic_id=<uuid>&date=YYYY-MM-DD&format=json|csv

func (h *ReportHandler) GetDailyReport(w http.ResponseWriter, r *http.Request) {
	clinicID, err := uuid.Parse(r.URL.Query().Get("clinic_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "clinic_id inválido")
		return
	}

	date := time.Now().UTC()
	if d := r.URL.Query().Get("date"); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			date = t
		}
	}

	report, err := h.getDailyReport.Handle(r.Context(), billingqry.GetDailyReportQuery{
		ClinicID: sharedtypes.ClinicID(clinicID),
		Date:     date,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		h.writeDailyReportCSV(w, report)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// ── GET /billing/reports/clinic/:clinicId ─────────────────────────
// Reporte de facturación de una sede en un rango de fechas.
// ?from=YYYY-MM-DD&to=YYYY-MM-DD&format=json|csv

func (h *ReportHandler) GetClinicReport(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil || !claims.HasRole(middleware.RoleClinicAdmin, middleware.RoleSuperAdmin) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "acceso denegado")
		return
	}

	clinicID, err := uuid.Parse(r.URL.Query().Get("clinic_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "clinic_id inválido")
		return
	}

	from := time.Now().UTC().AddDate(0, -1, 0) // último mes por defecto
	to := time.Now().UTC()

	if f := r.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse("2006-01-02", f); err == nil {
			from = t
		}
	}
	if t := r.URL.Query().Get("to"); t != "" {
		if parsed, err := time.Parse("2006-01-02", t); err == nil {
			to = parsed
		}
	}

	// Limitar rango a 90 días para evitar queries muy costosas.
	if to.Sub(from) > 90*24*time.Hour {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT",
			"el rango máximo de fechas es 90 días")
		return
	}

	report, err := h.getClinicReport.Handle(r.Context(), billingqry.GetClinicReportQuery{
		ClinicID: sharedtypes.ClinicID(clinicID),
		From:     from,
		To:       to,
	})
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		h.writeClinicReportCSV(w, report)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// ── CSV writers ───────────────────────────────────────────────────

// writeDailyReportCSV serializa el reporte diario como CSV descargable.
// Formato compatible con Excel argentino (separador punto y coma, decimales con coma).
func (h *ReportHandler) writeDailyReportCSV(w http.ResponseWriter, report *billingqry.DailyReportDTO) {
	filename := fmt.Sprintf("caja_%s_%s.csv", report.ClinicID[:8], report.Date)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	// BOM UTF-8 para que Excel lo abra correctamente.
	w.Write([]byte("\xEF\xBB\xBF"))

	writer := csv.NewWriter(w)
	writer.Comma = ';' // separador para Excel en locale AR

	// Encabezados.
	writer.Write([]string{
		"Fecha",
		"ID Turno",
		"Paciente ID",
		"Procedimiento",
		"Estado Presupuesto",
		"Arancel ($)",
		"Cobertura (%)",
		"Monto Cobertura ($)",
		"Copago ($)",
		"Pagado ($)",
		"Pendiente ($)",
		"Método Pago",
		"Cargo Tardío ($)",
		"Tipo Cobertura",
	})

	for _, q := range report.Quotes {
		slotStart := ""
		if t, err := time.Parse(time.RFC3339, q.SlotStart); err == nil {
			slotStart = t.Format("02/01/2006 15:04")
		}

		paymentMethod := ""
		totalPaid := int64(0)
		for _, p := range q.Payments {
			if p.Status == "Confirmed" {
				paymentMethod = p.PaymentMethod
				totalPaid += p.AmountCents
			}
		}

		totalLateFee := int64(0)
		for _, f := range q.LateFees {
			if f.Status == "Pending" || f.Status == "Paid" {
				totalLateFee += f.AmountCents
			}
		}

		writer.Write([]string{
			slotStart,
			q.AppointmentID,
			q.PatientID,
			q.ProcedureDesc,
			q.Status,
			centsToARS(q.ArancelCents),
			strconv.Itoa(q.CoveragePercent),
			centsToARS(q.CoverageAmountCents),
			centsToARS(q.CoPayAmountCents),
			centsToARS(totalPaid),
			centsToARS(q.PendingAmountCents),
			paymentMethod,
			centsToARS(totalLateFee),
			q.CoverageType,
		})
	}

	// Fila de totales.
	writer.Write([]string{})
	writer.Write([]string{
		"TOTALES", "", "", "", "",
		"", "", "",
		"",
		centsToARS(report.TotalPaidCents),
		centsToARS(report.TotalPendingCents),
		"", centsToARS(report.TotalFeesCents), "",
	})

	writer.Flush()
}

// writeClinicReportCSV serializa el reporte por rango de fechas como CSV.
func (h *ReportHandler) writeClinicReportCSV(w http.ResponseWriter, report *billingqry.ClinicReportDTO) {
	filename := fmt.Sprintf("facturacion_%s_%s_%s.csv",
		report.ClinicID[:8], report.From, report.To)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	w.Write([]byte("\xEF\xBB\xBF"))

	writer := csv.NewWriter(w)
	writer.Comma = ';'

	writer.Write([]string{
		"Fecha",
		"ID Turno",
		"Paciente ID",
		"Procedimiento",
		"Estado",
		"Arancel ($)",
		"Cobertura (%)",
		"Copago ($)",
		"Pagado ($)",
		"Pendiente ($)",
		"Tipo Cobertura",
		"Método Pago",
	})

	for _, q := range report.Quotes {
		slotStart := ""
		if t, err := time.Parse(time.RFC3339, q.SlotStart); err == nil {
			slotStart = t.Format("02/01/2006 15:04")
		}

		paymentMethod := ""
		for _, p := range q.Payments {
			if p.Status == "Confirmed" {
				paymentMethod = p.PaymentMethod
				break
			}
		}

		writer.Write([]string{
			slotStart,
			q.AppointmentID,
			q.PatientID,
			q.ProcedureDesc,
			q.Status,
			centsToARS(q.ArancelCents),
			strconv.Itoa(q.CoveragePercent),
			centsToARS(q.CoPayAmountCents),
			centsToARS(q.TotalPaidCents),
			centsToARS(q.PendingAmountCents),
			q.CoverageType,
			paymentMethod,
		})
	}

	writer.Write([]string{})
	writer.Write([]string{
		"TOTALES", "", "", "", "",
		centsToARS(report.TotalArancelCents),
		"",
		centsToARS(report.TotalCoPayCents),
		centsToARS(report.TotalPaidCents),
		centsToARS(report.TotalPendingCents),
		"", "",
	})

	writer.Flush()
}

// centsToARS convierte centavos a string con formato argentino "1.234,56".
func centsToARS(cents int64) string {
	pesos := cents / 100
	centavos := cents % 100
	return fmt.Sprintf("%d,%02d", pesos, centavos)
}


