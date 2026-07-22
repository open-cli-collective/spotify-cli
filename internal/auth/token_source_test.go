package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/open-cli-collective/spotify-cli/internal/token"
)

func TestTokenSourceRefreshesAndPersistsWithoutLosingFields(t *testing.T) {
	var request url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("PKCE refresh sent an Authorization header")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		request = r.Form
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	initial := token.Envelope{
		AccessToken: "old-access", TokenType: "Bearer", RefreshToken: "old-refresh",
		ExpiresAt: time.Now().Add(-time.Minute), Scopes: []string{"user-read-private"},
	}
	var persisted token.Envelope
	source := NewTokenSource(context.Background(), server.Client(), "client-id", server.URL, initial, func(value token.Envelope) error {
		persisted = value
		return nil
	})

	got, err := source.Token()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new-access" || got.RefreshToken != "old-refresh" {
		t.Fatalf("refreshed token = access %q refresh %q", got.AccessToken, got.RefreshToken)
	}
	if request.Get("grant_type") != "refresh_token" || request.Get("refresh_token") != "old-refresh" || request.Get("client_id") != "client-id" {
		t.Fatalf("refresh request = %v", request)
	}
	if request.Get("client_secret") != "" {
		t.Fatal("PKCE refresh sent a client secret")
	}
	if persisted.AccessToken != "new-access" || persisted.RefreshToken != "old-refresh" || !slices.Equal(persisted.Scopes, []string{"user-read-private"}) {
		t.Fatalf("persisted envelope = %+v", persisted)
	}
}

func TestTokenSourcePersistsRotationWhenAccessTokenIsReused(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"access_token":"same-access","token_type":"Bearer","refresh_token":"new-refresh","expires_in":3600,"scope":"user-read-private"}`)
	}))
	defer server.Close()

	initial := token.Envelope{
		AccessToken: "same-access", TokenType: "Bearer", RefreshToken: "old-refresh",
		ExpiresAt: now.Add(-time.Minute), Scopes: []string{"user-read-private"},
	}
	var persisted token.Envelope
	source := NewTokenSource(context.Background(), server.Client(), "client-id", server.URL, initial, func(value token.Envelope) error {
		persisted = value
		return nil
	})
	if _, err := source.Token(); err != nil {
		t.Fatal(err)
	}
	if persisted.RefreshToken != "new-refresh" || !persisted.ExpiresAt.After(now) {
		t.Fatalf("persisted token = %+v", persisted)
	}
}

func TestTokenSourceRejectsExplicitlyEmptyScope(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"access_token":"new-access","token_type":"Bearer","expires_in":3600,"scope":""}`)
	}))
	defer server.Close()

	persisted := false
	source := NewTokenSource(context.Background(), server.Client(), "client-id", server.URL, token.Envelope{
		AccessToken: "old-access", TokenType: "Bearer", RefreshToken: "old-refresh",
		ExpiresAt: now.Add(-time.Minute), Scopes: []string{"user-read-private"},
	}, func(token.Envelope) error {
		persisted = true
		return nil
	})
	if _, err := source.Token(); !errors.Is(err, ErrRefresh) || persisted {
		t.Fatalf("error = %v, persisted = %t", err, persisted)
	}
}

func TestTokenSourceClassifiesInvalidGrantWithoutLeaking(t *testing.T) {
	const secret = "refresh-secret-sentinel"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"revoked ` + secret + `"}`))
	}))
	defer server.Close()

	initial := token.Envelope{
		AccessToken: "old-access", TokenType: "Bearer", RefreshToken: secret,
		ExpiresAt: time.Now().Add(-time.Minute), Scopes: []string{"user-read-private"},
	}
	_, err := NewTokenSource(context.Background(), server.Client(), "client-id", server.URL, initial, nil).Token()
	if !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("error = %v, want ErrInvalidGrant", err)
	}
	if err != nil && strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked refresh token: %v", err)
	}
}

func TestTokenSourceFailsWhenRefreshedTokenCannotPersist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","token_type":"Bearer","expires_in":3600,"refresh_token":"new-refresh","scope":"user-read-private"}`))
	}))
	defer server.Close()

	initial := token.Envelope{
		AccessToken: "old-access", TokenType: "Bearer", RefreshToken: "old-refresh",
		ExpiresAt: time.Now().Add(-time.Minute), Scopes: []string{"user-read-private"},
	}
	_, err := NewTokenSource(context.Background(), server.Client(), "client-id", server.URL, initial, func(token.Envelope) error {
		return errors.New("store rejected new-access")
	}).Token()
	if !errors.Is(err, ErrPersistRefresh) {
		t.Fatalf("error = %v, want ErrPersistRefresh", err)
	}
	if err != nil && strings.Contains(err.Error(), "new-access") {
		t.Fatalf("error leaked refreshed access token: %v", err)
	}
}
