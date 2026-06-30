// White-box: package postgres para acceder a las funciones unexported
// marshalRefreshTokens, unmarshalRefreshTokens e isUniqueViolation.
//
// NO se testean los métodos Save/Update/FindByID/etc. porque requieren
// una conexión real a PostgreSQL. Esos pertenecen a tests de integración
// (ej. con testcontainers).
package postgres

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
)

// ── helpers ──────────────────────────────────────────────────────

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("setup: parse time %q: %v", s, err)
	}
	return ts
}

// ── marshalRefreshTokens ──────────────────────────────────────────

func TestMarshalRefreshTokens(t *testing.T) {
	t.Run("slice vacío produce JSON de array vacío", func(t *testing.T) {
		data, err := marshalRefreshTokens(nil)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		var arr []any
		if err := json.Unmarshal(data, &arr); err != nil {
			t.Fatalf("JSON inválido: %v", err)
		}
		if len(arr) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(arr))
		}
	})

	t.Run("token activo (sin RevokedAt) se serializa correctamente", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		token := aggregate.RefreshToken{
			TokenID:   uuid.New(),
			DeviceID:  "device-1",
			TokenHash: "sha256hash",
			IssuedAt:  now,
			ExpiresAt: now.Add(30 * 24 * time.Hour),
		}

		data, err := marshalRefreshTokens([]aggregate.RefreshToken{token})
		if err != nil {
			t.Fatalf("error = %v", err)
		}

		var dtos []refreshTokenJSON
		if err := json.Unmarshal(data, &dtos); err != nil {
			t.Fatalf("JSON inválido: %v", err)
		}
		if len(dtos) != 1 {
			t.Fatalf("len = %d, se esperaba 1", len(dtos))
		}
		dto := dtos[0]
		if dto.TokenID != token.TokenID.String() {
			t.Errorf("TokenID = %q, se esperaba %q", dto.TokenID, token.TokenID.String())
		}
		if dto.DeviceID != "device-1" {
			t.Errorf("DeviceID = %q", dto.DeviceID)
		}
		if dto.TokenHash != "sha256hash" {
			t.Errorf("TokenHash = %q", dto.TokenHash)
		}
		if dto.RevokedAt != nil {
			t.Errorf("RevokedAt debe ser nil para token activo, got %v", *dto.RevokedAt)
		}
		// Verificar formato RFC3339.
		if _, err := time.Parse(time.RFC3339, dto.ExpiresAt); err != nil {
			t.Errorf("ExpiresAt %q no es RFC3339: %v", dto.ExpiresAt, err)
		}
	})

	t.Run("token revocado incluye revoked_at en formato RFC3339", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		revoked := now.Add(-time.Hour)
		token := aggregate.RefreshToken{
			TokenID:   uuid.New(),
			DeviceID:  "device-2",
			TokenHash: "hash2",
			IssuedAt:  now.Add(-2 * time.Hour),
			ExpiresAt: now.Add(time.Hour),
			RevokedAt: &revoked,
		}

		data, err := marshalRefreshTokens([]aggregate.RefreshToken{token})
		if err != nil {
			t.Fatalf("error = %v", err)
		}

		var dtos []refreshTokenJSON
		_ = json.Unmarshal(data, &dtos)

		if dtos[0].RevokedAt == nil {
			t.Fatal("RevokedAt debería estar presente para token revocado")
		}
		parsed, err := time.Parse(time.RFC3339, *dtos[0].RevokedAt)
		if err != nil {
			t.Fatalf("revoked_at %q no es RFC3339: %v", *dtos[0].RevokedAt, err)
		}
		if !parsed.Equal(revoked) {
			t.Errorf("revoked_at parseado = %v, se esperaba %v", parsed, revoked)
		}
	})

	t.Run("múltiples tokens preservan el orden", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		tokens := []aggregate.RefreshToken{
			{TokenID: uuid.New(), DeviceID: "d1", TokenHash: "h1", IssuedAt: now, ExpiresAt: now.Add(time.Hour)},
			{TokenID: uuid.New(), DeviceID: "d2", TokenHash: "h2", IssuedAt: now, ExpiresAt: now.Add(time.Hour)},
		}

		data, err := marshalRefreshTokens(tokens)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		var dtos []refreshTokenJSON
		_ = json.Unmarshal(data, &dtos)

		if dtos[0].DeviceID != "d1" || dtos[1].DeviceID != "d2" {
			t.Error("orden de tokens no preservado")
		}
	})
}

