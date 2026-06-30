package service_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/iam/domain/service"
	"github.com/juantevez/odontoagenda/context/iam/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── helpers ──────────────────────────────────────────────────────

const (
	testSecret = "test-secret-key-0123456789abcdef"
	testIssuer = "odontoagenda-test"
	validPw    = "Sup3rSecret"
)

func defaultCfg() service.TokenConfig {
	return service.DefaultTokenConfig([]byte(testSecret), testIssuer)
}

func newSvc(cfg service.TokenConfig) *service.TokenService {
	return service.NewTokenService(cfg)
}

// newActiveUser crea un User activo con password hasheado para usar en tests.
func newActiveUser(t *testing.T, role valueobject.Role, linkedID *uuid.UUID, linkedType string) *aggregate.User {
	t.Helper()
	em, err := sharedvo.NewEmail("test@example.com")
	if err != nil {
		t.Fatalf("setup: email: %v", err)
	}
	hp, err := valueobject.HashPassword(validPw)
	if err != nil {
		t.Fatalf("setup: hash password: %v", err)
	}
	user, err := aggregate.NewUser(em, hp, role, linkedID, linkedType, nil)
	if err != nil {
		t.Fatalf("setup: NewUser: %v", err)
	}
	user.PendingEvents()
	return user
}

// newUserWithOldPassword reconstruye un User cuyo updatedAt es daysOld días en el pasado.
// Permite testear la política de expiración de password.
func newUserWithOldPassword(t *testing.T, role valueobject.Role, daysOld int) *aggregate.User {
	t.Helper()
	em, err := sharedvo.NewEmail("oldpw@example.com")
	if err != nil {
		t.Fatalf("setup: email: %v", err)
	}
	hp, err := valueobject.HashPassword(validPw)
	if err != nil {
		t.Fatalf("setup: hash password: %v", err)
	}
	past := time.Now().Add(-time.Duration(daysOld) * 24 * time.Hour)
	audit := sharedtypes.AuditInfo{CreatedAt: past, UpdatedAt: past}
	return aggregate.Reconstitute(uuid.New(), em, hp, role, valueobject.StatusActive, nil, "", nil, audit, 1)
}

// parseAccessToken verifica firma y extrae los claims del access token.
func parseAccessToken(t *testing.T, tokenStr string) *middleware.UserClaims {
	t.Helper()
	claims := &middleware.UserClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(_ *jwt.Token) (any, error) {
		return []byte(testSecret), nil
	}, jwt.WithIssuer(testIssuer), jwt.WithExpirationRequired())
	if err != nil {
		t.Fatalf("parseAccessToken: %v", err)
	}
	return claims
}

// ── DefaultTokenConfig ───────────────────────────────────────────

