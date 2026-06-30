package aggregate_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/iam/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── helpers ──────────────────────────────────────────────────────

func mustEmail(t *testing.T, raw string) sharedvo.Email {
	t.Helper()
	e, err := sharedvo.NewEmail(raw)
	if err != nil {
		t.Fatalf("setup: email: %v", err)
	}
	return e
}

func mustHash(t *testing.T, plain string) valueobject.HashedPassword {
	t.Helper()
	h, err := valueobject.HashPassword(plain)
	if err != nil {
		t.Fatalf("setup: hash password: %v", err)
	}
	return h
}

// newProfessional crea un User activo de rol profesional.
func newProfessional(t *testing.T) *aggregate.User {
	t.Helper()
	user, err := aggregate.NewUser(
		mustEmail(t, "prof@example.com"),
		mustHash(t, "Sup3rSecret"),
		valueobject.RoleProfessional,
		nil, "", nil,
	)
	if err != nil {
		t.Fatalf("setup: NewUser: %v", err)
	}
	user.PendingEvents()
	return user
}

func domainCode(t *testing.T, err error) sharederrors.ErrorCode {
	t.Helper()
	de, ok := sharederrors.As(err)
	if !ok {
		t.Fatalf("se esperaba *DomainError, se obtuvo %T: %v", err, err)
	}
	return de.Code
}

// ── RefreshToken.IsValid ──────────────────────────────────────────

func TestRefreshTokenIsValid(t *testing.T) {
	now := time.Now().UTC()

	t.Run("token activo es válido", func(t *testing.T) {
		rt := aggregate.RefreshToken{
			TokenID:   uuid.New(),
			TokenHash: "h",
			DeviceID:  "d",
			IssuedAt:  now,
			ExpiresAt: now.Add(time.Hour),
		}
		if !rt.IsValid() {
			t.Error("IsValid() = false, se esperaba true")
		}
	})

	t.Run("token revocado no es válido", func(t *testing.T) {
		revoked := now
		rt := aggregate.RefreshToken{
			TokenID:   uuid.New(),
			TokenHash: "h",
			DeviceID:  "d",
			IssuedAt:  now,
			ExpiresAt: now.Add(time.Hour),
			RevokedAt: &revoked,
		}
		if rt.IsValid() {
			t.Error("IsValid() = true, se esperaba false para token revocado")
		}
	})

	t.Run("token expirado no es válido", func(t *testing.T) {
		rt := aggregate.RefreshToken{
			TokenID:   uuid.New(),
			TokenHash: "h",
			DeviceID:  "d",
			IssuedAt:  now.Add(-2 * time.Hour),
			ExpiresAt: now.Add(-time.Second),
		}
		if rt.IsValid() {
			t.Error("IsValid() = true, se esperaba false para token expirado")
		}
	})
}

// ── NewUser ───────────────────────────────────────────────────────

func TestNewUser(t *testing.T) {
	t.Run("crea usuario con los campos correctos", func(t *testing.T) {
		em := mustEmail(t, "nuevo@example.com")
		hp := mustHash(t, "Sup3rSecret")
		linkedID := uuid.New()

		user, err := aggregate.NewUser(em, hp, valueobject.RolePatient, &linkedID, "patient", nil)
		if err != nil {
			t.Fatalf("NewUser() error = %v", err)
		}

		if user.ID() == (sharedtypes.UserID{}) {
			t.Error("ID vacío")
		}
		if user.Email() != em {
			t.Errorf("Email = %v, se esperaba %v", user.Email(), em)
		}
		if user.Role() != valueobject.RolePatient {
			t.Errorf("Role = %v, se esperaba %v", user.Role(), valueobject.RolePatient)
		}
		if user.Status() != valueobject.StatusActive {
			t.Errorf("Status = %v, se esperaba %v", user.Status(), valueobject.StatusActive)
		}
		if user.LinkedID() == nil || *user.LinkedID() != linkedID {
			t.Errorf("LinkedID = %v, se esperaba %v", user.LinkedID(), linkedID)
		}
		if user.LinkedType() != "patient" {
			t.Errorf("LinkedType = %q, se esperaba 'patient'", user.LinkedType())
		}
		if user.Version() != 1 {
			t.Errorf("Version = %d, se esperaba 1", user.Version())
		}
	})

	t.Run("agrega evento UserRegistered a pendingEvents", func(t *testing.T) {
		user, _ := aggregate.NewUser(
			mustEmail(t, "ev@example.com"),
			mustHash(t, "Sup3rSecret"),
			valueobject.RoleProfessional, nil, "", nil,
		)
		evts := user.PendingEvents()
		if len(evts) != 1 {
			t.Fatalf("len(pendingEvents) = %d, se esperaba 1", len(evts))
		}
		if evts[0].EventType() != "user.registered" {
			t.Errorf("EventType = %q, se esperaba 'user.registered'", evts[0].EventType())
		}
	})

	t.Run("PendingEvents limpia el slice tras leerlos", func(t *testing.T) {
		user, _ := aggregate.NewUser(
			mustEmail(t, "clear@example.com"),
			mustHash(t, "Sup3rSecret"),
			valueobject.RoleProfessional, nil, "", nil,
		)
		user.PendingEvents()
		if evts := user.PendingEvents(); len(evts) != 0 {
			t.Errorf("segunda llamada debería retornar slice vacío, obtuvo %d eventos", len(evts))
		}
	})

	t.Run("rechaza rol inválido", func(t *testing.T) {
		_, err := aggregate.NewUser(
			mustEmail(t, "bad@example.com"),
			mustHash(t, "Sup3rSecret"),
			valueobject.Role("rol-inexistente"), nil, "", nil,
		)
		if err == nil {
			t.Fatal("se esperaba error con rol inválido")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInvalidArgument)
		}
	})
}

