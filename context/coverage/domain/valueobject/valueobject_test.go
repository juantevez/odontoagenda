package valueobject_test

import (
	"testing"

	"github.com/juantevez/odontoagenda/context/coverage/domain/valueobject"
)

// ── ProviderType ──────────────────────────────────────────────────

func TestParseProviderType_Valid(t *testing.T) {
	valid := []string{
		"ObraSocial", "PrepagaExterna", "PrepagaPropia",
		"Corporativo", "ConvenioEspecial", "Privado",
	}
	for _, s := range valid {
		pt, err := valueobject.ParseProviderType(s)
		if err != nil {
			t.Errorf("ParseProviderType(%q) unexpected error: %v", s, err)
		}
		if pt.String() != s {
			t.Errorf("ParseProviderType(%q).String() = %q", s, pt.String())
		}
	}
}

func TestParseProviderType_Invalid(t *testing.T) {
	_, err := valueobject.ParseProviderType("invalid")
	if err == nil {
		t.Fatal("expected error for invalid ProviderType")
	}
}

func TestProviderType_IsPrivado(t *testing.T) {
	if !valueobject.ProviderTypePrivado.IsPrivado() {
		t.Fatal("Privado.IsPrivado() should be true")
	}
	if valueobject.ProviderTypeObraSocial.IsPrivado() {
		t.Fatal("ObraSocial.IsPrivado() should be false")
	}
}

// ── AgreementStatus ───────────────────────────────────────────────

func TestParseAgreementStatus_Valid(t *testing.T) {
	cases := []struct {
		s        string
		isActive bool
	}{
		{"Active", true},
		{"Suspended", false},
		{"Expired", false},
	}
	for _, tc := range cases {
		st, err := valueobject.ParseAgreementStatus(tc.s)
		if err != nil {
			t.Errorf("ParseAgreementStatus(%q) unexpected error: %v", tc.s, err)
		}
		if st.String() != tc.s {
			t.Errorf("String() = %q, want %q", st.String(), tc.s)
		}
		if st.IsActive() != tc.isActive {
			t.Errorf("IsActive() = %v, want %v for %q", st.IsActive(), tc.isActive, tc.s)
		}
	}
}

func TestParseAgreementStatus_Invalid(t *testing.T) {
	_, err := valueobject.ParseAgreementStatus("unknown")
	if err == nil {
		t.Fatal("expected error for invalid AgreementStatus")
	}
}

// ── PlanStatus ────────────────────────────────────────────────────

func TestPlanStatus_IsActive(t *testing.T) {
	if !valueobject.PlanStatusActive.IsActive() {
		t.Fatal("PlanStatusActive.IsActive() should be true")
	}
	if valueobject.PlanStatusDiscontinued.IsActive() {
		t.Fatal("PlanStatusDiscontinued.IsActive() should be false")
	}
}

func TestPlanStatus_String(t *testing.T) {
	if valueobject.PlanStatusActive.String() != "Active" {
		t.Fatal("PlanStatusActive.String() mismatch")
	}
	if valueobject.PlanStatusDiscontinued.String() != "Discontinued" {
		t.Fatal("PlanStatusDiscontinued.String() mismatch")
	}
}

// ── CoPayType ─────────────────────────────────────────────────────

func TestParseCoPayType_Valid(t *testing.T) {
	valid := []string{"Percent", "FixedAmount", "None"}
	for _, s := range valid {
		ct, err := valueobject.ParseCoPayType(s)
		if err != nil {
			t.Errorf("ParseCoPayType(%q) unexpected error: %v", s, err)
		}
		if ct.String() != s {
			t.Errorf("ParseCoPayType(%q).String() = %q", s, ct.String())
		}
	}
}

func TestParseCoPayType_Invalid(t *testing.T) {
	_, err := valueobject.ParseCoPayType("bad")
	if err == nil {
		t.Fatal("expected error for invalid CoPayType")
	}
}

// ── AuthorizationStatus ───────────────────────────────────────────

func TestParseAuthorizationStatus_Valid(t *testing.T) {
	cases := []struct {
		s          string
		isPending  bool
		isApproved bool
		isTerminal bool
	}{
		{"Pending", true, false, false},
		{"Approved", false, true, true},
		{"Rejected", false, false, true},
		{"Expired", false, false, true},
	}
	for _, tc := range cases {
		st, err := valueobject.ParseAuthorizationStatus(tc.s)
		if err != nil {
			t.Errorf("ParseAuthorizationStatus(%q) unexpected error: %v", tc.s, err)
		}
		if st.String() != tc.s {
			t.Errorf("String() = %q, want %q", st.String(), tc.s)
		}
		if st.IsPending() != tc.isPending {
			t.Errorf("IsPending() = %v, want %v for %q", st.IsPending(), tc.isPending, tc.s)
		}
		if st.IsApproved() != tc.isApproved {
			t.Errorf("IsApproved() = %v, want %v for %q", st.IsApproved(), tc.isApproved, tc.s)
		}
		if st.IsTerminal() != tc.isTerminal {
			t.Errorf("IsTerminal() = %v, want %v for %q", st.IsTerminal(), tc.isTerminal, tc.s)
		}
	}
}

func TestParseAuthorizationStatus_Invalid(t *testing.T) {
	_, err := valueobject.ParseAuthorizationStatus("nope")
	if err == nil {
		t.Fatal("expected error for invalid AuthorizationStatus")
	}
}

// ── AffiliationStatus ─────────────────────────────────────────────

func TestAffiliationStatus_IsActive(t *testing.T) {
	if !valueobject.AffiliationStatusActive.IsActive() {
		t.Fatal("AffiliationStatusActive.IsActive() should be true")
	}
	if valueobject.AffiliationStatusSuspended.IsActive() {
		t.Fatal("AffiliationStatusSuspended.IsActive() should be false")
	}
	if valueobject.AffiliationStatusCancelled.IsActive() {
		t.Fatal("AffiliationStatusCancelled.IsActive() should be false")
	}
}

func TestAffiliationStatus_String(t *testing.T) {
	cases := []struct {
		s    valueobject.AffiliationStatus
		want string
	}{
		{valueobject.AffiliationStatusActive, "Active"},
		{valueobject.AffiliationStatusSuspended, "Suspended"},
		{valueobject.AffiliationStatusCancelled, "Cancelled"},
	}
	for _, tc := range cases {
		if tc.s.String() != tc.want {
			t.Errorf("String() = %q, want %q", tc.s.String(), tc.want)
		}
	}
}

// ── NotCovered / PrivadoResult ────────────────────────────────────

func TestNotCovered(t *testing.T) {
	r := valueobject.NotCovered("no cubierto")
	if r.IsCovered {
		t.Fatal("IsCovered should be false")
	}
	if r.RejectionReason != "no cubierto" {
		t.Fatalf("RejectionReason = %q", r.RejectionReason)
	}
}

func TestPrivadoResult(t *testing.T) {
	r := valueobject.PrivadoResult()
	if !r.IsCovered {
		t.Fatal("IsCovered should be true")
	}
	if r.CoveragePercent != 0 {
		t.Fatalf("CoveragePercent = %d, want 0", r.CoveragePercent)
	}
	if r.CoPayType != valueobject.CoPayTypePercent {
		t.Fatalf("CoPayType = %s, want Percent", r.CoPayType)
	}
	if r.CoPayValue != 100 {
		t.Fatalf("CoPayValue = %d, want 100", r.CoPayValue)
	}
	if r.RequiresAuthorization {
		t.Fatal("RequiresAuthorization should be false")
	}
}