func TestDefaultTokenConfig(t *testing.T) {
	cfg := service.DefaultTokenConfig([]byte(testSecret), testIssuer)

	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Errorf("AccessTokenTTL = %v, se esperaba 15m", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 30*24*time.Hour {
		t.Errorf("RefreshTokenTTL = %v, se esperaba 30d", cfg.RefreshTokenTTL)
	}
	if cfg.Issuer != testIssuer {
		t.Errorf("Issuer = %q, se esperaba %q", cfg.Issuer, testIssuer)
	}
	if string(cfg.SecretKey) != testSecret {
		t.Error("SecretKey no coincide")
	}
}

// ── IssueTokenPair ───────────────────────────────────────────────

func TestIssueTokenPair(t *testing.T) {
	t.Run("emite access token y refresh token no vacíos", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		svc := newSvc(defaultCfg())

		pair, err := svc.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("IssueTokenPair() error = %v", err)
		}
		if pair.AccessToken == "" {
			t.Error("AccessToken vacío")
		}
		if pair.RefreshToken == "" {
			t.Error("RefreshToken vacío")
		}
		if pair.RefreshTokenHash == "" {
			t.Error("RefreshTokenHash vacío")
		}
	})

	t.Run("las expiraciones quedan en el futuro", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		svc := newSvc(defaultCfg())
		before := time.Now()

		pair, err := svc.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("IssueTokenPair() error = %v", err)
		}
		if !pair.AccessTokenExpiry.After(before) {
			t.Errorf("AccessTokenExpiry %v no está en el futuro", pair.AccessTokenExpiry)
		}
		if !pair.RefreshTokenExpiry.After(before) {
			t.Errorf("RefreshTokenExpiry %v no está en el futuro", pair.RefreshTokenExpiry)
		}
		if !pair.RefreshTokenExpiry.After(pair.AccessTokenExpiry) {
			t.Error("RefreshTokenExpiry debería ser posterior a AccessTokenExpiry")
		}
	})

	t.Run("registra el refresh token en el aggregate User", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		svc := newSvc(defaultCfg())

		if _, err := svc.IssueTokenPair(user, "device-1", nil, nil, false); err != nil {
			t.Fatalf("IssueTokenPair() error = %v", err)
		}

		if len(user.RefreshTokens()) != 1 {
			t.Errorf("RefreshTokens count = %d, se esperaba 1", len(user.RefreshTokens()))
		}
		rt := user.RefreshTokens()[0]
		if rt.DeviceID != "device-1" {
			t.Errorf("DeviceID = %q, se esperaba 'device-1'", rt.DeviceID)
		}
		if !rt.IsValid() {
			t.Error("se esperaba que el RefreshToken fuera válido")
		}
	})

	t.Run("dos llamadas con el mismo device reemplaza el refresh token anterior", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		svc := newSvc(defaultCfg())

		pair1, err := svc.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("primera llamada: %v", err)
		}
		pair2, err := svc.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("segunda llamada: %v", err)
		}

		// Debe haber 2 entradas en el slice (la vieja revocada + la nueva activa).
		tokens := user.RefreshTokens()
		if len(tokens) != 2 {
			t.Fatalf("RefreshTokens count = %d, se esperaban 2", len(tokens))
		}
		// El token de la primera llamada debe estar revocado.
		var foundFirstRevoked bool
		for _, rt := range tokens {
			if rt.TokenHash == pair1.RefreshTokenHash && rt.RevokedAt != nil {
				foundFirstRevoked = true
			}
		}
		if !foundFirstRevoked {
			t.Error("el primer refresh token debería estar revocado")
		}
		// El par 2 debe ser diferente al par 1.
		if pair1.RefreshToken == pair2.RefreshToken {
			t.Error("se esperaban refresh tokens distintos en cada llamada")
		}
	})

	t.Run("dos llamadas con distintos devices emite tokens independientes", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		svc := newSvc(defaultCfg())

		if _, err := svc.IssueTokenPair(user, "device-1", nil, nil, false); err != nil {
			t.Fatalf("device-1: %v", err)
		}
		if _, err := svc.IssueTokenPair(user, "device-2", nil, nil, false); err != nil {
			t.Fatalf("device-2: %v", err)
		}

		var active int
		for _, rt := range user.RefreshTokens() {
			if rt.IsValid() {
				active++
			}
		}
		if active != 2 {
			t.Errorf("active tokens = %d, se esperaban 2 (uno por device)", active)
		}
	})

	t.Run("el access token es un JWT válido y firmado con la clave correcta", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		svc := newSvc(defaultCfg())

		pair, err := svc.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("IssueTokenPair() error = %v", err)
		}

		claims := parseAccessToken(t, pair.AccessToken)
		if claims.UserID != user.ID() {
			t.Errorf("claims.UserID = %v, se esperaba %v", claims.UserID, user.ID())
		}
		if claims.Issuer != testIssuer {
			t.Errorf("claims.Issuer = %q, se esperaba %q", claims.Issuer, testIssuer)
		}
		if string(claims.Role) != string(valueobject.RoleProfessional) {
			t.Errorf("claims.Role = %q, se esperaba %q", claims.Role, valueobject.RoleProfessional)
		}
	})

	t.Run("propaga PatientID en los claims del JWT para usuarios paciente", func(t *testing.T) {
		linkedID := uuid.New()
		user := newActiveUser(t, valueobject.RolePatient, &linkedID, "patient")
		svc := newSvc(defaultCfg())

		pair, err := svc.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("IssueTokenPair() error = %v", err)
		}

		claims := parseAccessToken(t, pair.AccessToken)
		if claims.PatientID == nil {
			t.Fatal("claims.PatientID = nil, se esperaba el linkedID del paciente")
		}
		if *claims.PatientID != linkedID {
			t.Errorf("claims.PatientID = %v, se esperaba %v", *claims.PatientID, linkedID)
		}
	})

	t.Run("propaga FamilyID e IsGuardian en los claims del JWT", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RolePatient, nil, "")
		svc := newSvc(defaultCfg())
		familyID := sharedtypes.FamilyID(uuid.New())

		pair, err := svc.IssueTokenPair(user, "device-1", nil, &familyID, true)
		if err != nil {
			t.Fatalf("IssueTokenPair() error = %v", err)
		}

		claims := parseAccessToken(t, pair.AccessToken)
		if claims.FamilyID == nil {
			t.Fatal("claims.FamilyID = nil, se esperaba el FamilyID")
		}
		if uuid.UUID(*claims.FamilyID) != uuid.UUID(familyID) {
			t.Errorf("claims.FamilyID = %v, se esperaba %v", *claims.FamilyID, familyID)
		}
		if !claims.IsGuardian {
			t.Error("claims.IsGuardian = false, se esperaba true")
		}
	})

	t.Run("propaga ClinicIDs en los claims del JWT para staff", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		svc := newSvc(defaultCfg())
		clinic1, clinic2 := uuid.New(), uuid.New()
		clinicIDs := []sharedtypes.ClinicID{clinic1, clinic2}

		pair, err := svc.IssueTokenPair(user, "device-1", clinicIDs, nil, false)
		if err != nil {
			t.Fatalf("IssueTokenPair() error = %v", err)
		}

		claims := parseAccessToken(t, pair.AccessToken)
		if len(claims.ClinicIDs) != 2 {
			t.Fatalf("len(claims.ClinicIDs) = %d, se esperaban 2", len(claims.ClinicIDs))
		}
	})

	t.Run("falla si el usuario está suspendido", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		if err := user.Suspend("test", uuid.New()); err != nil {
			t.Fatalf("setup: Suspend: %v", err)
		}
		svc := newSvc(defaultCfg())

		_, err := svc.IssueTokenPair(user, "device-1", nil, nil, false)
		if err == nil {
			t.Fatal("se esperaba un error para usuario suspendido")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})
}

