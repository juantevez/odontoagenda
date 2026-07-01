// Package postgres implementa los repositorios de salida del bounded context Notifications.
package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/notifications/domain/entity"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
)

// InboxPostgresRepository implementa repository.InboxRepository sobre PostgreSQL.
type InboxPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewInboxPostgresRepository(pool *pgxpool.Pool) *InboxPostgresRepository {
	return &InboxPostgresRepository{pool: pool}
}

func (r *InboxPostgresRepository) Save(ctx context.Context, n *entity.InboxNotification) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO notifications.inbox (id, type, clinic_id, reference_id, title, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		n.ID, string(n.Type), n.ClinicID, n.ReferenceID, n.Title, n.Body, n.CreatedAt,
	)
	return err
}

func (r *InboxPostgresRepository) FindByClinic(
	ctx context.Context,
	clinicID uuid.UUID,
	unreadOnly bool,
	limit int,
) ([]*entity.InboxNotification, error) {
	q := `
		SELECT id, type, clinic_id, reference_id, title, body, read_at, created_at
		FROM notifications.inbox
		WHERE (clinic_id = $1 OR clinic_id IS NULL)`
	if unreadOnly {
		q += ` AND read_at IS NULL`
	}
	q += ` ORDER BY created_at DESC LIMIT $2`

	rows, err := r.pool.Query(ctx, q, clinicID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*entity.InboxNotification
	for rows.Next() {
		n, err := scanInboxRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	if result == nil {
		result = []*entity.InboxNotification{}
	}
	return result, rows.Err()
}

func (r *InboxPostgresRepository) MarkRead(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notifications.inbox SET read_at = NOW() WHERE id = $1 AND read_at IS NULL`,
		id,
	)
	return err
}

func (r *InboxPostgresRepository) MarkAllRead(ctx context.Context, clinicID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notifications.inbox SET read_at = NOW() WHERE (clinic_id = $1 OR clinic_id IS NULL) AND read_at IS NULL`,
		clinicID,
	)
	return err
}

func (r *InboxPostgresRepository) CountUnread(ctx context.Context, clinicID uuid.UUID) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications.inbox WHERE (clinic_id = $1 OR clinic_id IS NULL) AND read_at IS NULL`,
		clinicID,
	).Scan(&count)
	return count, err
}

// ── scan helpers ──────────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInboxRow(row rowScanner) (*entity.InboxNotification, error) {
	var (
		id          uuid.UUID
		notifType   string
		clinicIDPtr *uuid.UUID
		referenceID string
		title, body string
		readAt      *time.Time
		createdAt   time.Time
	)
	if err := row.Scan(&id, &notifType, &clinicIDPtr, &referenceID, &title, &body, &readAt, &createdAt); err != nil {
		return nil, err
	}
	return &entity.InboxNotification{
		ID:          id,
		Type:        valueobject.NotificationType(notifType),
		ClinicID:    clinicIDPtr,
		ReferenceID: referenceID,
		Title:       title,
		Body:        body,
		ReadAt:      readAt,
		CreatedAt:   createdAt,
	}, nil
}
