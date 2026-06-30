package aggregate_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/iam/domain/aggregate"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── helpers ──────────────────────────────────────────────────────

// newFamily crea una FamilyAccount con un adulto primario.
func newFamily(t *testing.T) (*aggregate.FamilyAccount, sharedtypes.PatientID) {
	t.Helper()
	primaryID := sharedtypes.PatientID(uuid.New())
	f := aggregate.NewFamilyAccount(primaryID, "Familia Test", nil)
	return f, primaryID
}

// ── NewFamilyAccount ─────────────────────────────────────────────

func TestNewFamilyAccount(t *testing.T) {
	t.Run("crea cuenta con un único miembro (adulto primario)", func(t *testing.T) {
		primaryID := sharedtypes.PatientID(uuid.New())
		f := aggregate.NewFamilyAccount(primaryID, "Familia García", nil)

		if f.ID() == (sharedtypes.FamilyID{}) {
			t.Error("ID vacío")
		}
		if f.FamilyName() != "Familia García" {
			t.Errorf("FamilyName = %q, se esperaba 'Familia García'", f.FamilyName())
		}
		if f.PrimaryAdultID() != primaryID {
			t.Errorf("PrimaryAdultID = %v, se esperaba %v", f.PrimaryAdultID(), primaryID)
		}
		if f.Status() != aggregate.FamilyStatusActive {
			t.Errorf("Status = %v, se esperaba Active", f.Status())
		}
		if f.Version() != 1 {
			t.Errorf("Version = %d, se esperaba 1", f.Version())
		}

		members := f.Members()
		if len(members) != 1 {
			t.Fatalf("len(Members) = %d, se esperaba 1", len(members))
		}
		m := members[0]
		if m.PatientID != primaryID {
			t.Errorf("miembro PatientID = %v", m.PatientID)
		}
		if m.Role != aggregate.FamilyRolePrimaryAdult {
			t.Errorf("miembro Role = %v, se esperaba PrimaryAdult", m.Role)
		}
		if m.Relationship != aggregate.RelationshipSelf {
			t.Errorf("miembro Relationship = %v, se esperaba Titular", m.Relationship)
		}
		if m.IsMinor {
			t.Error("el adulto primario no debería ser menor")
		}
	})
}

// ── AddMinor ─────────────────────────────────────────────────────

