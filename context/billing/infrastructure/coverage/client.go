// Package coverage contiene el adaptador HTTP hacia el BC Coverage.
// Implementa el puerto CoverageClient que Billing necesita para
// obtener el CoverageResult al crear un Quote.
package coverage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	billingservice "github.com/juantevez/odontoagenda/context/billing/domain/service"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── CoverageClient — adaptador HTTP ──────────────────────────────

// CoverageClient llama al BC Coverage via HTTP para obtener el
// resultado de cobertura de un paciente para un procedimiento.
//
// En el MVP no tiene circuit breaker ni retry policy sofisticada.
// Si Coverage no responde en el timeout, retorna un CoverageInput
// con IsCovered=false para que Billing aplique el fast-path privado.
type CoverageClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewCoverageClient(baseURL string) *CoverageClient {
	return &CoverageClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// CalculateCoverageRequest agrupa los parámetros necesarios para la consulta.
type CalculateCoverageRequest struct {
	AgreementID     string
	PlanID          string
	ProcedureCode   string
	PatientID       sharedtypes.PatientID
	PatientAge      int
	AppointmentDate time.Time
	VisitsThisYear  int
}

// coverageAPIResponse es el DTO de la respuesta de Coverage BC.
type coverageAPIResponse struct {
	IsCovered             bool   `json:"is_covered"`
	CoveragePercent       int    `json:"coverage_percent"`
	CoPayType             string `json:"co_pay_type"`
	CoPayValue            int    `json:"co_pay_value"`
	RequiresAuthorization bool   `json:"requires_authorization"`
	RejectionReason       string `json:"rejection_reason,omitempty"`
}

// CalculateCoverage consulta Coverage BC para obtener el CoverageResult.
// Si la llamada falla o Coverage no está disponible, retorna el fallback
// de pago privado (IsCovered=false) para que Billing no quede bloqueado.
func (c *CoverageClient) CalculateCoverage(
	ctx context.Context,
	req CalculateCoverageRequest,
) (billingservice.CoverageInput, error) {
	// Si no hay agreementID o planID, el paciente es Privado directamente.
	if req.AgreementID == "" || req.PlanID == "" {
		return billingservice.CoverageInput{IsCovered: false}, nil
	}

	params := url.Values{}
	params.Set("agreement_id", req.AgreementID)
	params.Set("plan_id", req.PlanID)
	params.Set("procedure_code", req.ProcedureCode)
	params.Set("patient_id", req.PatientID.String())
	params.Set("patient_age", fmt.Sprintf("%d", req.PatientAge))
	params.Set("appointment_date", req.AppointmentDate.Format("2006-01-02"))
	params.Set("visits_this_year", fmt.Sprintf("%d", req.VisitsThisYear))

	endpoint := fmt.Sprintf("%s/api/v1/coverage/calculate?%s", c.baseURL, params.Encode())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return privateFallback(), fmt.Errorf("CoverageClient: build request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// Coverage no disponible: fallback a pago privado.
		return privateFallback(), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Coverage retornó error: fallback a pago privado.
		return privateFallback(), nil
	}

	var apiResp coverageAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return privateFallback(), fmt.Errorf("CoverageClient: decode response: %w", err)
	}

	return billingservice.CoverageInput{
		IsCovered:             apiResp.IsCovered,
		CoveragePercent:       apiResp.CoveragePercent,
		CoPayType:             apiResp.CoPayType,
		CoPayValue:            apiResp.CoPayValue,
		RequiresAuthorization: apiResp.RequiresAuthorization,
	}, nil
}

// privateFallback retorna el CoverageInput de pago privado.
// Se usa cuando Coverage BC no está disponible.
func privateFallback() billingservice.CoverageInput {
	return billingservice.CoverageInput{
		IsCovered:             false,
		CoveragePercent:       0,
		CoPayType:             "None",
		CoPayValue:            0,
		RequiresAuthorization: false,
	}
}