// ── unmarshalRefreshTokens ────────────────────────────────────────

func TestUnmarshalRefreshTokens(t *testing.T) {
	t.Run("nil retorna slice vacío sin error", func(t *testing.T) {
		tokens, err := unmarshalRefreshTokens(nil)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(tokens) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(tokens))
		}
	})

	t.Run("cadena 'null' retorna slice vacío sin error", func(t *testing.T) {
		tokens, err := unmarshalRefreshTokens([]byte("null"))
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(tokens) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(tokens))
		}
	})

	t.Run("round-trip marshal→unmarshal preserva todos los campos", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		revoked := now.Add(-30 * time.Minute)
		original := []aggregate.RefreshToken{
			{
				TokenID:   uuid.New(),
				DeviceID:  "mobile-app",
				TokenHash: "abc123hash",
				IssuedAt:  now.Add(-time.Hour),
				ExpiresAt: now.Add(29 * 24 * time.Hour),
			},
			{
				TokenID:   uuid.New(),
				DeviceID:  "web-browser",
				TokenHash: "def456hash",
				IssuedAt:  now.Add(-2 * time.Hour),
				ExpiresAt: now.Add(28 * 24 * time.Hour),
				RevokedAt: &revoked,
			},
		}

		data, err := marshalRefreshTokens(original)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		restored, err := unmarshalRefreshTokens(data)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(restored) != len(original) {
			t.Fatalf("len = %d, se esperaba %d", len(restored), len(original))
		}

		for i, got := range restored {
			want := original[i]
			if got.TokenID != want.TokenID {
				t.Errorf("[%d] TokenID = %v, se esperaba %v", i, got.TokenID, want.TokenID)
			}
			if got.DeviceID != want.DeviceID {
				t.Errorf("[%d] DeviceID = %q", i, got.DeviceID)
			}
			if got.TokenHash != want.TokenHash {
				t.Errorf("[%d] TokenHash = %q", i, got.TokenHash)
			}
			if !got.ExpiresAt.Equal(want.ExpiresAt) {
				t.Errorf("[%d] ExpiresAt = %v, se esperaba %v", i, got.ExpiresAt, want.ExpiresAt)
			}
			if !got.IssuedAt.Equal(want.IssuedAt) {
				t.Errorf("[%d] IssuedAt = %v, se esperaba %v", i, got.IssuedAt, want.IssuedAt)
			}
			if (got.RevokedAt == nil) != (want.RevokedAt == nil) {
				t.Errorf("[%d] RevokedAt nil mismatch", i)
			} else if got.RevokedAt != nil && !got.RevokedAt.Equal(*want.RevokedAt) {
				t.Errorf("[%d] RevokedAt = %v, se esperaba %v", i, *got.RevokedAt, *want.RevokedAt)
			}
		}
	})

	t.Run("error si el JSON es inválido", func(t *testing.T) {
		_, err := unmarshalRefreshTokens([]byte("{not valid json"))
		if err == nil {
			t.Fatal("se esperaba error")
		}
	})

	t.Run("error si token_id no es UUID válido", func(t *testing.T) {
		data := []byte(`[{"token_id":"not-a-uuid","device_id":"d","token_hash":"h","expires_at":"2025-01-01T00:00:00Z","issued_at":"2024-01-01T00:00:00Z"}]`)
		_, err := unmarshalRefreshTokens(data)
		if err == nil {
			t.Fatal("se esperaba error por token_id inválido")
		}
	})

	t.Run("error si expires_at no tiene formato RFC3339", func(t *testing.T) {
		data := []byte(fmt.Sprintf(
			`[{"token_id":%q,"device_id":"d","token_hash":"h","expires_at":"01/01/2025","issued_at":"2024-01-01T00:00:00Z"}]`,
			uuid.New().String(),
		))
		_, err := unmarshalRefreshTokens(data)
		if err == nil {
			t.Fatal("se esperaba error por expires_at inválido")
		}
	})

	t.Run("error si issued_at no tiene formato RFC3339", func(t *testing.T) {
		data := []byte(fmt.Sprintf(
			`[{"token_id":%q,"device_id":"d","token_hash":"h","expires_at":"2025-01-01T00:00:00Z","issued_at":"not-a-date"}]`,
			uuid.New().String(),
		))
		_, err := unmarshalRefreshTokens(data)
		if err == nil {
			t.Fatal("se esperaba error por issued_at inválido")
		}
	})

	t.Run("error si revoked_at no tiene formato RFC3339", func(t *testing.T) {
		badDate := "01-01-2025"
		data := []byte(fmt.Sprintf(
			`[{"token_id":%q,"device_id":"d","token_hash":"h","expires_at":"2025-01-01T00:00:00Z","issued_at":"2024-01-01T00:00:00Z","revoked_at":%q}]`,
			uuid.New().String(), badDate,
		))
		_, err := unmarshalRefreshTokens(data)
		if err == nil {
			t.Fatal("se esperaba error por revoked_at inválido")
		}
	})

	t.Run("acepta token con revoked_at en nil (campo omitido)", func(t *testing.T) {
		data := []byte(fmt.Sprintf(
			`[{"token_id":%q,"device_id":"d","token_hash":"h","expires_at":"2025-01-01T00:00:00Z","issued_at":"2024-01-01T00:00:00Z"}]`,
			uuid.New().String(),
		))
		tokens, err := unmarshalRefreshTokens(data)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if tokens[0].RevokedAt != nil {
			t.Error("RevokedAt debería ser nil cuando el campo está ausente")
		}
	})

	t.Run("los timestamps se recuperan en UTC", func(t *testing.T) {
		now := mustParseTime(t, "2025-06-15T12:00:00Z")
		data := []byte(fmt.Sprintf(
			`[{"token_id":%q,"device_id":"d","token_hash":"h","expires_at":"2025-07-15T12:00:00Z","issued_at":"2025-06-15T12:00:00Z"}]`,
			uuid.New().String(),
		))
		tokens, err := unmarshalRefreshTokens(data)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if tokens[0].IssuedAt.Location() != time.UTC {
			t.Error("IssuedAt debería estar en UTC")
		}
		if !tokens[0].IssuedAt.Equal(now) {
			t.Errorf("IssuedAt = %v, se esperaba %v", tokens[0].IssuedAt, now)
		}
	})
}

// ── isUniqueViolation ─────────────────────────────────────────────

func TestIsUniqueViolation(t *testing.T) {
	t.Run("retorna true para PgError con código 23505", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23505"}
		if !isUniqueViolation(pgErr) {
			t.Error("isUniqueViolation() = false, se esperaba true para SQLSTATE 23505")
		}
	})

	t.Run("retorna false para PgError con otro código", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23000"} // integrity constraint violation genérico
		if isUniqueViolation(pgErr) {
			t.Error("isUniqueViolation() = true, se esperaba false para código 23000")
		}
	})

	t.Run("retorna true si el PgError está envuelto en otro error", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23505"}
		wrapped := fmt.Errorf("operación fallida: %w", pgErr)
		if !isUniqueViolation(wrapped) {
			t.Error("isUniqueViolation() = false, errors.As debería desenvolver el PgError")
		}
	})

	t.Run("retorna false para errores que no son PgError", func(t *testing.T) {
		if isUniqueViolation(errors.New("connection refused")) {
			t.Error("isUniqueViolation() = true para error genérico")
		}
	})

	t.Run("retorna false para nil", func(t *testing.T) {
		if isUniqueViolation(nil) {
			t.Error("isUniqueViolation(nil) = true")
		}
	})
}
