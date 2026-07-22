package mecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/open-cli-collective/cli-common/statedirtest"

	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/session"
	"github.com/open-cli-collective/spotify-cli/internal/token"
)

func TestMeRendersIdentity(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-secret" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"account_id":"account","display_name":"Name","id":"spotify-id","uri":"spotify:user:spotify-id"}`))
	}))
	defer server.Close()

	harness := newHarness(t, now, server)
	harness.storeEnvelope(t, token.Envelope{
		AccessToken: "access-secret", TokenType: "Bearer", RefreshToken: "refresh-secret",
		ExpiresAt: now.Add(time.Hour), Scopes: []string{"user-read-private"},
	})
	stdout, err := harness.execute()
	if err != nil {
		t.Fatal(err)
	}
	want := "account_id\taccount\ndisplay_name\tName\nspotify_id\tspotify-id\nuri\tspotify:user:spotify-id\nscopes\tuser-read-private\n"
	if stdout != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestMeJSON(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"account_id":"account","display_name":null,"id":"spotify-id","uri":"spotify:user:spotify-id"}`))
	}))
	defer server.Close()
	harness := newHarness(t, now, server)
	harness.storeEnvelope(t, token.Envelope{
		AccessToken: "access", TokenType: "Bearer", RefreshToken: "refresh",
		ExpiresAt: now.Add(time.Hour), Scopes: []string{"user-read-private"},
	})
	stdout, err := harness.execute("--json")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result["account_id"] != "account" || result["display_name"] != "" {
		t.Fatalf("JSON = %v", result)
	}
}

func TestMeRefreshesAndPersistsSameCredential(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("client_id") != "client-id" || r.Form.Get("refresh_token") != "old-refresh" {
				t.Fatalf("refresh form = %v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"new-access","token_type":"Bearer","expires_in":3600,"scope":"playlist-read-private user-read-private"}`))
		case "/v1/me":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"account_id":"account"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	harness := newHarness(t, now, server)
	harness.tokenURL = server.URL + "/token"
	harness.apiBaseURL = server.URL + "/v1"
	harness.storeEnvelope(t, token.Envelope{
		AccessToken: "old-access", TokenType: "Bearer", RefreshToken: "old-refresh",
		ExpiresAt: now.Add(-time.Minute), Scopes: []string{"user-read-private"},
	})
	stdout, err := harness.execute()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := token.Decode([]byte(harness.store.values["default/"+credentials.OAuthTokenKey]), now)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "new-access" || stored.RefreshToken != "old-refresh" || strings.Join(stored.Scopes, ",") != "playlist-read-private,user-read-private" {
		t.Fatalf("stored token = %+v", stored)
	}
	if !strings.Contains(stdout, "scopes\tplaylist-read-private,user-read-private\n") {
		t.Fatalf("stdout = %q", stdout)
	}
	if harness.store.setCalls != 1 || !harness.store.overwrite {
		t.Fatalf("set calls = %d overwrite = %t", harness.store.setCalls, harness.store.overwrite)
	}
}