// ── Reconstitute ──────────────────────────────────────────────────

func TestReconstitute(t *testing.T) {
	t.Run("reconstruye el aggregate sin eventos pendientes", func(t *testing.T) {
		id := uuid.New()
		em := mustEmail(t, "rec@example.com")
		hp := mustHash(t, "Sup3rSecret")

		user := aggregate.Reconstitute(
			id, em, hp,
			valueobject.RoleProfessional, valueobject.StatusSuspended,
			nil, "",
			nil,
			sharedtypes.NewAuditInfo(nil),
			5,
		)

		if user.ID() != id {
			t.Errorf("ID = %v, se esperaba %v", user.ID(), id)
		}
		if user.Status() != valueobject.StatusSuspended {
			t.Errorf("Status = %v, se esperaba Suspended", user.Status())
		}
		if user.Version() != 5 {
			t.Errorf("Version = %d, se esperaba 5", user.Version())
		}
		if evts := user.PendingEvents(); len(evts) != 0 {
			t.Errorf("Reconstitute no debe generar eventos, obtuvo %d", len(evts))
		}
	})
}

// ── Authenticate ──────────────────────────────────────────────────

func TestAuthenticate(t *testing.T) {
	t.Run("password correcto en usuario activo", func(t *testing.T) {
		user := newProfessional(t)
		if err := user.Authenticate("Sup3rSecret"); err != nil {
			t.Errorf("Authenticate() error = %v, se esperaba nil", err)
		}
	})

	t.Run("password incorrecto devuelve ErrUnauthorized", func(t *testing.T) {
		user := newProfessional(t)
		err := user.Authenticate("WrongPw1")
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("usuario suspendido devuelve ErrPrecondition", func(t *testing.T) {
		user := newProfessional(t)
		_ = user.Suspend("test", uuid.New())
		user.PendingEvents()

		err := user.Authenticate("Sup3rSecret")
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})
}

// ── IssueRefreshToken ─────────────────────────────────────────────

func TestIssueRefreshToken(t *testing.T) {
	t.Run("emite token y lo registra en el aggregate", func(t *testing.T) {
		user := newProfessional(t)

		rt, err := user.IssueRefreshToken("device-1", "hash-abc", time.Hour)
		if err != nil {
			t.Fatalf("IssueRefreshToken() error = %v", err)
		}
		if rt.DeviceID != "device-1" {
			t.Errorf("DeviceID = %q, se esperaba 'device-1'", rt.DeviceID)
		}
		if rt.TokenHash != "hash-abc" {
			t.Errorf("TokenHash = %q, se esperaba 'hash-abc'", rt.TokenHash)
		}
		if !rt.IsValid() {
			t.Error("el token recién emitido debería ser válido")
		}
		if len(user.RefreshTokens()) != 1 {
			t.Errorf("RefreshTokens count = %d, se esperaba 1", len(user.RefreshTokens()))
		}
	})

	t.Run("segunda emisión en el mismo device revoca el token anterior", func(t *testing.T) {
		user := newProfessional(t)
		_, _ = user.IssueRefreshToken("device-1", "hash-1", time.Hour)
		_, _ = user.IssueRefreshToken("device-1", "hash-2", time.Hour)

		tokens := user.RefreshTokens()
		if len(tokens) != 2 {
			t.Fatalf("len = %d, se esperaban 2", len(tokens))
		}
		var revoked, active int
		for _, rt := range tokens {
			if rt.RevokedAt != nil {
				revoked++
			} else {
				active++
			}
		}
		if revoked != 1 || active != 1 {
			t.Errorf("revocados=%d activos=%d, se esperaba 1 y 1", revoked, active)
		}
	})

	t.Run("usuario suspendido devuelve ErrPrecondition", func(t *testing.T) {
		user := newProfessional(t)
		_ = user.Suspend("test", uuid.New())
		user.PendingEvents()

		_, err := user.IssueRefreshToken("device-1", "hash", time.Hour)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})
}

// ── ValidateRefreshToken ──────────────────────────────────────────

func TestValidateRefreshToken(t *testing.T) {
	t.Run("valida un token activo correctamente", func(t *testing.T) {
		user := newProfessional(t)
		_, _ = user.IssueRefreshToken("d", "hash-ok", time.Hour)

		rt, err := user.ValidateRefreshToken("hash-ok")
		if err != nil {
			t.Fatalf("ValidateRefreshToken() error = %v", err)
		}
		if rt.TokenHash != "hash-ok" {
			t.Errorf("TokenHash = %q, se esperaba 'hash-ok'", rt.TokenHash)
		}
	})

	t.Run("devuelve ErrUnauthorized para hash desconocido", func(t *testing.T) {
		user := newProfessional(t)
		_, _ = user.IssueRefreshToken("d", "hash-ok", time.Hour)

		_, err := user.ValidateRefreshToken("hash-inexistente")
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("devuelve ErrUnauthorized para token revocado", func(t *testing.T) {
		user := newProfessional(t)
		_, _ = user.IssueRefreshToken("d", "hash-revocado", time.Hour)
		_ = user.RevokeRefreshToken("hash-revocado")

		_, err := user.ValidateRefreshToken("hash-revocado")
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})

	t.Run("devuelve ErrUnauthorized para token expirado", func(t *testing.T) {
		user := newProfessional(t)
		// TTL negativo → expira en el pasado inmediatamente.
		_, _ = user.IssueRefreshToken("d", "hash-exp", -time.Second)

		_, err := user.ValidateRefreshToken("hash-exp")
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrUnauthorized {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrUnauthorized)
		}
	})
}

// ── RevokeRefreshToken ────────────────────────────────────────────

func TestRevokeRefreshToken(t *testing.T) {
	t.Run("revoca el token correspondiente", func(t *testing.T) {
		user := newProfessional(t)
		_, _ = user.IssueRefreshToken("d", "hash-ok", time.Hour)

		if err := user.RevokeRefreshToken("hash-ok"); err != nil {
			t.Fatalf("RevokeRefreshToken() error = %v", err)
		}
		tokens := user.RefreshTokens()
		if tokens[0].RevokedAt == nil {
			t.Error("se esperaba RevokedAt != nil")
		}
	})

	t.Run("devuelve ErrNotFound para hash inexistente", func(t *testing.T) {
		user := newProfessional(t)

		err := user.RevokeRefreshToken("no-existe")
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrNotFound)
		}
	})
}

// ── RevokeAllTokens ───────────────────────────────────────────────

func TestRevokeAllTokens(t *testing.T) {
	t.Run("marca todos los tokens activos como revocados", func(t *testing.T) {
		user := newProfessional(t)
		_, _ = user.IssueRefreshToken("d1", "h1", time.Hour)
		_, _ = user.IssueRefreshToken("d2", "h2", time.Hour)

		user.RevokeAllTokens()

		for _, rt := range user.RefreshTokens() {
			if rt.RevokedAt == nil {
				t.Errorf("token del device %q debería estar revocado", rt.DeviceID)
			}
		}
	})

	t.Run("agrega evento UserLoggedOut", func(t *testing.T) {
		user := newProfessional(t)
		user.RevokeAllTokens()

		evts := user.PendingEvents()
		if len(evts) == 0 {
			t.Fatal("se esperaba al menos un evento")
		}
		if evts[0].EventType() != "user.logged_out" {
			t.Errorf("EventType = %q, se esperaba 'user.logged_out'", evts[0].EventType())
		}
	})
}

// ── ChangePassword ────────────────────────────────────────────────

func TestChangePassword(t *testing.T) {
	t.Run("actualiza el password y revoca todos los tokens", func(t *testing.T) {
		user := newProfessional(t)
		_, _ = user.IssueRefreshToken("d", "h", time.Hour)

		newHash := mustHash(t, "Nuev4Clave")
		user.ChangePassword(newHash)

		// El nuevo password debe funcionar en Authenticate.
		if err := user.Authenticate("Nuev4Clave"); err != nil {
			t.Errorf("Authenticate con nuevo password error = %v", err)
		}
		// Los tokens deben estar revocados.
		for _, rt := range user.RefreshTokens() {
			if rt.RevokedAt == nil {
				t.Error("se esperaba que los tokens quedaran revocados tras ChangePassword")
			}
		}
	})
}

// ── Suspend ───────────────────────────────────────────────────────

func TestSuspend(t *testing.T) {
	t.Run("suspende el usuario, revoca tokens y emite evento", func(t *testing.T) {
		user := newProfessional(t)
		_, _ = user.IssueRefreshToken("d", "h", time.Hour)
		admin := uuid.New()

		if err := user.Suspend("incumplimiento", admin); err != nil {
			t.Fatalf("Suspend() error = %v", err)
		}

		if user.Status() != valueobject.StatusSuspended {
			t.Errorf("Status = %v, se esperaba Suspended", user.Status())
		}
		for _, rt := range user.RefreshTokens() {
			if rt.RevokedAt == nil {
				t.Error("tokens deberían estar revocados")
			}
		}
		evts := user.PendingEvents()
		if len(evts) == 0 {
			t.Fatal("se esperaba al menos un evento")
		}
		found := false
		for _, e := range evts {
			if e.EventType() == "user.suspended" {
				found = true
			}
		}
		if !found {
			t.Error("se esperaba evento 'user.suspended'")
		}
	})

	t.Run("no puede suspenderse a un superadmin", func(t *testing.T) {
		sa, err := aggregate.NewUser(
			mustEmail(t, "sa@example.com"),
			mustHash(t, "Sup3rSecret"),
			valueobject.RoleSuperAdmin, nil, "", nil,
		)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}

		err = sa.Suspend("test", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})

	t.Run("no puede suspenderse un usuario ya suspendido", func(t *testing.T) {
		user := newProfessional(t)
		_ = user.Suspend("primera vez", uuid.New())
		user.PendingEvents()

		err := user.Suspend("segunda vez", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})
}

// ── Activate ──────────────────────────────────────────────────────

func TestActivate(t *testing.T) {
	t.Run("reactiva un usuario suspendido", func(t *testing.T) {
		user := newProfessional(t)
		_ = user.Suspend("test", uuid.New())
		user.PendingEvents()

		admin := uuid.New()
		if err := user.Activate(admin); err != nil {
			t.Fatalf("Activate() error = %v", err)
		}
		if user.Status() != valueobject.StatusActive {
			t.Errorf("Status = %v, se esperaba Active", user.Status())
		}
	})

	t.Run("no puede activarse un usuario ya activo", func(t *testing.T) {
		user := newProfessional(t)
		err := user.Activate(uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})
}

// ── BumpVersion ───────────────────────────────────────────────────

func TestBumpVersion(t *testing.T) {
	user := newProfessional(t)
	initial := user.Version()
	user.BumpVersion()
	if user.Version() != initial+1 {
		t.Errorf("Version = %d, se esperaba %d", user.Version(), initial+1)
	}
}

// ── SetLinkedID ───────────────────────────────────────────────────

func TestSetLinkedID(t *testing.T) {
	t.Run("establece linkedID y linkedType en el aggregate", func(t *testing.T) {
		user := newProfessional(t)
		id := uuid.New()

		user.SetLinkedID(&id, "patient")

		if user.LinkedID() == nil || *user.LinkedID() != id {
			t.Errorf("LinkedID = %v, se esperaba %v", user.LinkedID(), id)
		}
		if user.LinkedType() != "patient" {
			t.Errorf("LinkedType = %q, se esperaba 'patient'", user.LinkedType())
		}
	})
}

// ── Getters ───────────────────────────────────────────────────────

func TestUserGetters(t *testing.T) {
	em := mustEmail(t, "getters@example.com")
	hp := mustHash(t, "Sup3rSecret")
	linkedID := uuid.New()

	user, err := aggregate.NewUser(em, hp, valueobject.RolePatient, &linkedID, "patient", nil)
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	user.PendingEvents()

	if user.Email() != em {
		t.Errorf("Email mismatch")
	}
	if user.Role() != valueobject.RolePatient {
		t.Errorf("Role mismatch")
	}
	if user.Status() != valueobject.StatusActive {
		t.Errorf("Status mismatch")
	}
	if user.Password().Bytes() == nil {
		t.Error("Password vacío")
	}
	if len(user.RefreshTokens()) != 0 {
		t.Error("RefreshTokens debería estar vacío en usuario nuevo")
	}
	if user.Audit().CreatedAt.IsZero() {
		t.Error("Audit.CreatedAt cero")
	}
}
