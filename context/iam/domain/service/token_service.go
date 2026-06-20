// Package service contiene los Domain Services del bounded context IAM.
// Los domain services encapsulan lógica que no pertenece a un único Aggregate.
package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/iam/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/middleware"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── TokenConfig ───────────────────────────────────────────────────

// TokenConfig contiene la configuración de JWT.
type TokenConfig struct {
	SecretKey       []byte
	Issuer          string
	AccessTokenTTL  time.Duration // típicamente 15 minutos
	RefreshTokenTTL time.Duration // típicamente 30 días
}

// DefaultTokenConfig retorna una configuración razonable para producción.
func DefaultTokenConfig(secretKey []byte, issuer string) TokenConfig {
	return TokenConfig{
		SecretKey:       secretKey,
		Issuer:          issuer,
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
	}
}

// ── TokenService ─────────────────────────────────────────────────

// TokenService es el Domain Service responsable de emitir y validar JWT.
// Se diferencia de la capa de infraestructura porque opera sobre primitivas
// del dominio (UserClaims, User aggregate), no sobre HTTP.
type TokenService struct {
	cfg TokenConfig
}

func NewTokenService(cfg TokenConfig) *TokenService {
	return &TokenService{cfg: cfg}
}

// TokenPair contiene el par access + refresh token emitido en un login.
type TokenPair struct {
	AccessToken        string
	RefreshToken       string
	RefreshTokenHash   string // SHA-256 del refresh token, para almacenar en BD
	AccessTokenExpiry  time.Time
	RefreshTokenExpiry time.Time
}

// IssueTokenPair genera un par de tokens para el usuario dado.
// Registra el refresh token en el aggregate User.
func (s *TokenService) IssueTokenPair(
	user *aggregate.User,
	deviceID string,
	clinicIDs []sharedtypes.ClinicID,
	familyID *sharedtypes.FamilyID,
	isGuardian bool,
) (TokenPair, error) {
	// 1. Generar access token JWT.
	accessExpiry := time.Now().UTC().Add(s.cfg.AccessTokenTTL)
	accessToken, err := s.generateAccessToken(user, clinicIDs, familyID, isGuardian, accessExpiry)
	if err != nil {
		return TokenPair{}, fmt.Errorf("IssueTokenPair: access token: %w", err)
	}

	// 2. Generar refresh token opaco (random bytes → base64).
	refreshTokenPlain, err := generateSecureToken(32)
	if err != nil {
		return TokenPair{}, fmt.Errorf("IssueTokenPair: refresh token: %w", err)
	}

	refreshTokenHash := hashToken(refreshTokenPlain)
	refreshExpiry := time.Now().UTC().Add(s.cfg.RefreshTokenTTL)

	// 3. Registrar el refresh token en el aggregate (dispara lógica de revocación por device).
	if _, err := user.IssueRefreshToken(deviceID, refreshTokenHash, s.cfg.RefreshTokenTTL); err != nil {
		return TokenPair{}, fmt.Errorf("IssueTokenPair: register refresh token: %w", err)
	}

	return TokenPair{
		AccessToken:        accessToken,
		RefreshToken:       refreshTokenPlain,
		RefreshTokenHash:   refreshTokenHash,
		AccessTokenExpiry:  accessExpiry,
		RefreshTokenExpiry: refreshExpiry,
	}, nil
}

// generateAccessToken construye y firma el JWT de acceso.
func (s *TokenService) generateAccessToken(
	user *aggregate.User,
	clinicIDs []sharedtypes.ClinicID,
	familyID *sharedtypes.FamilyID,
	isGuardian bool,
	expiry time.Time,
) (string, error) {
	var patientIDPtr *uuid.UUID
	if user.LinkedType() == "patient" {
		id := user.LinkedID()
		patientIDPtr = id
	}

	clinicIDStrs := make([]uuid.UUID, len(clinicIDs))
	copy(clinicIDStrs, clinicIDs)

	claims := middleware.UserClaims{
		UserID:     user.ID(),
		PatientID:  patientIDPtr,
		Role:       middleware.Role(user.Role()),
		ClinicIDs:  clinicIDStrs,
		FamilyID:   (*uuid.UUID)(familyID),
		IsGuardian: isGuardian,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.cfg.Issuer,
			Subject:   user.ID().String(),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(expiry),
			ID:        uuid.New().String(), // jti: previene replay attacks
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.cfg.SecretKey)
	if err != nil {
		return "", fmt.Errorf("firmar JWT: %w", err)
	}
	return signed, nil
}

// ValidateRefreshAndRotate valida el refresh token y emite un nuevo par (token rotation).
// El token usado queda revocado y se emite uno nuevo.
func (s *TokenService) ValidateRefreshAndRotate(
	user *aggregate.User,
	refreshTokenPlain string,
	deviceID string,
	clinicIDs []sharedtypes.ClinicID,
	familyID *sharedtypes.FamilyID,
	isGuardian bool,
) (TokenPair, error) {
	tokenHash := hashToken(refreshTokenPlain)

	// Validar que existe y está activo.
	if _, err := user.ValidateRefreshToken(tokenHash); err != nil {
		return TokenPair{}, err
	}

	// Revocar el token usado (rotation: un token, un uso).
	if err := user.RevokeRefreshToken(tokenHash); err != nil {
		return TokenPair{}, err
	}

	// Emitir nuevo par.
	return s.IssueTokenPair(user, deviceID, clinicIDs, familyID, isGuardian)
}

// PasswordPolicy contiene las reglas de expiración de password.
type PasswordPolicy struct {
	MaxAgeDays int // 0 = sin expiración
}

// ShouldForcePasswordChange evalúa si el usuario debe cambiar contraseña.
func (s *TokenService) ShouldForcePasswordChange(user *aggregate.User, policy PasswordPolicy) bool {
	if policy.MaxAgeDays == 0 {
		return false
	}
	// Solo roles de staff están sujetos a política de rotación.
	if !valueobject.Role(user.Role()).IsStaff() {
		return false
	}
	limit := time.Now().UTC().AddDate(0, 0, -policy.MaxAgeDays)
	return user.Audit().UpdatedAt.Before(limit)
}

// ── Helpers ───────────────────────────────────────────────────────

// generateSecureToken genera un token aleatorio criptográficamente seguro.
func generateSecureToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generar token seguro: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// hashToken aplica SHA-256 al token en plano para almacenamiento seguro.
func hashToken(plain string) string {
	h := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(h[:])
}