func TestMeFailureClasses(t *testing.T) {
	now := time.Now().UTC()
	t.Run("missing client ID", func(t *testing.T) {
		harness := newHarness(t, now, nil)
		cfg := config.Default()
		if err := config.Save(harness.scope, cfg); err != nil {
			t.Fatal(err)
		}
		_, err := harness.execute()
		if exitcode.Code(err) != exitcode.Config {
			t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
		}
	})
	t.Run("missing credential", func(t *testing.T) {
		harness := newHarness(t, now, nil)
		_, err := harness.execute()
		if exitcode.Code(err) != exitcode.Config {
			t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
		}
	})
	t.Run("insufficient scope", func(t *testing.T) {
		harness := newHarness(t, now, nil)
		harness.storeEnvelope(t, token.Envelope{
			AccessToken: "access", TokenType: "Bearer", RefreshToken: "refresh",
			ExpiresAt: now.Add(time.Hour), Scopes: []string{"other-scope"},
		})
		_, err := harness.execute()
		if exitcode.Code(err) != exitcode.Config {
			t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
		}
	})
	t.Run("invalid stored credential", func(t *testing.T) {
		harness := newHarness(t, now, nil)
		harness.store.values["default/"+credentials.OAuthTokenKey] = "invalid-token-secret-sentinel"
		stdout, err := harness.execute()
		if exitcode.Code(err) != exitcode.Config || stdout != "" || strings.Contains(err.Error(), "invalid-token-secret-sentinel") {
			t.Fatalf("stdout=%q error=%v code=%d", stdout, err, exitcode.Code(err))
		}
	})
	for _, test := range []struct {
		name   string
		status int
		code   int
	}{
		{"unauthorized", http.StatusUnauthorized, exitcode.Config},
		{"forbidden", http.StatusForbidden, exitcode.Config},
		{"server error", http.StatusInternalServerError, exitcode.Upstream},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, "response-secret-sentinel")
			}))
			defer server.Close()
			harness := newHarness(t, now, server)
			harness.storeEnvelope(t, token.Envelope{
				AccessToken: "access", TokenType: "Bearer", RefreshToken: "refresh",
				ExpiresAt: now.Add(time.Hour), Scopes: []string{"user-read-private"},
			})
			stdout, err := harness.execute()
			if exitcode.Code(err) != test.code || stdout != "" || strings.Contains(err.Error(), "response-secret-sentinel") {
				t.Fatalf("stdout=%q error=%v code=%d", stdout, err, exitcode.Code(err))
			}
		})
	}
	t.Run("invalid grant", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/token" {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":"invalid_grant","error_description":"grant-secret-sentinel"}`)
		}))
		defer server.Close()
		harness := newHarness(t, now, server)
		harness.tokenURL = server.URL + "/token"
		harness.storeEnvelope(t, token.Envelope{
			AccessToken: "old-access", TokenType: "Bearer", RefreshToken: "old-refresh",
			ExpiresAt: now.Add(-time.Minute), Scopes: []string{"user-read-private"},
		})
		stdout, err := harness.execute()
		if exitcode.Code(err) != exitcode.Config || stdout != "" || strings.Contains(err.Error(), "grant-secret-sentinel") {
			t.Fatalf("stdout=%q error=%v code=%d", stdout, err, exitcode.Code(err))
		}
	})
	t.Run("upstream unavailable", func(t *testing.T) {
		harness := newHarness(t, now, nil)
		harness.httpClient = &http.Client{Transport: failingTransport{}}
		harness.storeEnvelope(t, token.Envelope{
			AccessToken: "access", TokenType: "Bearer", RefreshToken: "refresh",
			ExpiresAt: now.Add(time.Hour), Scopes: []string{"user-read-private"},
		})
		_, err := harness.execute()
		if exitcode.Code(err) != exitcode.Upstream {
			t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
		}
	})
}

func TestMeReportsOutputWriterFailure(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"account_id":"account"}`)
	}))
	defer server.Close()
	harness := newHarness(t, now, server)
	harness.storeEnvelope(t, token.Envelope{
		AccessToken: "access", TokenType: "Bearer", RefreshToken: "refresh",
		ExpiresAt: now.Add(time.Hour), Scopes: []string{"user-read-private"},
	})
	err := harness.executeTo(failingWriter{})
	if exitcode.Code(err) != exitcode.Generic {
		t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
	}
}

type meHarness struct {
	scope      statedir.Scope
	store      *memoryStore
	now        time.Time
	httpClient *http.Client
	tokenURL   string
	apiBaseURL string
	backend    string
}

func newHarness(t *testing.T, now time.Time, server *httptest.Server) *meHarness {
	t.Helper()
	statedirtest.Hermetic(t)
	scope := statedir.Scope{Name: config.Service}
	cfg := config.Default()
	cfg.ClientID = "client-id"
	if err := config.Save(scope, cfg); err != nil {
		t.Fatal(err)
	}
	harness := &meHarness{scope: scope, store: &memoryStore{values: map[string]string{}}, now: now}
	if server != nil {
		harness.httpClient = server.Client()
		harness.apiBaseURL = server.URL + "/v1"
	}
	return harness
}

func (harness *meHarness) storeEnvelope(t *testing.T, value token.Envelope) {
	t.Helper()
	encoded, err := token.Encode(value, harness.now)
	if err != nil {
		t.Fatal(err)
	}
	harness.store.values["default/"+credentials.OAuthTokenKey] = string(encoded)
}

func (harness *meHarness) execute(args ...string) (string, error) {
	var stdout bytes.Buffer
	err := harness.executeTo(&stdout, args...)
	return stdout.String(), err
}

func (harness *meHarness) executeTo(stdout io.Writer, args ...string) error {
	opener := session.Opener{
		Scope: harness.scope,
		OpenStore: func(credentials.OpenRequest) (session.CredentialStore, error) {
			return harness.store, nil
		},
		Now: func() time.Time { return harness.now }, HTTPClient: harness.httpClient,
		TokenURL: harness.tokenURL, APIBaseURL: harness.apiBaseURL,
	}
	command := New(Dependencies{
		OpenSession: func(ctx context.Context, backend string, backendSet bool) (Session, error) {
			return opener.Open(ctx, backend, backendSet)
		},
		Backend: &harness.backend,
	})
	command.SetOut(stdout)
	command.SetErr(&bytes.Buffer{})
	command.SilenceUsage = true
	command.SetArgs(args)
	return command.ExecuteContext(context.Background())
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("writer failed") }

type memoryStore struct {
	values    map[string]string
	setCalls  int
	overwrite bool
}

func (store *memoryStore) Close() error { return nil }
func (store *memoryStore) Get(profile, key string) (string, error) {
	value, ok := store.values[profile+"/"+key]
	if !ok {
		return "", credstore.ErrNotFound
	}
	return value, nil
}
func (store *memoryStore) Set(profile, key, value string, opts ...credstore.SetOpt) error {
	store.setCalls++
	store.overwrite = len(opts) > 0
	store.values[profile+"/"+key] = value
	return nil
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}
