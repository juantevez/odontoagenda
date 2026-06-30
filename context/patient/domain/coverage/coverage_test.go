package coverage_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/patient/domain/coverage"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── helpers ──────────────────────────────────────────────────────

func domainCode(t *testing.T, err error) sharederrors.ErrorCode {
	t.Helper()
	de, ok := sharederrors.As(err)
	if !ok {
		t.Fatalf("se esperaba *DomainError, se obtuvo %T: %v", err, err)
	}
	return de.Code
}

func newActiveCoverage(t *testing.T) *coverage.PatientCoverage {
	t.Helper()
	c, err := coverage.NewPatientCoverage(
		sharedtypes.PatientID(uuid.New()), valueobject.CoverageTypePrivate,
		nil, "", "", "",
		time.Now().Add(-time.Hour), nil, uuid.New(),
	)
	if err != nil {
		t.Fatalf("setup: NewPatientCoverage: %v", err)
	}
	return c
}

// ── ParseCoverageStatus ────────────────────────────────────────────

func TestParseCoverageStatus(t *testing.T) {
	cases := []struct {
		input   string
		want    coverage.CoverageStatus
		wantErr bool
	}{
		{"Active", coverage.CoverageStatusActive, false},
		{"Suspended", coverage.CoverageStatusSuspended, false},
		{"Expired", coverage.CoverageStatusExpired, false},
		{"Inexistente", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := coverage.ParseCoverageStatus(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("se esperaba error para %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("error = %v", err)
			}
			if got != tc.want {
				t.Errorf("got = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}

// ── NewPatientCoverage ──────────────────────────────────────────────

func TestNewPatientCoverage(t *testing.T) {
	patientID := sharedtypes.PatientID(uuid.New())
	createdBy := uuid.New()

	t.Run("Privado no requiere providerName", func(t *testing.T) {
		c, err := coverage.NewPatientCoverage(
			patientID, valueobject.CoverageTypePrivate, nil, "", "", "",
			time.Now(), nil, createdBy,
		)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if c.Status() != coverage.CoverageStatusActive {
			t.Errorf("Status = %v, se esperaba Active", c.Status())
		}
		if c.ID() == uuid.Nil {
			t.Error("ID vacío")
		}
	})

	t.Run("tipo no-Privado sin providerName falla", func(t *testing.T) {
		_, err := coverage.NewPatientCoverage(
			patientID, valueobject.CoverageTypeObraSocial, nil, "", "", "",
			time.Now(), nil, createdBy,
		)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("tipo no-Privado con providerName es válido", func(t *testing.T) {
		c, err := coverage.NewPatientCoverage(
			patientID, valueobject.CoverageTypeObraSocial, nil, "OSDE", "310", "12345",
			time.Now(), nil, createdBy,
		)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if c.ProviderName() != "OSDE" {
			t.Errorf("ProviderName = %q", c.ProviderName())
		}
	})

	t.Run("rechaza validUntil anterior a validFrom", func(t *testing.T) {
		from := time.Now()
		until := from.Add(-time.Hour)
		_, err := coverage.NewPatientCoverage(
			patientID, valueobject.CoverageTypePrivate, nil, "", "", "",
			from, &until, createdBy,
		)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("acepta validUntil posterior a validFrom", func(t *testing.T) {
		from := time.Now()
		until := from.Add(365 * 24 * time.Hour)
		c, err := coverage.NewPatientCoverage(
			patientID, valueobject.CoverageTypePrivate, nil, "", "", "",
			from, &until, createdBy,
		)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if c.ValidUntil() == nil {
			t.Error("ValidUntil debería estar seteado")
		}
	})

	t.Run("recorta espacios en blanco de providerName, planCode y membershipNumber", func(t *testing.T) {
		c, err := coverage.NewPatientCoverage(
			patientID, valueobject.CoverageTypeObraSocial, nil,
			"  OSDE  ", "  310  ", "  12345  ",
			time.Now(), nil, createdBy,
		)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if c.ProviderName() != "OSDE" || c.PlanCode() != "310" || c.MembershipNumber() != "12345" {
			t.Errorf("campos no recortados: %q %q %q", c.ProviderName(), c.PlanCode(), c.MembershipNumber())
		}
	})

	t.Run("validFrom se almacena en UTC", func(t *testing.T) {
		loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
		if err != nil {
			t.Skipf("zona horaria no disponible: %v", err)
		}
		localTime := time.Date(2025, 6, 15, 10, 0, 0, 0, loc)
		c, err := coverage.NewPatientCoverage(
			patientID, valueobject.CoverageTypePrivate, nil, "", "", "",
			localTime, nil, createdBy,
		)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if c.ValidFrom().Location() != time.UTC {
			t.Error("ValidFrom debería estar en UTC")
		}
		if !c.ValidFrom().Equal(localTime) {
			t.Errorf("ValidFrom = %v, se esperaba el mismo instante que %v", c.ValidFrom(), localTime)
		}
	})

	t.Run("agreementID se preserva", func(t *testing.T) {
		agreementID := uuid.New()
		c, err := coverage.NewPatientCoverage(
			patientID, valueobject.CoverageTypeObraSocial, &agreementID, "OSDE", "", "",
			time.Now(), nil, createdBy,
		)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if c.AgreementID() == nil || *c.AgreementID() != agreementID {
			t.Error("AgreementID no coincide")
		}
	})
}

// ── ReconstituteCoverage ────────────────────────────────────────────

func TestReconstituteCoverage(t *testing.T) {
	id := uuid.New()
	patientID := sharedtypes.PatientID(uuid.New())
	agreementID := uuid.New()
	createdAt := time.Now().Add(-30 * 24 * time.Hour)
	updatedAt := time.Now().Add(-time.Hour)
	createdBy := uuid.New()
	until := time.Now().Add(365 * 24 * time.Hour)

	c := coverage.ReconstituteCoverage(
		id, patientID, valueobject.CoverageTypeExtPrepaid, coverage.CoverageStatusSuspended,
		&agreementID, "OSDE", "310", "999",
		createdAt, &until,
		createdAt, updatedAt, createdBy,
	)

	if c.ID() != id {
		t.Errorf("ID = %v, se esperaba %v", c.ID(), id)
	}
	if c.Status() != coverage.CoverageStatusSuspended {
		t.Errorf("Status = %v, se esperaba Suspended", c.Status())
	}
	if c.CreatedAt() != createdAt {
		t.Error("CreatedAt no coincide")
	}
	if c.UpdatedAt() != updatedAt {
		t.Error("UpdatedAt no coincide")
	}
	if c.CreatedBy() != createdBy {
		t.Error("CreatedBy no coincide")
	}
	if len(c.AnnualLimits()) != 0 {
		t.Error("AnnualLimits debería inicializarse vacío")
	}
	if len(c.Benefits()) != 0 {
		t.Error("Benefits debería inicializarse vacío")
	}
}

// ── IsActive ─────────────────────────────────────────────────────

func TestIsActive(t *testing.T) {
	t.Run("false si el status no es Active", func(t *testing.T) {
		c := newActiveCoverage(t)
		_ = c.Suspend("test", uuid.New())
		if c.IsActive() {
			t.Error("se esperaba false para cobertura suspendida")
		}
	})

	t.Run("false si validFrom es futuro", func(t *testing.T) {
		c, err := coverage.NewPatientCoverage(
			sharedtypes.PatientID(uuid.New()), valueobject.CoverageTypePrivate, nil, "", "", "",
			time.Now().Add(24*time.Hour), nil, uuid.New(),
		)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		if c.IsActive() {
			t.Error("se esperaba false: la cobertura aún no comenzó")
		}
	})

	t.Run("true si está dentro del rango de vigencia", func(t *testing.T) {
		c := newActiveCoverage(t)
		if !c.IsActive() {
			t.Error("se esperaba true")
		}
	})

	t.Run("false si validUntil ya pasó", func(t *testing.T) {
		until := time.Now().Add(-time.Hour)
		c, err := coverage.NewPatientCoverage(
			sharedtypes.PatientID(uuid.New()), valueobject.CoverageTypePrivate, nil, "", "", "",
			time.Now().Add(-48*time.Hour), &until, uuid.New(),
		)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		if c.IsActive() {
			t.Error("se esperaba false: la cobertura ya venció")
		}
	})

	t.Run("true si validUntil es futuro", func(t *testing.T) {
		until := time.Now().Add(24 * time.Hour)
		c, err := coverage.NewPatientCoverage(
			sharedtypes.PatientID(uuid.New()), valueobject.CoverageTypePrivate, nil, "", "", "",
			time.Now().Add(-time.Hour), &until, uuid.New(),
		)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		if !c.IsActive() {
			t.Error("se esperaba true: aún no vence")
		}
	})
}

// ── Suspend ───────────────────────────────────────────────────────

func TestSuspend(t *testing.T) {
	t.Run("suspende una cobertura activa", func(t *testing.T) {
		c := newActiveCoverage(t)
		if err := c.Suspend("fraude detectado", uuid.New()); err != nil {
			t.Fatalf("Suspend() error = %v", err)
		}
		if c.Status() != coverage.CoverageStatusSuspended {
			t.Errorf("Status = %v, se esperaba Suspended", c.Status())
		}
	})

	t.Run("falla si ya está suspendida", func(t *testing.T) {
		c := newActiveCoverage(t)
		_ = c.Suspend("primera", uuid.New())

		err := c.Suspend("segunda", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})

	t.Run("falla si está vencida", func(t *testing.T) {
		c := newActiveCoverage(t)
		c.Expire()

		err := c.Suspend("test", uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v", code)
		}
	})
}

// ── Expire ────────────────────────────────────────────────────────

func TestExpire(t *testing.T) {
	c := newActiveCoverage(t)
	c.Expire()
	if c.Status() != coverage.CoverageStatusExpired {
		t.Errorf("Status = %v, se esperaba Expired", c.Status())
	}
}

// ── SetCoPayPercent / SetCoPayFixed ───────────────────────────────

func TestSetCoPayPercent(t *testing.T) {
	t.Run("acepta valores entre 0 y 100", func(t *testing.T) {
		c := newActiveCoverage(t)
		if err := c.SetCoPayPercent(25); err != nil {
			t.Fatalf("error = %v", err)
		}
		if c.CoPayPercent() == nil || *c.CoPayPercent() != 25 {
			t.Errorf("CoPayPercent = %v, se esperaba 25", c.CoPayPercent())
		}
	})

	t.Run("rechaza porcentaje negativo", func(t *testing.T) {
		c := newActiveCoverage(t)
		err := c.SetCoPayPercent(-1)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza porcentaje mayor a 100", func(t *testing.T) {
		c := newActiveCoverage(t)
		err := c.SetCoPayPercent(101)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("limpia CoPayFixed al establecer porcentaje", func(t *testing.T) {
		c := newActiveCoverage(t)
		_ = c.SetCoPayFixed(5000)
		_ = c.SetCoPayPercent(20)

		if c.CoPayFixed() != nil {
			t.Error("CoPayFixed debería ser nil tras setear CoPayPercent")
		}
	})
}

func TestSetCoPayFixed(t *testing.T) {
	t.Run("acepta monto no negativo", func(t *testing.T) {
		c := newActiveCoverage(t)
		if err := c.SetCoPayFixed(15000); err != nil {
			t.Fatalf("error = %v", err)
		}
		if c.CoPayFixed() == nil || *c.CoPayFixed() != 15000 {
			t.Errorf("CoPayFixed = %v, se esperaba 15000", c.CoPayFixed())
		}
	})

	t.Run("rechaza monto negativo", func(t *testing.T) {
		c := newActiveCoverage(t)
		err := c.SetCoPayFixed(-1)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("acepta monto cero", func(t *testing.T) {
		c := newActiveCoverage(t)
		if err := c.SetCoPayFixed(0); err != nil {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("limpia CoPayPercent al establecer monto fijo", func(t *testing.T) {
		c := newActiveCoverage(t)
		_ = c.SetCoPayPercent(20)
		_ = c.SetCoPayFixed(5000)

		if c.CoPayPercent() != nil {
			t.Error("CoPayPercent debería ser nil tras setear CoPayFixed")
		}
	})
}

// ── SetAnnualLimit / ConsumeLimit ──────────────────────────────────

func TestSetAnnualLimit(t *testing.T) {
	c := newActiveCoverage(t)
	c.SetAnnualLimit("D1110", 100000)

	if c.AnnualLimits()["D1110"] != 100000 {
		t.Errorf("AnnualLimits['D1110'] = %d, se esperaba 100000", c.AnnualLimits()["D1110"])
	}
	if c.RemainingLimits()["D1110"] != 100000 {
		t.Errorf("RemainingLimits['D1110'] = %d, se esperaba 100000 inicialmente", c.RemainingLimits()["D1110"])
	}
}

func TestConsumeLimit(t *testing.T) {
	t.Run("sin límite definido no hace nada y no falla", func(t *testing.T) {
		c := newActiveCoverage(t)
		if err := c.ConsumeLimit("D9999", 5000); err != nil {
			t.Errorf("error = %v, se esperaba nil (sin límite = ilimitado)", err)
		}
	})

	t.Run("consume del límite disponible", func(t *testing.T) {
		c := newActiveCoverage(t)
		c.SetAnnualLimit("D1110", 100000)

		if err := c.ConsumeLimit("D1110", 30000); err != nil {
			t.Fatalf("error = %v", err)
		}
		if c.RemainingLimits()["D1110"] != 70000 {
			t.Errorf("remaining = %d, se esperaba 70000", c.RemainingLimits()["D1110"])
		}
	})

	t.Run("consumos sucesivos descuentan acumulativamente", func(t *testing.T) {
		c := newActiveCoverage(t)
		c.SetAnnualLimit("D1110", 100000)

		_ = c.ConsumeLimit("D1110", 30000)
		_ = c.ConsumeLimit("D1110", 20000)

		if c.RemainingLimits()["D1110"] != 50000 {
			t.Errorf("remaining = %d, se esperaba 50000", c.RemainingLimits()["D1110"])
		}
	})

	t.Run("falla si excede el límite disponible", func(t *testing.T) {
		c := newActiveCoverage(t)
		c.SetAnnualLimit("D1110", 100000)

		err := c.ConsumeLimit("D1110", 150000)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
		// El límite no debe haberse modificado tras un consumo rechazado.
		if c.RemainingLimits()["D1110"] != 100000 {
			t.Errorf("remaining = %d, no debería cambiar tras rechazo", c.RemainingLimits()["D1110"])
		}
	})

	t.Run("falla si el segundo consumo excede lo que queda", func(t *testing.T) {
		c := newActiveCoverage(t)
		c.SetAnnualLimit("D1110", 100000)
		_ = c.ConsumeLimit("D1110", 80000) // quedan 20000

		err := c.ConsumeLimit("D1110", 30000)
		if err == nil {
			t.Fatal("se esperaba error: 30000 > 20000 restantes")
		}
	})
}

// ── AddBenefit / CoverageForProcedure ──────────────────────────────

func TestAddBenefit(t *testing.T) {
	t.Run("agrega un nuevo beneficio", func(t *testing.T) {
		c := newActiveCoverage(t)
		err := c.AddBenefit(coverage.Benefit{
			ProcedureCode:   "D1110",
			Description:     "Limpieza",
			CoveragePercent: 80,
		})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(c.Benefits()) != 1 {
			t.Errorf("len(Benefits) = %d, se esperaba 1", len(c.Benefits()))
		}
	})

	t.Run("rechaza CoveragePercent fuera de rango", func(t *testing.T) {
		c := newActiveCoverage(t)
		err := c.AddBenefit(coverage.Benefit{ProcedureCode: "D1110", CoveragePercent: 150})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("rechaza CoveragePercent negativo", func(t *testing.T) {
		c := newActiveCoverage(t)
		err := c.AddBenefit(coverage.Benefit{ProcedureCode: "D1110", CoveragePercent: -10})
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInvalidArgument {
			t.Errorf("code = %v", code)
		}
	})

	t.Run("reemplaza beneficio existente con el mismo ProcedureCode", func(t *testing.T) {
		c := newActiveCoverage(t)
		_ = c.AddBenefit(coverage.Benefit{ProcedureCode: "D1110", CoveragePercent: 50})
		_ = c.AddBenefit(coverage.Benefit{ProcedureCode: "D1110", CoveragePercent: 90})

		if len(c.Benefits()) != 1 {
			t.Fatalf("len(Benefits) = %d, se esperaba 1 (reemplazo, no duplicado)", len(c.Benefits()))
		}
		if c.Benefits()[0].CoveragePercent != 90 {
			t.Errorf("CoveragePercent = %d, se esperaba 90 (el valor actualizado)", c.Benefits()[0].CoveragePercent)
		}
	})
}

func TestCoverageForProcedure(t *testing.T) {
	t.Run("retorna el benefit cuando existe", func(t *testing.T) {
		c := newActiveCoverage(t)
		_ = c.AddBenefit(coverage.Benefit{ProcedureCode: "D1110", CoveragePercent: 80})

		b, found := c.CoverageForProcedure("D1110")
		if !found {
			t.Fatal("se esperaba found=true")
		}
		if b.CoveragePercent != 80 {
			t.Errorf("CoveragePercent = %d", b.CoveragePercent)
		}
	})

	t.Run("retorna found=false cuando no existe", func(t *testing.T) {
		c := newActiveCoverage(t)
		_, found := c.CoverageForProcedure("D9999")
		if found {
			t.Error("se esperaba found=false")
		}
	})
}

// ── Getters (smoke test) ────────────────────────────────────────────

func TestCoverageGetters(t *testing.T) {
	patientID := sharedtypes.PatientID(uuid.New())
	c, err := coverage.NewPatientCoverage(
		patientID, valueobject.CoverageTypeObraSocial, nil, "OSDE", "310", "999",
		time.Now(), nil, uuid.New(),
	)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if c.PatientID() != patientID {
		t.Error("PatientID no coincide")
	}
	if c.CoverageType() != valueobject.CoverageTypeObraSocial {
		t.Errorf("CoverageType = %v", c.CoverageType())
	}
	if c.PlanCode() != "310" {
		t.Errorf("PlanCode = %q", c.PlanCode())
	}
	if c.MembershipNumber() != "999" {
		t.Errorf("MembershipNumber = %q", c.MembershipNumber())
	}
	if c.CreatedAt().IsZero() {
		t.Error("CreatedAt no debería ser cero")
	}
	if c.UpdatedAt().IsZero() {
		t.Error("UpdatedAt no debería ser cero")
	}
}
