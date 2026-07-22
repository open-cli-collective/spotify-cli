package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/open-cli-collective/spotify-cli/internal/token"
)

const (
	// ScopeUserReadPrivate is the only scope requested by the initial CLI slice.
	ScopeUserReadPrivate = "user-read-private"
	defaultAuthorizeURL  = "https://accounts.spotify.com/authorize"
	defaultTokenURL      = "https://accounts.spotify.com/api/token" // #nosec G101 -- fixed public OAuth endpoint, not a credential.
	defaultAuthTimeout   = 5 * time.Minute
	maxCallbackBytes     = 16 << 10
)

var (
	// ErrInvalidCallback reports a malformed or unexpected redirected URL.
	ErrInvalidCallback = errors.New("invalid Spotify authorization callback")
	// ErrStateMismatch reports a callback that does not match this authorization attempt.
	ErrStateMismatch = errors.New("spotify authorization state did not match")
	// ErrAccessDenied reports a denied Spotify authorization request.
	ErrAccessDenied = errors.New("spotify authorization was denied")
	// ErrAuthorizationTimeout reports an authorization callback timeout.
	ErrAuthorizationTimeout = errors.New("spotify authorization timed out")
	// ErrExchange reports a failed authorization-code exchange.
	ErrExchange = errors.New("exchanging Spotify authorization code failed")
)

// Endpoints selects Spotify OAuth endpoints. Production uses the zero value.
type Endpoints struct {
	AuthorizeURL string
	TokenURL     string
}

// Authorizer performs one Authorization Code with PKCE flow.
type Authorizer struct {
	HTTPClient  *http.Client
	Endpoints   Endpoints
	OpenBrowser func(string) error
	Timeout     time.Duration
}

// Request contains one authorization attempt's non-secret inputs and I/O.
type Request struct {
	ClientID      string
	RedirectURI   string
	NoBrowser     bool
	AuthCodeStdin bool
	In            io.Reader
	ErrOut        io.Writer
}

// Authorize obtains and validates a Spotify OAuth token envelope.
func (authorizer Authorizer) Authorize(ctx context.Context, request Request) (token.Envelope, error) {
	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return token.Envelope{}, errors.New("generating Spotify authorization state failed")
	}

	redirectURI := request.RedirectURI
	var listener net.Listener
	if !request.AuthCodeStdin {
		listener, redirectURI, err = listenRedirect(request.RedirectURI)
		if err != nil {
			return token.Envelope{}, err
		}
		defer func() { _ = listener.Close() }()
	}

	config := authorizer.oauthConfig(request.ClientID, redirectURI)
	authURL := config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	if _, err := fmt.Fprintf(writerOrDiscard(request.ErrOut), "Authorization URL: %s\n", authURL); err != nil {
		return token.Envelope{}, errors.New("writing authorization URL failed")
	}
	if !request.NoBrowser && !request.AuthCodeStdin && authorizer.OpenBrowser != nil {
		if err := authorizer.OpenBrowser(authURL); err != nil {
			if _, writeErr := fmt.Fprintln(writerOrDiscard(request.ErrOut), "Could not open browser automatically; use the authorization URL above."); writeErr != nil {
				return token.Envelope{}, errors.New("writing browser warning failed")
			}
		}
	}

	var code string
	if request.AuthCodeStdin {
		code, err = readRedirect(request.In, redirectURI, state)
	} else {
		code, err = waitForCallback(ctx, listener, redirectURI, state, authorizer.timeout())
	}
	if err != nil {
		return token.Envelope{}, err
	}

	exchangeContext := ctx
	if authorizer.HTTPClient != nil {
		exchangeContext = context.WithValue(ctx, oauth2.HTTPClient, authorizer.HTTPClient)
	}
	value, err := config.Exchange(exchangeContext, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return token.Envelope{}, ErrExchange
	}
	envelope, err := fromOAuthToken(value, []string{ScopeUserReadPrivate})
	if err != nil || envelope.RefreshToken == "" {
		return token.Envelope{}, ErrExchange
	}
	return envelope, nil
}

