// Package redis contiene los adaptadores de salida Redis del bounded context Coverage.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
)

const (
	coverageCacheTTL = 5 * time.Minute
	cacheKeyPrefix   = "coverage:result"
	//planKeyPrefix    = "coverage:plan"
)

// RedisClient define los métodos Redis necesarios (misma interfaz que en scheduling).
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Keys(ctx context.Context, pattern string) ([]string, error)
}

// CoverageCacheRedis implementa repository.CoverageCache sobre Redis.
type CoverageCacheRedis struct {
	client RedisClient
}

func NewCoverageCacheRedis(client RedisClient) *CoverageCacheRedis {
	return &CoverageCacheRedis{client: client}
}

// GetCoverageResult retorna el CoverageResult cacheado. (nil, nil) = cache miss.
func (c *CoverageCacheRedis) GetCoverageResult(
	ctx context.Context,
	planID uuid.UUID,
	procedureCode string,
) (*valueobject.CoverageResult, error) {
	key := c.resultKey(planID, procedureCode)
	val, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, nil // cache miss
	}
	var result valueobject.CoverageResult
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		return nil, nil // dato corrupto → cache miss
	}
	return &result, nil
}

// SetCoverageResult almacena el CoverageResult con TTL de 5 minutos.
func (c *CoverageCacheRedis) SetCoverageResult(
	ctx context.Context,
	planID uuid.UUID,
	procedureCode string,
	result valueobject.CoverageResult,
) error {
	key := c.resultKey(planID, procedureCode)
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("CoverageCache.Set: marshal: %w", err)
	}
	return c.client.Set(ctx, key, string(data), coverageCacheTTL)
}

// InvalidatePlan invalida todo el cache de un plan al modificar sus reglas.
func (c *CoverageCacheRedis) InvalidatePlan(ctx context.Context, planID uuid.UUID) error {
	pattern := fmt.Sprintf("%s:%s:*", cacheKeyPrefix, planID)
	keys, err := c.client.Keys(ctx, pattern)
	if err != nil {
		return fmt.Errorf("CoverageCache.InvalidatePlan: keys: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}
	return c.client.Del(ctx, keys...)
}

// resultKey genera la clave de cache para un resultado de cobertura.
// Formato: coverage:result:{plan_id}:{procedure_code}
func (c *CoverageCacheRedis) resultKey(planID uuid.UUID, procedureCode string) string {
	return fmt.Sprintf("%s:%s:%s", cacheKeyPrefix, planID, procedureCode)
}
