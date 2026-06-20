// Package service contiene los Domain Services del bounded context Professional.
package service

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/professional/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── ScheduleConflictChecker — Domain Service ──────────────────────

// ScheduleConflictChecker valida que los horarios de las distintas sedes
// de un profesional no se solapen entre sí.
//
// Es Domain Service porque coordina información de múltiples ClinicAssignments
// del mismo Professional, lógica que no pertenece a ninguna Entity en particular.
//
// Regla de negocio:
//
//	Un profesional no puede estar en dos sedes al mismo tiempo.
//	Los horarios semanales de sedes distintas no pueden tener solapamiento
//	en el mismo día de la semana.
type ScheduleConflictChecker struct{}

func NewScheduleConflictChecker() *ScheduleConflictChecker {
	return &ScheduleConflictChecker{}
}

// ConflictResult describe un solapamiento detectado.
type ConflictResult struct {
	ConflictingClinicID sharedtypes.ClinicID
	ConflictingDay      valueobject.Weekday
	// Descripción legible del conflicto para el usuario.
	Description string
}

// CheckNewAssignment verifica que un nuevo horario propuesto para una sede
// no solapa con ninguna asignación activa existente del profesional.
//
// Debe llamarse ANTES de invocar professional.AssignToClinic().
func (s *ScheduleConflictChecker) CheckNewAssignment(
	professional *aggregate.Professional,
	newClinicID sharedtypes.ClinicID,
	proposedSchedule []valueobject.DaySchedule,
) ([]ConflictResult, error) {
	conflicts := make([]ConflictResult, 0)

	for _, existing := range professional.ClinicAssignments() {
		// Solo comparar con asignaciones activas a otras sedes.
		if !existing.Status().IsActive() || existing.ClinicID() == newClinicID {
			continue
		}

		for _, newDay := range proposedSchedule {
			for _, existingDay := range existing.WeeklySchedule() {
				if newDay.Weekday != existingDay.Weekday {
					continue
				}
				if schedulesOverlap(newDay, existingDay) {
					conflicts = append(conflicts, ConflictResult{
						ConflictingClinicID: existing.ClinicID(),
						ConflictingDay:      newDay.Weekday,
						Description: fmt.Sprintf(
							"%s: horario propuesto %02d:%02d-%02d:%02d solapa con sede '%s' (%02d:%02d-%02d:%02d)",
							newDay.Weekday,
							newDay.StartHour, newDay.StartMin,
							newDay.EndHour, newDay.EndMin,
							existing.ClinicID(),
							existingDay.StartHour, existingDay.StartMin,
							existingDay.EndHour, existingDay.EndMin,
						),
					})
				}
			}
		}
	}

	return conflicts, nil
}

// CheckScheduleUpdate verifica que un horario actualizado para una asignación
// existente no solapa con el resto de asignaciones del profesional.
func (s *ScheduleConflictChecker) CheckScheduleUpdate(
	professional *aggregate.Professional,
	assignmentID uuid.UUID,
	newSchedule []valueobject.DaySchedule,
) ([]ConflictResult, error) {
	// Identificar la sede que se está actualizando.
	var targetClinicID sharedtypes.ClinicID
	found := false
	for _, a := range professional.ClinicAssignments() {
		if a.ID() == assignmentID {
			targetClinicID = a.ClinicID()
			found = true
			break
		}
	}
	if !found {
		return nil, sharederrors.NewNotFound("ClinicAssignment", assignmentID.String())
	}

	return s.CheckNewAssignment(professional, targetClinicID, newSchedule)
}

// CheckSpecificDateTime verifica si el profesional tiene conflicto de horario
// en un datetime específico entre todas sus sedes activas.
// Retorna la ClinicID que está ocupando ese slot, si existe.
func (s *ScheduleConflictChecker) CheckSpecificDateTime(
	professional *aggregate.Professional,
	excludeClinicID sharedtypes.ClinicID,
	at time.Time,
) (conflictingClinicID *sharedtypes.ClinicID, hasConflict bool) {
	for _, assignment := range professional.ClinicAssignments() {
		if !assignment.Status().IsActive() {
			continue
		}
		if assignment.ClinicID() == excludeClinicID {
			continue
		}
		if assignment.IsAvailableAt(at) {
			id := assignment.ClinicID()
			return &id, true
		}
	}
	return nil, false
}

// ── LicenseExpirationChecker — Domain Service ─────────────────────

// LicenseExpirationChecker evalúa el estado de vencimiento de las matrículas.
// Usado tanto por la validación en tiempo real como por el job scheduler.
type LicenseExpirationChecker struct{}

func NewLicenseExpirationChecker() *LicenseExpirationChecker {
	return &LicenseExpirationChecker{}
}

// ExpirationStatus describe el estado de vencimiento de una matrícula.
type ExpirationStatus string

const (
	ExpirationStatusValid    ExpirationStatus = "Valid"
	ExpirationStatusExpiring ExpirationStatus = "ExpiringSoon" // vence en ≤ 30 días
	ExpirationStatusExpired  ExpirationStatus = "Expired"
	ExpirationStatusNoExpiry ExpirationStatus = "NoExpiry" // sin fecha de vencimiento
)

// LicenseExpirationReport describe el estado de vencimiento de una matrícula.
type LicenseExpirationReport struct {
	LicenseID     uuid.UUID
	SpecialtyCode string
	LicenseNumber string
	Status        ExpirationStatus
	DaysRemaining int // negativo si ya venció
}

// Evaluate evalúa el estado de vencimiento de todas las licencias de un profesional.
func (c *LicenseExpirationChecker) Evaluate(
	professional *aggregate.Professional,
) []LicenseExpirationReport {
	now := time.Now().UTC()
	reports := make([]LicenseExpirationReport, 0, len(professional.Licenses()))

	for _, license := range professional.Licenses() {
		report := LicenseExpirationReport{
			LicenseID:     license.ID(),
			SpecialtyCode: string(license.Specialty().Code),
			LicenseNumber: license.LicenseNumber(),
		}

		if license.ExpiresAt() == nil {
			report.Status = ExpirationStatusNoExpiry
		} else {
			days := int(license.ExpiresAt().Sub(now).Hours() / 24)
			report.DaysRemaining = days

			switch {
			case days < 0:
				report.Status = ExpirationStatusExpired
			case days <= 30:
				report.Status = ExpirationStatusExpiring
			default:
				report.Status = ExpirationStatusValid
			}
		}
		reports = append(reports, report)
	}
	return reports
}

// ── Helpers internos ──────────────────────────────────────────────

// schedulesOverlap verifica si dos DaySchedule del mismo día de la semana se solapan.
// Algoritmo: A solapa B si A.start < B.end && B.start < A.end
func schedulesOverlap(a, b valueobject.DaySchedule) bool {
	aStart := a.StartHour*60 + a.StartMin
	aEnd := a.EndHour*60 + a.EndMin
	bStart := b.StartHour*60 + b.StartMin
	bEnd := b.EndHour*60 + b.EndMin
	return aStart < bEnd && bStart < aEnd
}
