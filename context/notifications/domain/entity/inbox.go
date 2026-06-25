// Package entity define los agregados y entidades del bounded context Notifications.
package entity

import (
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
)

// InboxNotification es una notificación persistida en la bandeja del staff.
type InboxNotification struct {
	ID          uuid.UUID
	Type        valueobject.NotificationType
	ClinicID    *uuid.UUID // nil = visible en todas las sedes
	ReferenceID string     // appointment_id, license_id, etc.
	Title       string
	Body        string
	ReadAt      *time.Time
	CreatedAt   time.Time
}

// NewInboxNotification construye una notificación nueva (no leída).
func NewInboxNotification(
	notifType valueobject.NotificationType,
	clinicID *uuid.UUID,
	referenceID, title, body string,
) *InboxNotification {
	return &InboxNotification{
		ID:          uuid.New(),
		Type:        notifType,
		ClinicID:    clinicID,
		ReferenceID: referenceID,
		Title:       title,
		Body:        body,
		CreatedAt:   time.Now().UTC(),
	}
}

func (n *InboxNotification) IsRead() bool { return n.ReadAt != nil }
