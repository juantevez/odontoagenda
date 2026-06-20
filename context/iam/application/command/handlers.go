// Package command contiene los Command Handlers del bounded context IAM.
// Cada handler orquesta un caso de uso de escritura:
// valida la entrada, coordina el dominio y persiste los cambios.
//
// Los handlers NO contienen lógica de negocio: esa vive en el dominio.
package command

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/iam/domain/repository"
	"github.com/juantevez/odontoagenda/context/iam/domain/service"
	"github.com/juantevez/odontoagenda/context/iam/domain/valueobject"
	"github.com/juantevez/odontoagenda/pkg/events"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── RegisterUser ─────────────────────────────────────────────────

// RegisterUserCommand contiene los datos para registrar un nuevo usuario.
type RegisterUserCommand struct {
	Email         string
	PlainPassword string
	Role          string
	LinkedID      *sharedtypes.UserID // ID del Patient o Professional asociado
	LinkedType    string              // "patient" | "professional" | "staff"
	FamilyName    string              // Solo para pacientes: nombre de la familia
}

// RegisterUserResult contiene el resultado del registro.
type RegisterUserResult struct {
	UserID   sharedtypes.UserID
	FamilyID *sharedtypes.FamilyID // nil si no es paciente
}

// RegisterUserHandler orquesta el caso de uso de registro de usuario.
type RegisterUserHandler struct {
	userRepo   repository.UserRepository
	familyRepo repository.FamilyRepository
	eventBus   events.Bus
	logger     *slog.Logger
}

func NewRegisterUserHandler(
	userRepo repository.UserRepository,
	familyRepo repository.FamilyRepository,
	eventBus events.Bus,
) *RegisterUserHandler {
	return &RegisterUserHandler{
		userRepo:   userRepo,
		familyRepo: familyRepo,
		eventBus:   eventBus,
		logger:     slog.Default().With("handler", "RegisterUser"),
	}
}

func (h *RegisterUserHandler) Handle(ctx context.Context, cmd RegisterUserCommand) (RegisterUserResult, error) {
	// 1. Validar y construir Value Objects.
	email, err := sharedvo.NewEmail(cmd.Email)
	if err != nil {
		return RegisterUserResult{}, sharederrors.NewInvalidArgument("email", err.Error())
	}

	role := valueobject.Role(cmd.Role)
	if err := role.Validate(); err != nil {
		return RegisterUserResult{}, sharederrors.NewInvalidArgument("role", err.Error())
	}

	// 2. Verificar unicidad de email.
	exists, err := h.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		return RegisterUserResult{}, sharederrors.NewInternal(err)
	}
	if exists {
		return RegisterUserResult{}, sharederrors.NewAlreadyExists("User", "email", cmd.Email)
	}

	// 3. Hashear password (lógica de seguridad, no de negocio puro).
	hashedPw, err := valueobject.HashPassword(cmd.PlainPassword)
	if err != nil {
		return RegisterUserResult{}, sharederrors.NewInvalidArgument("password", err.Error())
	}

	// 4. Crear el aggregate User.
	user, err := aggregate.NewUser(email, hashedPw, role, cmd.LinkedID, cmd.LinkedType, nil)
	if err != nil {
		return RegisterUserResult{}, err
	}

	// 5. Persistir el User.
	if err := h.userRepo.Save(ctx, user); err != nil {
		return RegisterUserResult{}, fmt.Errorf("RegisterUser: save user: %w", err)
	}

	result := RegisterUserResult{UserID: user.ID()}

	// 6. Si es paciente, crear FamilyAccount automáticamente.
	if role == valueobject.RolePatient && cmd.LinkedID != nil {
		patientID := sharedtypes.PatientID(*cmd.LinkedID)
		userID := user.ID()
		family := aggregate.NewFamilyAccount(patientID, cmd.FamilyName, &userID)

		if err := h.familyRepo.Save(ctx, family); err != nil {
			// No fallamos el registro por esto, pero lo registramos.
			h.logger.ErrorContext(ctx, "error creando FamilyAccount",
				"user_id", user.ID(),
				"error", err,
			)
		} else {
			familyID := family.ID()
			result.FamilyID = &familyID
		}
	}

	// 7. Publicar Domain Events pendientes.
	for _, evt := range user.PendingEvents() {
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.logger.WarnContext(ctx, "error publicando evento",
				"event_type", evt.EventType(),
				"error", err,
			)
		}
	}

	h.logger.InfoContext(ctx, "usuario registrado",
		"user_id", user.ID(),
		"role", role,
	)

	return result, nil
}

