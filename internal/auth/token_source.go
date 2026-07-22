// Package auth implements Spotify OAuth without owning command or presentation behavior.
package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/open-cli-collective/cli-common/credstore"
	"golang.org/x/oauth2"

	"github.com/open-cli-collective/spotify-cli/internal/token"
)

var (
	// ErrInvalidGrant means Spotify requires a new authorization flow.
	ErrInvalidGrant = errors.New("spotify authorization expired; run sptfy init")
	// ErrRefresh means Spotify could not refresh the access token.
	ErrRefresh = errors.New("refreshing Spotify authorization failed")
	// ErrPersistRefresh means a rotated token could not be saved.
	ErrPersistRefresh = errors.New("saving refreshed Spotify authorization failed")
)

// NewTokenSource returns a token source that persists every access-token rotation.
func NewTokenSource(ctx context.Context, httpClient *http.Client, clientID, tokenURL string, initial token.Envelope, persist func(token.Envelope) error) oauth2.TokenSource {
	if httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	}
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}
	config := oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
	return &persistentTokenSource{
		base:    config.TokenSource(ctx, toOAuthToken(initial)),
		current: initial,
		persist: persist,
	}
}

type persistentTokenSource struct {
	mu      sync.Mutex
	base    oauth2.TokenSource
	current token.Envelope
	persist func(token.Envelope) error
}

func (source *persistentTokenSource) Token() (*oauth2.Token, error) {
	source.mu.Lock()
	defer source.mu.Unlock()

	value, err := source.base.Token()
	if err != nil {
		var retrieve *oauth2.RetrieveError
		if errors.As(err, &retrieve) && retrieve.ErrorCode == "invalid_grant" {
			return nil, ErrInvalidGrant
		}
		return nil, ErrRefresh
	}
	next, err := fromOAuthToken(value, source.current.Scopes)
	if err != nil {
		return nil, ErrRefresh
	}
	if sameEnvelope(next, source.current) {
		return value, nil
	}
	if source.persist == nil {
		return nil, ErrPersistRefresh
	}
	if err := source.persist(next); err != nil {
		redactor := credstore.NewRedactor(source.current.AccessToken, source.current.RefreshToken, next.AccessToken, next.RefreshToken)
		message := redactor.Redact(err.Error())
		if message == "" {
			return nil, ErrPersistRefresh
		}
		return nil, errors.Join(ErrPersistRefresh, errors.New(message))
	}
	source.current = next
	return value, nil
}

func sameEnvelope(left, right token.Envelope) bool {
	return left.AccessToken == right.AccessToken && left.TokenType == right.TokenType &&
		left.RefreshToken == right.RefreshToken && left.ExpiresAt.Equal(right.ExpiresAt) &&
		slices.Equal(left.Scopes, right.Scopes)
}

func toOAuthToken(value token.Envelope) *oauth2.Token {
	return (&oauth2.Token{
		AccessToken: value.AccessToken, TokenType: value.TokenType,
		RefreshToken: value.RefreshToken, Expiry: value.ExpiresAt,
	}).WithExtra(map[string]any{"scope": strings.Join(value.Scopes, " ")})
}

func fromOAuthToken(value *oauth2.Token, fallbackScopes []string) (token.Envelope, error) {
	if value == nil || strings.TrimSpace(value.AccessToken) == "" || !strings.EqualFold(value.TokenType, "Bearer") || value.Expiry.IsZero() {
		return token.Envelope{}, ErrRefresh
	}
	scopes := fallbackScopes
	if raw := value.Extra("scope"); raw != nil {
		scopeText, ok := raw.(string)
		if !ok || strings.TrimSpace(scopeText) == "" {
			return token.Envelope{}, ErrRefresh
		}
		scopes = strings.Fields(scopeText)
	}
	scopes = append([]string(nil), scopes...)
	slices.Sort(scopes)
	scopes = slices.Compact(scopes)
	if len(scopes) == 0 {
		return token.Envelope{}, ErrRefresh
	}
	return token.Envelope{
		AccessToken: value.AccessToken, TokenType: "Bearer", RefreshToken: value.RefreshToken,
		ExpiresAt: value.Expiry.UTC(), Scopes: scopes,
	}, nil
}