func TestAddMinor(t *testing.T) {
	t.Run("agrega un menor con guardian válido", func(t *testing.T) {
		f, primaryID := newFamily(t)
		minorID := sharedtypes.PatientID(uuid.New())

		err := f.AddMinor(minorID, aggregate.RelationshipChild, []sharedtypes.PatientID{primaryID}, uuid.New())
		if err != nil {
			t.Fatalf("AddMinor() error = %v", err)
		}

		members := f.Members()
		if len(members) != 2 {
			t.Fatalf("len(Members) = %d, se esperaba 2", len(members))
		}
		var found bool
		for _, m := range members {
			if m.PatientID == minorID {
				found = true
				if !m.IsMinor {
					t.Error("IsMinor = false, se esperaba true")
				}
				if m.Role != aggregate.FamilyRoleDependent {
					t.Errorf("Role = %v, se esperaba Dependent", m.Role)
				}
			}
		}
		if !found {
			t.Error("el menor no fue encontrado entre los miembros")
		}
	})

	t.Run("rechaza menor sin guardianes", func(t *testing.T) {
		f, _ := newFamily(t)
		minorID := sharedtypes.PatientID(uuid.New())

		err := f.AddMinor(minorID, aggregate.RelationshipChild, nil, uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})

	t.Run("rechaza guardian que no es miembro de la familia", func(t *testing.T) {
		f, _ := newFamily(t)
		minorID := sharedtypes.PatientID(uuid.New())
		outsiderID := sharedtypes.PatientID(uuid.New())

		err := f.AddMinor(minorID, aggregate.RelationshipChild, []sharedtypes.PatientID{outsiderID}, uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})

	t.Run("rechaza guardian que es menor (no adulto)", func(t *testing.T) {
		f, primaryID := newFamily(t)
		// Agregar un primer menor con el adulto primario como guardian.
		minor1 := sharedtypes.PatientID(uuid.New())
		_ = f.AddMinor(minor1, aggregate.RelationshipChild, []sharedtypes.PatientID{primaryID}, uuid.New())

		// Intentar agregar un segundo menor con el primer menor como guardian.
		minor2 := sharedtypes.PatientID(uuid.New())
		err := f.AddMinor(minor2, aggregate.RelationshipChild, []sharedtypes.PatientID{minor1}, uuid.New())
		if err == nil {
			t.Fatal("se esperaba error: el guardian no puede ser menor")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})

	t.Run("rechaza paciente ya existente en la familia", func(t *testing.T) {
		f, primaryID := newFamily(t)
		minorID := sharedtypes.PatientID(uuid.New())
		_ = f.AddMinor(minorID, aggregate.RelationshipChild, []sharedtypes.PatientID{primaryID}, uuid.New())

		err := f.AddMinor(minorID, aggregate.RelationshipChild, []sharedtypes.PatientID{primaryID}, uuid.New())
		if err == nil {
			t.Fatal("se esperaba error por duplicado")
		}
		if code := domainCode(t, err); code != sharederrors.ErrAlreadyExists {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrAlreadyExists)
		}
	})
}

// ── AddSecondaryAdult ─────────────────────────────────────────────

func TestAddSecondaryAdult(t *testing.T) {
	t.Run("agrega adulto secundario correctamente", func(t *testing.T) {
		f, _ := newFamily(t)
		secondID := sharedtypes.PatientID(uuid.New())

		err := f.AddSecondaryAdult(secondID, aggregate.RelationshipSpouse, uuid.New())
		if err != nil {
			t.Fatalf("AddSecondaryAdult() error = %v", err)
		}

		members := f.Members()
		if len(members) != 2 {
			t.Fatalf("len(Members) = %d, se esperaba 2", len(members))
		}
		var found bool
		for _, m := range members {
			if m.PatientID == secondID {
				found = true
				if m.Role != aggregate.FamilyRoleSecondaryAdult {
					t.Errorf("Role = %v, se esperaba SecondaryAdult", m.Role)
				}
				if m.IsMinor {
					t.Error("adulto secundario no debería ser menor")
				}
			}
		}
		if !found {
			t.Error("adulto secundario no encontrado en miembros")
		}
	})

	t.Run("rechaza paciente ya existente", func(t *testing.T) {
		f, primaryID := newFamily(t)

		err := f.AddSecondaryAdult(primaryID, aggregate.RelationshipSpouse, uuid.New())
		if err == nil {
			t.Fatal("se esperaba error por duplicado")
		}
		if code := domainCode(t, err); code != sharederrors.ErrAlreadyExists {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrAlreadyExists)
		}
	})
}

// ── RemoveMember ─────────────────────────────────────────────────

func TestRemoveMember(t *testing.T) {
	t.Run("elimina un adulto secundario correctamente", func(t *testing.T) {
		f, _ := newFamily(t)
		secondID := sharedtypes.PatientID(uuid.New())
		_ = f.AddSecondaryAdult(secondID, aggregate.RelationshipSpouse, uuid.New())

		if err := f.RemoveMember(secondID, uuid.New()); err != nil {
			t.Fatalf("RemoveMember() error = %v", err)
		}
		if len(f.Members()) != 1 {
			t.Errorf("Members count = %d, se esperaba 1 tras la eliminación", len(f.Members()))
		}
	})

	t.Run("no puede eliminar al adulto primario", func(t *testing.T) {
		f, primaryID := newFamily(t)

		err := f.RemoveMember(primaryID, uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})

	t.Run("no puede eliminar al único guardian de un menor", func(t *testing.T) {
		f, primaryID := newFamily(t)
		// Agregar adulto secundario (S) que será el único guardian del menor.
		secondID := sharedtypes.PatientID(uuid.New())
		_ = f.AddSecondaryAdult(secondID, aggregate.RelationshipSpouse, uuid.New())

		// Agregar menor con guardian=S únicamente.
		minorID := sharedtypes.PatientID(uuid.New())
		_ = f.AddMinor(minorID, aggregate.RelationshipChild, []sharedtypes.PatientID{secondID}, primaryID)

		err := f.RemoveMember(secondID, uuid.New())
		if err == nil {
			t.Fatal("se esperaba error: el menor quedaría sin guardian")
		}
		if code := domainCode(t, err); code != sharederrors.ErrPrecondition {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrPrecondition)
		}
	})

	t.Run("puede eliminar un guardian si el menor tiene otro guardian", func(t *testing.T) {
		f, primaryID := newFamily(t)
		secondID := sharedtypes.PatientID(uuid.New())
		_ = f.AddSecondaryAdult(secondID, aggregate.RelationshipSpouse, uuid.New())

		// El menor tiene DOS guardianes: primary y secondary.
		minorID := sharedtypes.PatientID(uuid.New())
		_ = f.AddMinor(minorID, aggregate.RelationshipChild,
			[]sharedtypes.PatientID{primaryID, secondID}, uuid.New())

		// Eliminar secondary debe ser posible porque primary sigue como guardian.
		if err := f.RemoveMember(secondID, uuid.New()); err != nil {
			t.Fatalf("RemoveMember() error = %v (el menor aún tiene un guardian)", err)
		}
	})

	t.Run("devuelve ErrNotFound para miembro inexistente", func(t *testing.T) {
		f, _ := newFamily(t)

		err := f.RemoveMember(sharedtypes.PatientID(uuid.New()), uuid.New())
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrNotFound {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrNotFound)
		}
	})
}

