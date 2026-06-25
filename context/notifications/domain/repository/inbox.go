// Package repository define los puertos de salida del bounded context Notifications.
package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/notifications/domain/entity"
)

// InboxRepository es el puerto de salida para la bandeja de notificaciones del staff.
type InboxRepository interface {
	Save(ctx context.Context, n *entity.InboxNotification) error
	FindByClinic(ctx context.Context, clinicID uuid.UUID, unreadOnly bool, limit int) ([]*entity.InboxNotification, error)
	MarkRead(ctx context.Context, id uuid.UUID) error
	MarkAllRead(ctx context.Context, clinicID uuid.UUID) error
	CountUnread(ctx context.Context, clinicID uuid.UUID) (int, error)
}