// ── ValidateRefreshAndRotate ──────────────────────────────────────

func TestValidateRefreshAndRotate(t *testing.T) {
	t.Run("rota el par de tokens con un refresh token válido", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		svc := newSvc(defaultCfg())

		pair1, err := svc.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("setup: IssueTokenPair: %v", err)
		}

		pair2, err := svc.ValidateRefreshAndRotate(user, pair1.RefreshToken, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("ValidateRefreshAndRotate() error = %v", err)
		}
		if pair2.RefreshToken == pair1.RefreshToken {
			t.Error("el nuevo refresh token debe ser diferente al anterior")
		}
		if pair2.AccessToken == "" {
			t.Error("AccessToken vacío tras la rotación")
		}
	})

	t.Run("el token anterior queda revocado tras la rotación", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		svc := newSvc(defaultCfg())

		pair1, _ := svc.IssueTokenPair(user, "device-1", nil, nil, false)
		if _, err := svc.ValidateRefreshAndRotate(user, pair1.RefreshToken, "device-1", nil, nil, false); err != nil {
			t.Fatalf("ValidateRefreshAndRotate() error = %v", err)
		}

		// Intentar rotar con el token ya usado debe fallar.
		_, err := svc.ValidateRefreshAndRotate(user, pair1.RefreshToken, "device-1", nil, nil, false)
		if err == nil {
			t.Fatal("se esperaba error al reusar un token ya rotado")
		}
		if code := domainCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("rechaza un refresh token con texto plano incorrecto", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")
		svc := newSvc(defaultCfg())
		if _, err := svc.IssueTokenPair(user, "device-1", nil, nil, false); err != nil {
			t.Fatalf("setup: %v", err)
		}

		_, err := svc.ValidateRefreshAndRotate(user, "token-incorrecto", "device-1", nil, nil, false)
		if err == nil {
			t.Fatal("se esperaba error con token incorrecto")
		}
		if code := domainCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("rechaza un refresh token expirado", func(t *testing.T) {
		user := newActiveUser(t, valueobject.RoleProfessional, nil, "")

		shortCfg := defaultCfg()
		shortCfg.RefreshTokenTTL = 10 * time.Millisecond
		svc := newSvc(shortCfg)

		pair, err := svc.IssueTokenPair(user, "device-1", nil, nil, false)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}

		time.Sleep(20 * time.Millisecond)

		_, err = svc.ValidateRefreshAndRotate(user, pair.RefreshToken, "device-1", nil, nil, false)
		if err == nil {
			t.Fatal("se esperaba error con token expirado")
		}
		if code := domainCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})
}