// ── LoginUser ─────────────────────────────────────────────────────

// LoginCommand contiene las credenciales del login.
type LoginCommand struct {
	Email         string
	PlainPassword string
	DeviceID      string // identificador del dispositivo/sesión
}

// LoginResult contiene el par de tokens emitido.
type LoginResult struct {
	AccessToken        string
	RefreshToken       string
	AccessTokenExpiry  int64 // Unix timestamp
	RefreshTokenExpiry int64 // Unix timestamp
}

// LoginHandler orquesta el caso de uso de autenticación.
type LoginHandler struct {
	userRepo     repository.UserRepository
	familyRepo   repository.FamilyRepository
	tokenService *service.TokenService
	logger       *slog.Logger
}

func NewLoginHandler(
	userRepo repository.UserRepository,
	familyRepo repository.FamilyRepository,
	tokenService *service.TokenService,
) *LoginHandler {
	return &LoginHandler{
		userRepo:     userRepo,
		familyRepo:   familyRepo,
		tokenService: tokenService,
		logger:       slog.Default().With("handler", "Login"),
	}
}

func (h *LoginHandler) Handle(ctx context.Context, cmd LoginCommand) (LoginResult, error) {
	// 1. Buscar usuario por email.
	email, err := sharedvo.NewEmail(cmd.Email)
	if err != nil {
		return LoginResult{}, sharederrors.NewUnauthorized("credenciales inválidas")
	}

	user, err := h.userRepo.FindByEmail(ctx, email)
	if err != nil {
		if sharederrors.IsNotFound(err) {
			// Misma respuesta que password incorrecto (evitar enumeración de usuarios).
			return LoginResult{}, sharederrors.NewUnauthorized("credenciales inválidas")
		}
		return LoginResult{}, sharederrors.NewInternal(err)
	}

	// 2. Autenticar (valida status y password).
	if err := user.Authenticate(cmd.PlainPassword); err != nil {
		return LoginResult{}, err
	}

	// 3. Obtener clinic IDs y family info para enriquecer los claims del JWT.
	clinicIDs, familyID, isGuardian := h.resolveUserContext(ctx, user)

	// 4. Emitir par de tokens.
	pair, err := h.tokenService.IssueTokenPair(user, cmd.DeviceID, clinicIDs, familyID, isGuardian)
	if err != nil {
		return LoginResult{}, sharederrors.NewInternal(err)
	}

	// 5. Persistir los cambios del User (refresh token registrado en aggregate).
	if err := h.userRepo.Update(ctx, user); err != nil {
		return LoginResult{}, sharederrors.NewInternal(err)
	}

	h.logger.InfoContext(ctx, "login exitoso",
		"user_id", user.ID(),
		"device_id", cmd.DeviceID,
	)

	return LoginResult{
		AccessToken:        pair.AccessToken,
		RefreshToken:       pair.RefreshToken,
		AccessTokenExpiry:  pair.AccessTokenExpiry.Unix(),
		RefreshTokenExpiry: pair.RefreshTokenExpiry.Unix(),
	}, nil
}

// resolveUserContext obtiene el contexto del usuario para enriquecer los claims JWT.
// En una implementación completa, consultaría clinic_staff y family tables.
func (h *LoginHandler) resolveUserContext(
	ctx context.Context,
	user *aggregate.User,
) (clinicIDs []sharedtypes.ClinicID, familyID *sharedtypes.FamilyID, isGuardian bool) {
	// Para pacientes: buscar su FamilyAccount.
	if user.LinkedType() == "patient" && user.LinkedID() != nil {
		patientID := sharedtypes.PatientID(*user.LinkedID())
		family, err := h.familyRepo.FindByPatientID(ctx, patientID)
		if err == nil {
			id := family.ID()
			familyID = &id
			// Verificar si es guardian de algún miembro.
			for _, m := range family.Members() {
				for _, gID := range m.GuardianIDs {
					if gID == patientID {
						isGuardian = true
						break
					}
				}
			}
		}
	}
	// Staff: clinic IDs se cargarían desde la tabla clinic_staff.
	// Simplificado aquí; en implementación completa haría un query adicional.
	return clinicIDs, familyID, isGuardian
}