func (authorizer Authorizer) oauthConfig(clientID, redirectURI string) oauth2.Config {
	authorizeURL := authorizer.Endpoints.AuthorizeURL
	if authorizeURL == "" {
		authorizeURL = defaultAuthorizeURL
	}
	tokenURL := authorizer.Endpoints.TokenURL
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}
	return oauth2.Config{
		ClientID: clientID, RedirectURL: redirectURI, Scopes: []string{ScopeUserReadPrivate},
		Endpoint: oauth2.Endpoint{AuthURL: authorizeURL, TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
}

func (authorizer Authorizer) timeout() time.Duration {
	if authorizer.Timeout > 0 {
		return authorizer.Timeout
	}
	return defaultAuthTimeout
}

func randomState() (string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func listenRedirect(raw string) (net.Listener, string, error) {
	redirect, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(redirect.Scheme, "http") || redirect.Hostname() != "127.0.0.1" {
		return nil, "", fmt.Errorf("%w: callback listener requires an http://127.0.0.1 redirect; use --auth-code-stdin otherwise", ErrInvalidCallback)
	}
	address := net.JoinHostPort("127.0.0.1", redirect.Port())
	listener, err := net.Listen("tcp4", address) // #nosec G102 -- OAuth callback is bound only to IPv4 loopback.
	if err != nil {
		return nil, "", errors.New("starting Spotify callback listener failed")
	}
	redirect.Host = listener.Addr().String()
	return listener, redirect.String(), nil
}

type callbackResult struct {
	code string
	err  error
}

func waitForCallback(ctx context.Context, listener net.Listener, rawRedirect, expectedState string, timeout time.Duration) (string, error) {
	redirect, err := url.Parse(rawRedirect)
	if err != nil {
		return "", ErrInvalidCallback
	}
	results := make(chan callbackResult, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.Host != redirect.Host || normalizedPath(request.URL) != normalizedPath(redirect) {
			http.Error(w, "Invalid authorization callback.", http.StatusBadRequest)
			return
		}
		result := parseCallback(request.URL.Query(), expectedState)
		if result.err != nil {
			http.Error(w, "Authorization could not be completed. Return to the terminal.", http.StatusBadRequest)
		} else {
			_, _ = io.WriteString(w, "Authorization complete. Return to the terminal.\n")
		}
		select {
		case results <- result:
		default:
		}
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case result := <-results:
		return result.code, result.err
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return "", ErrAuthorizationTimeout
		}
		return "", errors.New("spotify callback listener failed")
	case <-waitContext.Done():
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ErrAuthorizationTimeout
	}
}

func readRedirect(input io.Reader, expectedRedirect, expectedState string) (string, error) {
	if input == nil {
		return "", ErrInvalidCallback
	}
	data, err := io.ReadAll(io.LimitReader(input, maxCallbackBytes+1))
	if err != nil || len(data) > maxCallbackBytes {
		return "", ErrInvalidCallback
	}
	callback, err := url.Parse(strings.TrimSpace(string(data)))
	if err != nil || !callback.IsAbs() || callback.Fragment != "" {
		return "", ErrInvalidCallback
	}
	expected, err := url.Parse(expectedRedirect)
	if err != nil || !strings.EqualFold(callback.Scheme, expected.Scheme) || !strings.EqualFold(callback.Host, expected.Host) || normalizedPath(callback) != normalizedPath(expected) {
		return "", ErrInvalidCallback
	}
	result := parseCallback(callback.Query(), expectedState)
	return result.code, result.err
}

func normalizedPath(value *url.URL) string {
	if value.EscapedPath() == "" {
		return "/"
	}
	return value.EscapedPath()
}

func parseCallback(query url.Values, expectedState string) callbackResult {
	states, stateOK := query["state"]
	if !stateOK || len(states) != 1 || subtle.ConstantTimeCompare([]byte(states[0]), []byte(expectedState)) != 1 {
		return callbackResult{err: ErrStateMismatch}
	}
	codes, hasCode := query["code"]
	callbackErrors, hasError := query["error"]
	if hasError {
		if len(callbackErrors) != 1 || callbackErrors[0] == "" || hasCode {
			return callbackResult{err: ErrInvalidCallback}
		}
		return callbackResult{err: ErrAccessDenied}
	}
	if !hasCode || len(codes) != 1 || codes[0] == "" {
		return callbackResult{err: ErrInvalidCallback}
	}
	return callbackResult{code: codes[0]}
}

func writerOrDiscard(writer io.Writer) io.Writer {
	if writer == nil {
		return io.Discard
	}
	return writer
}
