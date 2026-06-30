// White-box: package service para acceder a los helpers unexported
// normalizeName, nameSimilarity, bigrams y min.
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/repository"
	"github.com/juantevez/odontoagenda/context/patient/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── mock ─────────────────────────────────────────────────────────

type mockPatientRepo struct {
	existsByNationalID  bool
	existsErr           error
	potentialDuplicates []*aggregate.Patient
	findPotentialErr    error
}

var _ repository.PatientRepository = (*mockPatientRepo)(nil)

func (m *mockPatientRepo) Save(_ context.Context, _ *aggregate.Patient) error   { return nil }
func (m *mockPatientRepo) Update(_ context.Context, _ *aggregate.Patient) error { return nil }
func (m *mockPatientRepo) FindByID(_ context.Context, id sharedtypes.PatientID) (*aggregate.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", id.String())
}
func (m *mockPatientRepo) FindByNationalID(_ context.Context, _ sharedvo.NationalID) (*aggregate.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", "")
}
func (m *mockPatientRepo) FindByUserID(_ context.Context, _ sharedtypes.UserID) (*aggregate.Patient, error) {
	return nil, sharederrors.NewNotFound("Patient", "")
}
func (m *mockPatientRepo) Search(_ context.Context, _ string, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	return sharedtypes.PagedResult[*aggregate.Patient]{}, nil
}
func (m *mockPatientRepo) FindNearClinic(_ context.Context, _ sharedtypes.ClinicID, _ float64, _ sharedtypes.Page) (sharedtypes.PagedResult[*aggregate.Patient], error) {
	return sharedtypes.PagedResult[*aggregate.Patient]{}, nil
}
func (m *mockPatientRepo) ExistsByNationalID(_ context.Context, _ sharedvo.NationalID) (bool, error) {
	return m.existsByNationalID, m.existsErr
}
func (m *mockPatientRepo) FindPotentialDuplicates(_ context.Context, _ string, _ string) ([]*aggregate.Patient, error) {
	return m.potentialDuplicates, m.findPotentialErr
}
func (m *mockPatientRepo) Archive(_ context.Context, _ sharedtypes.PatientID, _ string, _ sharedtypes.UserID) error {
	return nil
}

// ── helpers ──────────────────────────────────────────────────────

func newCandidate(t *testing.T, fullName, phone string) *aggregate.Patient {
	t.Helper()
	name, err := sharedvo.NewFullName(fullName)
	if err != nil {
		t.Fatalf("setup: NewFullName: %v", err)
	}
	bd, _ := valueobject.NewBirthDate(1990, 1, 1)
	gender, _ := valueobject.ParseGender("M")
	docID, _ := sharedvo.NewNationalID(sharedvo.DocDNI, "99999999")
	phoneVO, err := sharedvo.NewPhoneNumber(phone)
	if err != nil {
		t.Fatalf("setup: NewPhoneNumber: %v", err)
	}
	p, err := aggregate.NewPatient(nil, name, bd, gender, docID, phoneVO, nil)
	if err != nil {
		t.Fatalf("setup: NewPatient: %v", err)
	}
	p.PendingEvents()
	return p
}

func validNationalID(t *testing.T) sharedvo.NationalID {
	t.Helper()
	id, err := sharedvo.NewNationalID(sharedvo.DocDNI, "12345678")
	if err != nil {
		t.Fatalf("setup: NewNationalID: %v", err)
	}
	return id
}

func domainCode(t *testing.T, err error) sharederrors.ErrorCode {
	t.Helper()
	de, ok := sharederrors.As(err)
	if !ok {
		t.Fatalf("se esperaba *DomainError, se obtuvo %T: %v", err, err)
	}
	return de.Code
}

// ── Detect ───────────────────────────────────────────────────────

