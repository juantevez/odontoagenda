// Package redis contiene los adaptadores de salida Redis del bounded context Scheduling.
// Implementa AvailabilityCache (puerto) usando Redis como backing store.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

const (
	slotsCacheTTL  = 5 * time.Minute
	cacheKeyPrefix = "scheduling:availability"
	lockKeyPrefix  = "scheduling:slot_lock"
)

// ── RedisClient — interfaz mínima para testabilidad ───────────────

// RedisClient define los métodos Redis que necesita el adaptador.
// Permite inyectar implementaciones reales (go-redis) o mocks en tests.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Keys(ctx context.Context, pattern string) ([]string, error)
	// SetNX: Set if Not eXists — usado para locks atómicos.
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	// GetDel: Get + Delete en una sola operación — para release de locks.
	GetDel(ctx context.Context, key string) (string, error)
}

// ── AvailabilityCacheRedis ────────────────────────────────────────

// AvailabilityCacheRedis implementa repository.AvailabilityCache sobre Redis.
type AvailabilityCacheRedis struct {
	client RedisClient
}

func NewAvailabilityCacheRedis(client RedisClient) *AvailabilityCacheRedis {
	return &AvailabilityCacheRedis{client: client}
}

// ── GetSlots ─────────────────────────────────────────────────────

// GetSlots busca slots cacheados. Retorna (nil, nil) si no hay cache (miss).
func (c *AvailabilityCacheRedis) GetSlots(
	ctx context.Context,
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	date time.Time,
	procedureCode string,
) ([]aggregate.FreeSlot, error) {
	key := c.slotsKey(professionalID, clinicID, date, procedureCode)

	val, err := c.client.Get(ctx, key)
	if err != nil {
		// Cache miss: retornar nil sin error.
		return nil, nil
	}

	var slots []aggregate.FreeSlot
	if err := json.Unmarshal([]byte(val), &slots); err != nil {
		// Dato corrupto en cache: tratarlo como miss.
		return nil, nil
	}
	return slots, nil
}

// ── SetSlots ─────────────────────────────────────────────────────

// SetSlots almacena slots en cache con TTL de 5 minutos.
func (c *AvailabilityCacheRedis) SetSlots(
	ctx context.Context,
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	date time.Time,
	procedureCode string,
	slots []aggregate.FreeSlot,
) error {
	key := c.slotsKey(professionalID, clinicID, date, procedureCode)

	data, err := json.Marshal(slots)
	if err != nil {
		return fmt.Errorf("AvailabilityCache.SetSlots: marshal: %w", err)
	}

	return c.client.Set(ctx, key, string(data), slotsCacheTTL)
}

// ── InvalidateSchedule ────────────────────────────────────────────

// InvalidateSchedule elimina todas las entradas de cache de un (Professional, Clinic).
// Llamado al recibir ProfessionalScheduleUpdated o al actualizar un BookedSlot.
func (c *AvailabilityCacheRedis) InvalidateSchedule(
	ctx context.Context,
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
) error {
	// Patrón de keys para este professional+clinic: scheduling:availability:{prof}:{clinic}:*
	pattern := fmt.Sprintf("%s:%s:%s:*", cacheKeyPrefix, professionalID, clinicID)

	keys, err := c.client.Keys(ctx, pattern)
	if err != nil {
		return fmt.Errorf("AvailabilityCache.InvalidateSchedule: keys lookup: %w", err)
	}

	if len(keys) == 0 {
		return nil
	}

	return c.client.Del(ctx, keys...)
}

// ── AcquireSlotLock ───────────────────────────────────────────────

// AcquireSlotLock intenta adquirir un lock atómico sobre un slot específico.
// Usa SET NX (Set if Not eXists) que es atómico en Redis.
// Retorna (true, nil) si el lock fue adquirido con éxito.
// Retorna (false, nil) si el slot ya está siendo procesado por otro proceso.
func (c *AvailabilityCacheRedis) AcquireSlotLock(
	ctx context.Context,
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	slotStart time.Time,
	ttl time.Duration,
) (bool, error) {
	key := c.lockKey(professionalID, clinicID, slotStart)
	// El valor es un token único por proceso — usado para release seguro.
	token := fmt.Sprintf("lock:%d", time.Now().UnixNano())

	acquired, err := c.client.SetNX(ctx, key, token, ttl)
	if err != nil {
		return false, fmt.Errorf("AvailabilityCache.AcquireSlotLock: %w", err)
	}
	return acquired, nil
}

// ── ReleaseSlotLock ───────────────────────────────────────────────

// ReleaseSlotLock libera el lock de un slot.
// Se llama en el defer de la saga para garantizar que el lock se libere
// incluso si la operación falla.
func (c *AvailabilityCacheRedis) ReleaseSlotLock(
	ctx context.Context,
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	slotStart time.Time,
) error {
	key := c.lockKey(professionalID, clinicID, slotStart)
	// GetDel es atómico: evita que otro proceso haya adquirido el lock justo antes.
	_, err := c.client.GetDel(ctx, key)
	if err != nil {
		// Si no existe la clave (ya expiró o nunca existió), no es error.
		return nil
	}
	return nil
}

// ── Key builders ──────────────────────────────────────────────────

// slotsKey genera la clave de cache para los slots libres.
// Formato: scheduling:availability:{professional_id}:{clinic_id}:{date}:{procedure_code}
func (c *AvailabilityCacheRedis) slotsKey(
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	date time.Time,
	procedureCode string,
) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s",
		cacheKeyPrefix,
		professionalID.String(),
		clinicID.String(),
		date.Format("2006-01-02"),
		procedureCode,
	)
}

// lockKey genera la clave de lock para un slot específico.
// Formato: scheduling:slot_lock:{professional_id}:{clinic_id}:{slot_start_unix}
func (c *AvailabilityCacheRedis) lockKey(
	professionalID sharedtypes.ProfessionalID,
	clinicID sharedtypes.ClinicID,
	slotStart time.Time,
) string {
	return fmt.Sprintf("%s:%s:%s:%d",
		lockKeyPrefix,
		professionalID.String(),
		clinicID.String(),
		slotStart.Unix(),
	)
}
