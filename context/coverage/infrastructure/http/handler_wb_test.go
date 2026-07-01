// White-box tests (package http) to cover the claims==nil safety branches in
// CreateAgreement and ResolveAuthorization. These paths are unreachable when
// going through RegisterRoutes (the JWT middleware ensures claims are always set),
// so they cannot be exercised from the external test package.
package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateAgreement_NilClaims covers the defensive `claims == nil → 401`
// check inside CreateAgreement that is never reached in production (JWT
// middleware always populates claims before the handler runs).
func TestCreateAgreement_NilClaims(t *testing.T) {
	h := &coverageHTTPHandler{}
	r := httptest.NewRequest(http.MethodPost, "/agreements", nil)
	w := httptest.NewRecorder()
	h.CreateAgreement(w, r) // no claims in context
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestResolveAuthorization_NilClaims covers the defensive `claims == nil → 401`
// check inside ResolveAuthorization.
func TestResolveAuthorization_NilClaims(t *testing.T) {
	h := &coverageHTTPHandler{}
	r := httptest.NewRequest(http.MethodPatch, "/authorizations/some-id/resolve", nil)
	w := httptest.NewRecorder()
	h.ResolveAuthorization(w, r) // no claims in context → early 401 return
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