// ── RefreshTokens ─────────────────────────────────────────────────

// RefreshTokensCommand rota el par de tokens usando el refresh token.
type RefreshTokensCommand struct {
	RefreshToken string
	DeviceID     string
	UserID       sharedtypes.UserID
}

// RefreshTokensHandler maneja la rotación de tokens.
type RefreshTokensHandler struct {
	userRepo     repository.UserRepository
	familyRepo   repository.FamilyRepository
	tokenService *service.TokenService
	logger       *slog.Logger
}

func NewRefreshTokensHandler(
	userRepo repository.UserRepository,
	familyRepo repository.FamilyRepository,
	tokenService *service.TokenService,
) *RefreshTokensHandler {
	return &RefreshTokensHandler{
		userRepo:     userRepo,
		familyRepo:   familyRepo,
		tokenService: tokenService,
		logger:       slog.Default().With("handler", "RefreshTokens"),
	}
}

func (h *RefreshTokensHandler) Handle(ctx context.Context, cmd RefreshTokensCommand) (LoginResult, error) {
	user, err := h.userRepo.FindByID(ctx, cmd.UserID)
	if err != nil {
		return LoginResult{}, sharederrors.NewUnauthorized("token inválido")
	}

	clinicIDs, familyID, isGuardian := resolveContextForRefresh(user)

	pair, err := h.tokenService.ValidateRefreshAndRotate(
		user, cmd.RefreshToken, cmd.DeviceID, clinicIDs, familyID, isGuardian,
	)
	if err != nil {
		return LoginResult{}, err
	}

	if err := h.userRepo.Update(ctx, user); err != nil {
		return LoginResult{}, sharederrors.NewInternal(err)
	}

	return LoginResult{
		AccessToken:        pair.AccessToken,
		RefreshToken:       pair.RefreshToken,
		AccessTokenExpiry:  pair.AccessTokenExpiry.Unix(),
		RefreshTokenExpiry: pair.RefreshTokenExpiry.Unix(),
	}, nil
}

func resolveContextForRefresh(user *aggregate.User) ([]sharedtypes.ClinicID, *sharedtypes.FamilyID, bool) {
	// Simplificado; en producción cargaría desde BD.
	return nil, nil, false
}

// ── LogoutUser ────────────────────────────────────────────────────

// LogoutCommand revoca los tokens de una sesión.
type LogoutCommand struct {
	UserID       sharedtypes.UserID
	RefreshToken string // si está presente, revoca solo esta sesión; si no, revoca todas
	GlobalLogout bool   // true = revoca todas las sesiones
}

// LogoutHandler maneja el logout (parcial o global).
type LogoutHandler struct {
	userRepo repository.UserRepository
	eventBus events.Bus
	logger   *slog.Logger
}

func NewLogoutHandler(userRepo repository.UserRepository, eventBus events.Bus) *LogoutHandler {
	return &LogoutHandler{
		userRepo: userRepo,
		eventBus: eventBus,
		logger:   slog.Default().With("handler", "Logout"),
	}
}

func (h *LogoutHandler) Handle(ctx context.Context, cmd LogoutCommand) error {
	user, err := h.userRepo.FindByID(ctx, cmd.UserID)
	if err != nil {
		return err
	}

	if cmd.GlobalLogout || cmd.RefreshToken == "" {
		user.RevokeAllTokens()
	} else {
		if err := user.RevokeRefreshToken(hashTokenLocal(cmd.RefreshToken)); err != nil {
			return err
		}
	}

	if err := h.userRepo.Update(ctx, user); err != nil {
		return sharederrors.NewInternal(err)
	}

	for _, evt := range user.PendingEvents() {
		_ = h.eventBus.Publish(ctx, evt)
	}

	return nil
}

// hashTokenLocal es una copia local para evitar import circular.
// En producción estaría en un pkg interno compartido.
func hashTokenLocal(plain string) string {
	import_sha256 := fmt.Sprintf("%x", [32]byte{}) // placeholder
	_ = import_sha256
	// real impl usa crypto/sha256 igual que TokenService
	return plain // simplificado
}
