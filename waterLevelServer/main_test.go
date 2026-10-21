package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func setupAuthTest(t *testing.T) {
	t.Helper()
	oldSecret, oldAccounts := jwtSecret, accounts
	jwtSecret = []byte("unit-test-only-signing-key-not-for-deployment")
	accounts = []account{
		{username: "admin", password: "test-admin-password", role: roleSuperadmin},
		{username: "viewer", password: "test-viewer-password", role: roleViewer},
		{username: "disabled", password: "", role: roleViewer},
	}
	t.Cleanup(func() {
		jwtSecret, accounts = oldSecret, oldAccounts
	})
}

func signedTestToken(t *testing.T, role string, expires time.Time, key []byte) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  "test-user",
		"role": role,
		"iat":  time.Now().Unix(),
		"exp":  expires.Unix(),
	}).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestLoginIssuesVerifiableJWT(t *testing.T) {
	setupAuthTest(t)
	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(`{"username":"viewer","password":"test-viewer-password"}`))
	rec := httptest.NewRecorder()
	loginHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d; want 200", rec.Code)
	}
	var result loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Token == "" || result.Role != roleViewer {
		t.Fatal("login did not return a viewer token")
	}
	token, err := jwt.Parse(result.Token, func(_ *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		t.Fatalf("issued token failed verification: %v", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("unexpected claims type")
	}
	if claims["sub"] != "viewer" || claims["role"] != roleViewer {
		t.Fatal("token has incorrect account claims")
	}
	issuedAt, err := claims.GetIssuedAt()
	if err != nil || issuedAt == nil {
		t.Fatalf("invalid issued-at claim: %v", err)
	}
	expiry, err := claims.GetExpirationTime()
	if err != nil || expiry == nil {
		t.Fatalf("invalid expiration claim: %v", err)
	}
	if lifetime := expiry.Sub(issuedAt.Time); lifetime != 24*time.Hour {
		t.Fatalf("token lifetime = %v; want 24h", lifetime)
	}
}

func TestLoginRejectsInvalidOrDisabledAccounts(t *testing.T) {
	setupAuthTest(t)
	for _, body := range []string{
		`{"username":"viewer","password":"wrong-password"}`,
		`{"username":"unknown","password":"test-viewer-password"}`,
		`{"username":"disabled","password":""}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
		rec := httptest.NewRecorder()
		loginHandler(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("invalid credentials returned %d; want 401", rec.Code)
		}
	}
}

func TestAuthMiddlewareRejectsInvalidTokens(t *testing.T) {
	setupAuthTest(t)
	validUntil := time.Now().Add(time.Hour)
	tests := map[string]string{
		"missing":   "",
		"malformed": "Bearer not-a-jwt",
		"expired": "Bearer " + signedTestToken(t, roleViewer,
			time.Now().Add(-time.Hour), jwtSecret),
		"wrong-signature": "Bearer " + signedTestToken(t, roleSuperadmin,
			validUntil, []byte("different-test-only-key")),
	}
	for name, header := range tests {
		t.Run(name, func(t *testing.T) {
			called := false
			handler := authMiddleware(func(w http.ResponseWriter, r *http.Request) {
				called = true
			})
			req := httptest.NewRequest(http.MethodGet, "/latest", nil)
			req.Header.Set("Authorization", header)
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != http.StatusUnauthorized || called {
				t.Fatalf("status = %d, handler called = %v; want 401, false",
					rec.Code, called)
			}
		})
	}
}

func TestVerifiedRoleControlsPrivilegedAccess(t *testing.T) {
	setupAuthTest(t)
	for _, tc := range []struct {
		role string
		want int
	}{
		{role: roleSuperadmin, want: http.StatusNoContent},
		{role: roleViewer, want: http.StatusForbidden},
		{role: "", want: http.StatusForbidden},
	} {
		t.Run("role="+tc.role, func(t *testing.T) {
			handler := authMiddleware(func(w http.ResponseWriter, r *http.Request) {
				if !requireSuperadmin(w, r) {
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodPost, "/pump", nil)
			req.Header.Set("Authorization", "Bearer "+signedTestToken(t,
				tc.role, time.Now().Add(time.Hour), jwtSecret))
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d; want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestSSEAuthenticatesQueryToken(t *testing.T) {
	setupAuthTest(t)
	valid := signedTestToken(t, roleViewer, time.Now().Add(time.Hour), jwtSecret)
	expired := signedTestToken(t, roleViewer, time.Now().Add(-time.Hour), jwtSecret)
	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{name: "valid", token: valid, want: http.StatusNoContent},
		{name: "missing", want: http.StatusUnauthorized},
		{name: "malformed", token: "not-a-jwt", want: http.StatusUnauthorized},
		{name: "expired", token: expired, want: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := sseAuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/events?token="+tc.token, nil)
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d; want %d", rec.Code, tc.want)
			}
		})
	}
}
