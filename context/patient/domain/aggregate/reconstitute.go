// Package aggregate — funciones de reconstitución desde persistencia
// para MedicalAlert y DentalHistorySummary.
// Archivo separado para no modificar patient.go.
package aggregate

import (
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ReconstituteMedicalAlert reconstruye una MedicalAlert desde persistencia.
// Permite al repositorio Postgres reconstruir el estado sin disparar lógica de dominio.
func ReconstituteMedicalAlert(
	id uuid.UUID,
	alertType valueobject.AlertType,
	severity valueobject.AlertSeverity,
	description string,
	isSelfReported bool,
	isActive bool,
	createdBy uuid.UUID,
	createdAt time.Time,
	revokedAt *time.Time,
	revokedBy *uuid.UUID,
) MedicalAlert {
	return MedicalAlert{
		id:             id,
		alertType:      alertType,
		severity:       severity,
		description:    description,
		isSelfReported: isSelfReported,
		isActive:       isActive,
		createdBy:      createdBy,
		createdAt:      createdAt,
		revokedAt:      revokedAt,
		revokedBy:      revokedBy,
	}
}

// ReconstituteDentalHistory reconstruye un DentalHistorySummary desde persistencia.
func ReconstituteDentalHistory(
	id uuid.UUID,
	patientID sharedtypes.PatientID,
	lastVisitDate *time.Time,
	riskLevel valueobject.RiskLevel,
	visitCount int,
	treatments []TreatmentSummary,
	updatedAt time.Time,
	updatedByEvent string,
) *DentalHistorySummary {
	return &DentalHistorySummary{
		id:             id,
		patientID:      patientID,
		lastVisitDate:  lastVisitDate,
		riskLevel:      riskLevel,
		mainTreatments: treatments,
		visitCount:     visitCount,
		updatedAt:      updatedAt,
		updatedByEvent: updatedByEvent,
	}
}

// UpdatedByEvent expone el campo updatedByEvent para el repositorio.
func (d *DentalHistorySummary) UpdatedByEvent() string {
	return d.updatedByEvent
}
