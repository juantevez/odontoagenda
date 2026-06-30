package event_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/professional/domain/event"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// Verifica que todos los eventos implementan la interfaz DomainEvent
// y retornan los valores correctos.
func TestDomainEventImplementations(t *testing.T) {
	profID := sharedtypes.ProfessionalID(uuid.New())
	now := time.Now()

	events := []event.DomainEvent{
		event.ProfessionalRegistered{ProfessionalID: profID, FullName: "Dr. Test", OccurredAt: now},
		event.ProfessionalLicenseAdded{ProfessionalID: profID, OccurredAt: now},
		event.ProfessionalAssignedToClinic{ProfessionalID: profID, OccurredAt: now},
		event.ProfessionalScheduleUpdated{ProfessionalID: profID, OccurredAt: now},
		event.ProfessionalSuspended{ProfessionalID: profID, OccurredAt: now},
		event.ProfessionalLicenseExpiringSoon{ProfessionalID: profID, OccurredAt: now},
	}

	wantTypes := []string{
		"professional.registered",
		"professional.license.added",
		"professional.assigned_to_clinic",
		"professional.schedule.updated",
		"professional.suspended",
		"professional.license.expiring_soon",
	}

	for i, evt := range events {
		t.Run(wantTypes[i], func(t *testing.T) {
			if got := evt.EventType(); got != wantTypes[i] {
				t.Errorf("EventType() = %q, se esperaba %q", got, wantTypes[i])
			}
			if got := evt.AggregateID(); got != profID.String() {
				t.Errorf("AggregateID() = %q, se esperaba %q", got, profID.String())
			}
			if got := evt.AggregateType(); got != "Professional" {
				t.Errorf("AggregateType() = %q, se esperaba 'Professional'", got)
			}
			if got := evt.BoundedContext(); got != "professional" {
				t.Errorf("BoundedContext() = %q, se esperaba 'professional'", got)
			}
			if got := evt.SchemaVersion(); got != 1 {
				t.Errorf("SchemaVersion() = %d, se esperaba 1", got)
			}
		})
	}
}