func TestDetect(t *testing.T) {
	t.Run("NationalID exacto retorna ErrAlreadyExists", func(t *testing.T) {
		repo := &mockPatientRepo{existsByNationalID: true}
		d := NewDuplicateDetector(repo)

		_, err := d.Detect(context.Background(), "Juan Perez", "+5491112345678", validNationalID(t))
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrAlreadyExists {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrAlreadyExists)
		}
	})

	t.Run("error interno si ExistsByNationalID falla", func(t *testing.T) {
		repo := &mockPatientRepo{existsErr: errors.New("db timeout")}
		d := NewDuplicateDetector(repo)

		_, err := d.Detect(context.Background(), "Juan Perez", "+5491112345678", validNationalID(t))
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInternal {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInternal)
		}
	})

	t.Run("sin candidatos potenciales retorna HasDuplicates=false", func(t *testing.T) {
		repo := &mockPatientRepo{}
		d := NewDuplicateDetector(repo)

		result, err := d.Detect(context.Background(), "Juan Perez", "+5491112345678", validNationalID(t))
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if result.HasDuplicates {
			t.Error("HasDuplicates = true, se esperaba false")
		}
		if len(result.Candidates) != 0 {
			t.Errorf("len(Candidates) = %d, se esperaba 0", len(result.Candidates))
		}
	})

	t.Run("error interno si FindPotentialDuplicates falla", func(t *testing.T) {
		repo := &mockPatientRepo{findPotentialErr: errors.New("db timeout")}
		d := NewDuplicateDetector(repo)

		_, err := d.Detect(context.Background(), "Juan Perez", "+5491112345678", validNationalID(t))
		if err == nil {
			t.Fatal("se esperaba error")
		}
		if code := domainCode(t, err); code != sharederrors.ErrInternal {
			t.Errorf("code = %v, se esperaba %v", code, sharederrors.ErrInternal)
		}
	})

	t.Run("candidato con nombre similar produce score por 'name'", func(t *testing.T) {
		candidate := newCandidate(t, "Juan Perez", "+5491100000000") // teléfono distinto
		repo := &mockPatientRepo{potentialDuplicates: []*aggregate.Patient{candidate}}
		d := NewDuplicateDetector(repo)

		result, err := d.Detect(context.Background(), "Juan Perez", "+5491112345678", validNationalID(t))
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if !result.HasDuplicates {
			t.Fatal("HasDuplicates = false, se esperaba true")
		}
		if len(result.Candidates) != 1 {
			t.Fatalf("len(Candidates) = %d, se esperaba 1", len(result.Candidates))
		}
		c := result.Candidates[0]
		if c.Score != 0.5 {
			t.Errorf("Score = %v, se esperaba 0.5", c.Score)
		}
		if len(c.MatchedOn) != 1 || c.MatchedOn[0] != "name" {
			t.Errorf("MatchedOn = %v, se esperaba ['name']", c.MatchedOn)
		}
	})

	t.Run("candidato con mismo teléfono produce score por 'phone'", func(t *testing.T) {
		samePhone := "+5491112345678"
		candidate := newCandidate(t, "Persona Completamente Distinta", samePhone)
		repo := &mockPatientRepo{potentialDuplicates: []*aggregate.Patient{candidate}}
		d := NewDuplicateDetector(repo)

		result, err := d.Detect(context.Background(), "Juan Perez", samePhone, validNationalID(t))
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if !result.HasDuplicates {
			t.Fatal("HasDuplicates = false, se esperaba true")
		}
		c := result.Candidates[0]
		if c.Score != 0.8 {
			t.Errorf("Score = %v, se esperaba 0.8", c.Score)
		}
		if len(c.MatchedOn) != 1 || c.MatchedOn[0] != "phone" {
			t.Errorf("MatchedOn = %v, se esperaba ['phone']", c.MatchedOn)
		}
	})

	t.Run("candidato con nombre y teléfono coincidentes suma scores y los topea en 1.0", func(t *testing.T) {
		samePhone := "+5491112345678"
		candidate := newCandidate(t, "Juan Perez", samePhone)
		repo := &mockPatientRepo{potentialDuplicates: []*aggregate.Patient{candidate}}
		d := NewDuplicateDetector(repo)

		result, err := d.Detect(context.Background(), "Juan Perez", samePhone, validNationalID(t))
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		c := result.Candidates[0]
		if c.Score != 1.0 {
			t.Errorf("Score = %v, se esperaba 1.0 (0.5+0.8 topeado)", c.Score)
		}
		if len(c.MatchedOn) != 2 {
			t.Errorf("MatchedOn = %v, se esperaban 2 matches", c.MatchedOn)
		}
	})

	t.Run("candidato sin similitud de nombre ni teléfono no se incluye", func(t *testing.T) {
		candidate := newCandidate(t, "Persona Totalmente Distinta Y Larga", "+5491100000099")
		repo := &mockPatientRepo{potentialDuplicates: []*aggregate.Patient{candidate}}
		d := NewDuplicateDetector(repo)

		result, err := d.Detect(context.Background(), "Juan Perez", "+5491112345678", validNationalID(t))
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if result.HasDuplicates {
			t.Error("HasDuplicates = true, se esperaba false (candidato sin score)")
		}
		if len(result.Candidates) != 0 {
			t.Errorf("len(Candidates) = %d, se esperaba 0", len(result.Candidates))
		}
	})

	t.Run("phone vacío en el comando no genera match de teléfono", func(t *testing.T) {
		candidate := newCandidate(t, "Persona Distinta Sin Relacion", "+5491112345678")
		repo := &mockPatientRepo{potentialDuplicates: []*aggregate.Patient{candidate}}
		d := NewDuplicateDetector(repo)

		// phone="" en el comando: nunca debe matchear contra el teléfono del candidato.
		result, err := d.Detect(context.Background(), "Juan Perez", "", validNationalID(t))
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if result.HasDuplicates {
			t.Error("no debería haber match de teléfono con phone vacío")
		}
	})
}