// ── ShouldForcePasswordChange ─────────────────────────────────────

func TestShouldForcePasswordChange(t *testing.T) {
	svc := newSvc(defaultCfg())

	t.Run("MaxAgeDays=0 nunca fuerza el cambio", func(t *testing.T) {
		user := newUserWithOldPassword(t, valueobject.RoleProfessional, 365)
		policy := service.PasswordPolicy{MaxAgeDays: 0}

		if svc.ShouldForcePasswordChange(user, policy) {
			t.Error("ShouldForcePasswordChange() = true, se esperaba false con MaxAgeDays=0")
		}
	})

	t.Run("staff con password expirado devuelve true", func(t *testing.T) {
		user := newUserWithOldPassword(t, valueobject.RoleProfessional, 91)
		policy := service.PasswordPolicy{MaxAgeDays: 90}

		if !svc.ShouldForcePasswordChange(user, policy) {
			t.Error("ShouldForcePasswordChange() = false, se esperaba true para password de 91 días")
		}
	})

	t.Run("staff con password reciente devuelve false", func(t *testing.T) {
		user := newUserWithOldPassword(t, valueobject.RoleProfessional, 30)
		policy := service.PasswordPolicy{MaxAgeDays: 90}

		if svc.ShouldForcePasswordChange(user, policy) {
			t.Error("ShouldForcePasswordChange() = true, se esperaba false para password de 30 días")
		}
	})

	t.Run("paciente nunca fuerza el cambio, independientemente de la antigüedad", func(t *testing.T) {
		user := newUserWithOldPassword(t, valueobject.RolePatient, 365)
		policy := service.PasswordPolicy{MaxAgeDays: 30}

		if svc.ShouldForcePasswordChange(user, policy) {
			t.Error("ShouldForcePasswordChange() = true, se esperaba false para paciente")
		}
	})

	t.Run("superadmin es staff y también está sujeto a la política", func(t *testing.T) {
		user := newUserWithOldPassword(t, valueobject.RoleSuperAdmin, 91)
		policy := service.PasswordPolicy{MaxAgeDays: 90}

		if !svc.ShouldForcePasswordChange(user, policy) {
			t.Error("ShouldForcePasswordChange() = false, se esperaba true para superadmin con password expirado")
		}
	})

	t.Run("recepcionista con password en el límite exacto no fuerza el cambio", func(t *testing.T) {
		// El límite es: updatedAt < now - maxAgeDays. Si son exactamente 90 días
		// la comparación es Before(limit) donde limit = now - 90d.
		// Con 89 días de antigüedad, updatedAt > limit → false.
		user := newUserWithOldPassword(t, valueobject.RoleReceptionist, 89)
		policy := service.PasswordPolicy{MaxAgeDays: 90}

		if svc.ShouldForcePasswordChange(user, policy) {
			t.Error("ShouldForcePasswordChange() = true, se esperaba false con 89 días de antigüedad")
		}
	})
}

// ── helper ───────────────────────────────────────────────────────

func domainCode(t *testing.T, err error) sharederrors.ErrorCode {
	t.Helper()
	de, ok := sharederrors.As(err)
	if !ok {
		t.Fatalf("se esperaba *sharederrors.DomainError, se obtuvo: %T (%v)", err, err)
	}
	return de.Code
}