// ── CanBookFor ────────────────────────────────────────────────────

func TestCanBookFor(t *testing.T) {
	t.Run("auto-reserva siempre permitida", func(t *testing.T) {
		f, primaryID := newFamily(t)
		if err := f.CanBookFor(primaryID, primaryID); err != nil {
			t.Errorf("CanBookFor(self) error = %v", err)
		}
	})

	t.Run("guardian puede reservar para su menor", func(t *testing.T) {
		f, primaryID := newFamily(t)
		minorID := sharedtypes.PatientID(uuid.New())
		_ = f.AddMinor(minorID, aggregate.RelationshipChild, []sharedtypes.PatientID{primaryID}, uuid.New())

		if err := f.CanBookFor(primaryID, minorID); err != nil {
			t.Errorf("CanBookFor(guardian, minor) error = %v", err)
		}
	})

	t.Run("adulto de la familia puede reservar para menor aunque no sea guardian explícito", func(t *testing.T) {
		f, primaryID := newFamily(t)
		secondID := sharedtypes.PatientID(uuid.New())
		_ = f.AddSecondaryAdult(secondID, aggregate.RelationshipSpouse, uuid.New())

		minorID := sharedtypes.PatientID(uuid.New())
		// El menor tiene solo al primario como guardian, pero segundo es adulto de la familia.
		_ = f.AddMinor(minorID, aggregate.RelationshipChild, []sharedtypes.PatientID{primaryID}, uuid.New())

		if err := f.CanBookFor(secondID, minorID); err != nil {
			t.Errorf("CanBookFor(family adult, minor) error = %v", err)
		}
	})

	t.Run("no puede reservar para paciente fuera de la cuenta familiar", func(t *testing.T) {
		f, primaryID := newFamily(t)
		outsiderID := sharedtypes.PatientID(uuid.New())

		err := f.CanBookFor(primaryID, outsiderID)
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrForbidden {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrForbidden)
		}
	})

	t.Run("adulto no puede reservar para otro adulto sin autorización explícita", func(t *testing.T) {
		f, primaryID := newFamily(t)
		secondID := sharedtypes.PatientID(uuid.New())
		_ = f.AddSecondaryAdult(secondID, aggregate.RelationshipSpouse, uuid.New())

		// Primary intenta reservar para second (ambos adultos).
		err := f.CanBookFor(primaryID, secondID)
		if err == nil {
			t.Fatal("se esperaba error: adulto no puede reservar para otro adulto")
		}
		if code := domainCode(t, err); code != sharederrors.ErrForbidden {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrForbidden)
		}
	})
}

// ── ReconstituteFamilyAccount ─────────────────────────────────────

func TestReconstituteFamilyAccount(t *testing.T) {
	t.Run("reconstruye correctamente desde persistencia", func(t *testing.T) {
		id := sharedtypes.FamilyID(uuid.New())
		primaryID := sharedtypes.PatientID(uuid.New())
		members := []aggregate.FamilyMember{
			{PatientID: primaryID, Role: aggregate.FamilyRolePrimaryAdult, Relationship: aggregate.RelationshipSelf},
		}
		audit := sharedtypes.NewAuditInfo(nil)

		f := aggregate.ReconstituteFamilyAccount(
			id, "Familia Test", primaryID, members,
			aggregate.FamilyStatusSuspended,
			audit, 3,
		)

		if f.ID() != id {
			t.Errorf("ID = %v, se esperaba %v", f.ID(), id)
		}
		if f.Status() != aggregate.FamilyStatusSuspended {
			t.Errorf("Status = %v, se esperaba Suspended", f.Status())
		}
		if f.Version() != 3 {
			t.Errorf("Version = %d, se esperaba 3", f.Version())
		}
		if len(f.Members()) != 1 {
			t.Errorf("len(Members) = %d, se esperaba 1", len(f.Members()))
		}
		if f.Audit().CreatedAt != audit.CreatedAt {
			t.Error("Audit no coincide con el valor pasado a Reconstitute")
		}
	})
}

// ── ParseFamilyStatus ─────────────────────────────────────────────

func TestParseFamilyStatus(t *testing.T) {
	cases := []struct {
		input   string
		want    aggregate.FamilyStatus
		wantErr bool
	}{
		{"Active", aggregate.FamilyStatusActive, false},
		{"Suspended", aggregate.FamilyStatusSuspended, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := aggregate.ParseFamilyStatus(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseFamilyStatus(%q) esperaba error, obtuvo nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseFamilyStatus(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("got = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}
