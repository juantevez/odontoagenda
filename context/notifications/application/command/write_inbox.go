package command

import (
	"context"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/notifications/domain/entity"
	"github.com/juantevez/odontoagenda/context/notifications/domain/repository"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
)

// WriteInboxCommand representa la intención de crear una notificación en la bandeja del staff.
type WriteInboxCommand struct {
	Type        valueobject.NotificationType
	ClinicID    *uuid.UUID // nil = visible en todas las sedes
	ReferenceID string
	Title       string
	Body        string
}

// WriteInboxHandler persiste la notificación en la bandeja de entrada.
type WriteInboxHandler struct {
	repo repository.InboxRepository
}

func NewWriteInboxHandler(repo repository.InboxRepository) *WriteInboxHandler {
	return &WriteInboxHandler{repo: repo}
}

func (h *WriteInboxHandler) Handle(ctx context.Context, cmd WriteInboxCommand) error {
	n := entity.NewInboxNotification(cmd.Type, cmd.ClinicID, cmd.ReferenceID, cmd.Title, cmd.Body)
	return h.repo.Save(ctx, n)
}