// ── normalizeName ─────────────────────────────────────────────────

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Juan Perez", "juan perez"},
		{"  Juan   Perez  ", "juan perez"},
		{"José María Ñúñez", "jose maria nunez"},
		{"ÁÉÍÓÚÜÑ", "aeiouun"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := normalizeName(tc.input); got != tc.want {
				t.Errorf("normalizeName(%q) = %q, se esperaba %q", tc.input, got, tc.want)
			}
		})
	}
}

// ── bigrams ───────────────────────────────────────────────────────

func TestBigrams(t *testing.T) {
	t.Run("genera pares de caracteres consecutivos", func(t *testing.T) {
		got := bigrams("abc")
		want := map[string]int{"ab": 1, "bc": 1}
		if len(got) != len(want) {
			t.Fatalf("len = %d, se esperaba %d", len(got), len(want))
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("bigrams[%q] = %d, se esperaba %d", k, got[k], v)
			}
		}
	})

	t.Run("cuenta bigramas repetidos", func(t *testing.T) {
		got := bigrams("aaa")
		if got["aa"] != 2 {
			t.Errorf("bigrams('aaa')['aa'] = %d, se esperaba 2", got["aa"])
		}
	})

	t.Run("string vacío retorna mapa vacío", func(t *testing.T) {
		got := bigrams("")
		if len(got) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(got))
		}
	})

	t.Run("string de un solo caracter retorna mapa vacío", func(t *testing.T) {
		got := bigrams("a")
		if len(got) != 0 {
			t.Errorf("len = %d, se esperaba 0", len(got))
		}
	})
}

// ── nameSimilarity ────────────────────────────────────────────────

func TestNameSimilarity(t *testing.T) {
	t.Run("strings idénticos retornan 1.0", func(t *testing.T) {
		if got := nameSimilarity("juan perez", "juan perez"); got != 1.0 {
			t.Errorf("got = %v, se esperaba 1.0", got)
		}
	})

	t.Run("strings de menos de 2 caracteres retornan 0.0", func(t *testing.T) {
		if got := nameSimilarity("a", "ab"); got != 0.0 {
			t.Errorf("got = %v, se esperaba 0.0", got)
		}
		if got := nameSimilarity("ab", ""); got != 0.0 {
			t.Errorf("got = %v, se esperaba 0.0", got)
		}
	})

	t.Run("strings completamente distintos tienen score bajo", func(t *testing.T) {
		got := nameSimilarity("juan perez", "maria gonzalez")
		if got > 0.3 {
			t.Errorf("got = %v, se esperaba un score bajo (<0.3)", got)
		}
	})

	t.Run("strings similares con una letra de diferencia tienen score alto", func(t *testing.T) {
		got := nameSimilarity("juan perez", "juan perz") // typo: falta una 'e'
		if got < 0.7 {
			t.Errorf("got = %v, se esperaba un score alto (>0.7)", got)
		}
	})
}

// ── min ───────────────────────────────────────────────────────────

func TestMin(t *testing.T) {
	cases := []struct {
		a, b, want float64
	}{
		{1.0, 2.0, 1.0},
		{2.0, 1.0, 1.0},
		{1.5, 1.5, 1.5},
		{0.0, -1.0, -1.0},
	}
	for _, tc := range cases {
		got := min(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("min(%v, %v) = %v, se esperaba %v", tc.a, tc.b, got, tc.want)
		}
	}
}
