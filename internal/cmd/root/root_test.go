package root

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/open-cli-collective/cli-common/statedirtest"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
)

type fakeStore struct {
	values  map[string]string
	backend credstore.Backend
	source  credstore.Source
	setErr  error
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("writer failed") }

func (s *fakeStore) Backend() (credstore.Backend, credstore.Source) { return s.backend, s.source }
func (s *fakeStore) Close() error                                   { return nil }
func (s *fakeStore) Get(profile, key string) (string, error) {
	value, ok := s.values[profile+"/"+key]
	if !ok {
		return "", credstore.ErrNotFound
	}
	return value, nil
}
func (s *fakeStore) Set(profile, key, value string, opts ...credstore.SetOpt) error {
	if s.setErr != nil {
		return s.setErr
	}
	item := profile + "/" + key
	if _, ok := s.values[item]; ok && len(opts) == 0 {
		return credstore.ErrExists
	}
	s.values[item] = value
	return nil
}
func (s *fakeStore) Delete(profile, key string) error {
	item := profile + "/" + key
	if _, ok := s.values[item]; !ok {
		return credstore.ErrNotFound
	}
	delete(s.values, item)
	return nil
}
func (s *fakeStore) Exists(profile, key string) (bool, error) {
	_, ok := s.values[profile+"/"+key]
	return ok, nil
}

type harness struct {
	in       *bytes.Buffer
	out      *bytes.Buffer
	errOut   *bytes.Buffer
	store    *fakeStore
	requests []credentials.OpenRequest
	deps     Dependencies
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	statedirtest.Hermetic(t)
	h := &harness{
		in:     &bytes.Buffer{},
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
		store: &fakeStore{
			values:  map[string]string{},
			backend: credstore.BackendMemory,
			source:  credstore.SourceExplicit,
		},
	}
	h.deps = Dependencies{
		In:     h.in,
		Out:    h.out,
		ErrOut: h.errOut,
		Scope:  statedir.Scope{Name: config.Service},
		Cache:  statedir.Cache{Tool: config.Tool},
		Data:   statedir.Data{Tool: config.Tool},
		Now:    func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) },
		OpenStore: func(request credentials.OpenRequest) (credentials.Store, error) {
			h.requests = append(h.requests, request)
			return h.store, nil
		},
	}
	return h
}

func (h *harness) execute(args ...string) error {
	cmd := New(h.deps)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestVersion(t *testing.T) {
	h := newHarness(t)
	if err := h.execute("--version"); err != nil {
		t.Fatal(err)
	}
	if got := h.out.String(); !strings.HasPrefix(got, "sptfy dev ") {
		t.Fatalf("version output = %q", got)
	}
}

func TestUnknownCommandsAreUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"frobnicate"}, {"config", "frobnicate"}} {
		h := newHarness(t)
		err := h.execute(args...)
		if exitcode.Code(err) != exitcode.Usage {
			t.Fatalf("args %v: error = %v, code = %d", args, err, exitcode.Code(err))
		}
	}
}

func TestSearchTrackIsWiredToAuthenticatedSession(t *testing.T) {
	h := newHarness(t)
	cfg := config.Default()
	cfg.ClientID = "client-id"
	if err := config.Save(h.deps.Scope, cfg); err != nil {
		t.Fatal(err)
	}
	err := h.execute("search", "track", "query")
	if exitcode.Code(err) != exitcode.Config || len(h.requests) != 1 {
		t.Fatalf("error=%v code=%d store opens=%d", err, exitcode.Code(err), len(h.requests))
	}
}

func TestSearchTrackRejectsJSONBeforeSession(t *testing.T) {
	h := newHarness(t)
	err := h.execute("search", "track", "query", "--json")
	if exitcode.Code(err) != exitcode.Usage || len(h.requests) != 0 {
		t.Fatalf("error=%v code=%d store opens=%d", err, exitcode.Code(err), len(h.requests))
	}
}

func TestInitAndMeProductionCompositionRoutesOutput(t *testing.T) {
	h := newHarness(t)
	h.deps.Now = func() time.Time { return time.Now().UTC() }
	var meCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			if err := request.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			if request.Form.Get("client_id") != "client-id" || request.Form.Get("code") != "code" || request.Form.Get("client_secret") != "" {
				t.Errorf("token form = %v", request.Form)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"access_token":"access-token-canary","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-token-canary","scope":"user-read-private"}`)
		case "/v1/me":
			meCalls++
			if request.Header.Get("Authorization") != "Bearer access-token-canary" {
				t.Errorf("authorization header = %q", request.Header.Get("Authorization"))
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"account_id":"account-1","display_name":"Ada","id":"spotify-1","uri":"spotify:user:spotify-1"}`)
		case "/v1/search":
			if request.Header.Get("Authorization") != "Bearer access-token-canary" || request.URL.Query().Get("q") != "Ada" || request.URL.Query().Get("type") != "track" {
				t.Errorf("search authorization/query = %q %v", request.Header.Get("Authorization"), request.URL.Query())
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"tracks":{"items":[{"id":"track-1","name":"Song","artists":[{"id":"artist-1","name":"Ada"}],"album":{"id":"album-1","name":"Album"},"duration_ms":61000}],"limit":10,"offset":0,"total":1,"next":null}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	h.deps.HTTPClient = server.Client()
	h.deps.OAuthEndpoints = auth.Endpoints{AuthorizeURL: server.URL + "/authorize", TokenURL: server.URL + "/token"}
	h.deps.APIBaseURL = server.URL + "/v1"
	callbackErrors := make(chan error, 1)
	h.deps.OpenBrowser = func(rawAuthURL string) error {
		authURL, err := url.Parse(rawAuthURL)
		if err != nil {
			return err
		}
		callback, err := url.Parse(authURL.Query().Get("redirect_uri"))
		if err != nil {
			return err
		}
		query := callback.Query()
		query.Set("code", "code")
		query.Set("state", authURL.Query().Get("state"))
		callback.RawQuery = query.Encode()
		go func() {
			response, err := h.deps.HTTPClient.Get(callback.String())
			if err != nil {
				callbackErrors <- err
				return
			}
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != http.StatusOK {
				callbackErrors <- fmt.Errorf("callback status %d", response.StatusCode)
				return
			}
			callbackErrors <- nil
		}()
		return nil
	}

	if err := h.execute("init", "--non-interactive", "--client-id", "client-id"); err != nil {
		t.Fatal(err)
	}
	if err := <-callbackErrors; err != nil {
		t.Fatal(err)
	}
	if h.out.Len() != 0 {
		t.Fatalf("init stdout = %q", h.out.String())
	}
	if got := h.errOut.String(); !strings.Contains(got, "Authorization URL:") || !strings.Contains(got, "Authenticated as account-1.") || !strings.Contains(got, "Setup complete.") {
		t.Fatalf("init stderr = %q", got)
	}
	if strings.Contains(h.errOut.String(), "access-token-canary") || strings.Contains(h.errOut.String(), "refresh-token-canary") {
		t.Fatalf("init leaked token: %q", h.errOut.String())
	}

	h.out.Reset()
	h.errOut.Reset()
	if err := h.execute("me"); err != nil {
		t.Fatal(err)
	}
	if got := h.out.String(); got != "account_id\taccount-1\ndisplay_name\tAda\nspotify_id\tspotify-1\nuri\tspotify:user:spotify-1\nscopes\tuser-read-private\n" {
		t.Fatalf("me stdout = %q", got)
	}
	if h.errOut.Len() != 0 {
		t.Fatalf("me stderr = %q", h.errOut.String())
	}
	if meCalls != 2 {
		t.Fatalf("me calls = %d, want verify plus command", meCalls)
	}

	h.out.Reset()
	h.errOut.Reset()
	if err := h.execute("search", "track", "Ada"); err != nil {
		t.Fatal(err)
	}
	wantSearch := "ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\ntrack-1 | Song | artist-1 | Ada | album-1 | Album | 1:01\n"
	if h.out.String() != wantSearch || h.errOut.Len() != 0 {
		t.Fatalf("search stdout=%q stderr=%q", h.out.String(), h.errOut.String())
	}
}

func TestInitRequiresStdinModeForHTTPSCallback(t *testing.T) {
	h := newHarness(t)
	err := h.execute(
		"init", "--non-interactive", "--client-id", "client-id",
		"--redirect-uri", "https://callback.example/spotify", "--no-verify",
	)
	if exitcode.Code(err) != exitcode.Usage || !errors.Is(err, auth.ErrInvalidCallback) {
		t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
	}
}

func TestBackendValidationRunsForStoreFreeCommands(t *testing.T) {
	for _, backend := range []string{"definitely-invalid", "memory"} {
		for _, args := range [][]string{{"config", "path"}, {"search", "track", "query"}} {
			h := newHarness(t)
			err := h.execute(append([]string{"--backend", backend}, args...)...)
			if exitcode.Code(err) != exitcode.Usage {
				t.Fatalf("backend %q args %v: error = %v, code = %d", backend, args, err, exitcode.Code(err))
			}
			if len(h.requests) != 0 {
				t.Fatalf("backend %q args %v opened store", backend, args)
			}
		}
	}
}

func TestSetCredentialInvalidBackendJSONIsStructured(t *testing.T) {
	h := newHarness(t)
	h.deps.OpenStore = credentials.ProductionOpener(nil)
	h.in.WriteString(`{"version":1,"access_token":"access","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)
	err := h.execute("--backend", "memory", "set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token", "--stdin", "--json")
	if exitcode.Code(err) != exitcode.Usage || !exitcode.Quiet(err) {
		t.Fatalf("error = %v, code %d quiet %v", err, exitcode.Code(err), exitcode.Quiet(err))
	}
	if !strings.Contains(h.out.String(), `"backend":"","written":false,"error":"credential backend memory is test-only"`) {
		t.Fatalf("stdout = %q", h.out.String())
	}
	if h.in.Len() == 0 {
		t.Fatal("invalid backend consumed secret input")
	}
}

func TestSupportedButUnavailableBackendIsConfigError(t *testing.T) {
	h := newHarness(t)
	h.deps.OpenStore = func(credentials.OpenRequest) (credentials.Store, error) {
		return nil, fmt.Errorf("%w: unavailable on this platform", credstore.ErrBackendNotImplemented)
	}
	h.in.WriteString(`{"version":1,"access_token":"access","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)
	err := h.execute("--backend", "keychain", "set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token", "--stdin")
	if exitcode.Code(err) != exitcode.Config {
		t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
	}
}

func TestTextOutputWriterFailuresAreReported(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		stderr bool
		stdin  string
	}{
		{name: "config show", args: []string{"config", "show"}},
		{name: "config path", args: []string{"config", "path"}},
		{name: "config clear", args: []string{"config", "clear"}},
		{
			name:   "set credential",
			args:   []string{"set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token", "--stdin"},
			stderr: true,
			stdin:  `{"version":1,"access_token":"access","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.in.WriteString(tt.stdin)
			if tt.stderr {
				h.deps.ErrOut = failingWriter{}
			} else {
				h.deps.Out = failingWriter{}
			}
			err := h.execute(tt.args...)
			if exitcode.Code(err) != exitcode.Generic || exitcode.Quiet(err) {
				t.Fatalf("error = %v, code = %d quiet = %v", err, exitcode.Code(err), exitcode.Quiet(err))
			}
		})
	}
}

func TestClearPartialFailureKeepsConfigExitWhenRenderingFails(t *testing.T) {
	h := newHarness(t)
	dir, err := h.deps.Scope.ConfigDirEnsured()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte("unknown: value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.deps.Out = failingWriter{}
	err = h.execute("config", "clear", "--all")
	if exitcode.Code(err) != exitcode.Config {
		t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
	}
}

func TestConfigShowJSONDoesNotLeakCredential(t *testing.T) {
	h := newHarness(t)
	canary := "stored-super-secret-canary"
	h.store.values["default/oauth_token"] = canary

	if err := h.execute("config", "show", "--json"); err != nil {
		t.Fatal(err)
	}
	want := "{\"client_id\":\"\",\"redirect_uri\":\"http://127.0.0.1/callback\",\"credential_ref\":\"spotify-cli/default\",\"backend\":\"memory\",\"backend_source\":\"explicit\",\"oauth_token_present\":true,\"keyring\":{}}\n"
	if h.out.String() != want {
		t.Fatalf("stdout = %q, want %q", h.out.String(), want)
	}
	if strings.Contains(h.out.String()+h.errOut.String(), canary) {
		t.Fatal("config show leaked the credential")
	}
}

func TestConfigShowTextFixture(t *testing.T) {
	h := newHarness(t)
	if err := h.execute("config", "show"); err != nil {
		t.Fatal(err)
	}
	want := "client_id\t\nredirect_uri\thttp://127.0.0.1/callback\ncredential_ref\tspotify-cli/default\nbackend\tmemory\nbackend_source\texplicit\noauth_token_present\tfalse\n"
	if h.out.String() != want {
		t.Fatalf("stdout = %q, want %q", h.out.String(), want)
	}
}

func TestConfigPathJSON(t *testing.T) {
	h := newHarness(t)
	if err := h.execute("config", "path", "--json"); err != nil {
		t.Fatal(err)
	}
	configPath, _ := config.Path(h.deps.Scope)
	cachePath, _ := h.deps.Cache.CacheDir()
	dataPath, _ := h.deps.Data.DataDir()
	want := "{\"config\":\"" + configPath + "\",\"cache\":\"" + cachePath + "\",\"data\":\"" + dataPath + "\"}\n"
	if h.out.String() != want {
		t.Fatalf("stdout = %q, want %q", h.out.String(), want)
	}
}

func TestSetCredentialJSONCanonicalizesWithoutLeaking(t *testing.T) {
	h := newHarness(t)
	canary := "access-super-secret-canary"
	h.in.WriteString(`{"version":1,"access_token":"` + canary + `","token_type":"bearer","refresh_token":"refresh-super-secret-canary","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)

	err := h.execute("set-credential", "--ref", "spotify-cli/work", "--key", "oauth_token", "--stdin", "--json")
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"ref\":\"spotify-cli/work\",\"key\":\"oauth_token\",\"backend\":\"memory\",\"written\":true}\n"
	if h.out.String() != want {
		t.Fatalf("stdout = %q, want %q", h.out.String(), want)
	}
	if strings.Contains(h.out.String()+h.errOut.String(), canary) {
		t.Fatal("set-credential output leaked the credential")
	}
	stored := h.store.values["work/oauth_token"]
	if !strings.Contains(stored, canary) || !strings.Contains(stored, `"token_type":"Bearer"`) {
		t.Fatalf("stored envelope was not canonicalized: %q", stored)
	}
}

func TestSetCredentialJSONFailureIsQuietAndSecretSafe(t *testing.T) {
	h := newHarness(t)
	canary := "access-super-secret-canary"
	h.in.WriteString(`{"version":1,"access_token":"` + canary + `","token_type":"Basic","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)

	err := h.execute("set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token", "--stdin", "--json")
	if exitcode.Code(err) != exitcode.Usage || !exitcode.Quiet(err) {
		t.Fatalf("error = %v, code %d quiet %v", err, exitcode.Code(err), exitcode.Quiet(err))
	}
	want := "{\"ref\":\"spotify-cli/default\",\"key\":\"oauth_token\",\"backend\":\"\",\"written\":false,\"error\":\"oauth token envelope token_type must be Bearer\"}\n"
	if h.out.String() != want {
		t.Fatalf("stdout = %q, want %q", h.out.String(), want)
	}
	if strings.Contains(h.out.String()+h.errOut.String()+err.Error(), canary) {
		t.Fatal("set-credential failure leaked the credential")
	}
	if len(h.requests) != 0 {
		t.Fatal("store opened before envelope validation")
	}
}

func TestSetCredentialJSONFailurePropagatesWriterError(t *testing.T) {
	h := newHarness(t)
	h.deps.Out = failingWriter{}
	canary := "access-super-secret-canary"
	h.in.WriteString(`{"version":1,"access_token":"` + canary + `","token_type":"Basic","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)

	err := h.execute("set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token", "--stdin", "--json")
	if exitcode.Code(err) != exitcode.Generic || exitcode.Quiet(err) {
		t.Fatalf("error = %v, code %d quiet %v", err, exitcode.Code(err), exitcode.Quiet(err))
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatal("writer failure leaked the credential")
	}
}

func TestSetCredentialRejectsOtherServiceBeforeOpen(t *testing.T) {
	h := newHarness(t)
	h.in.WriteString(`{"version":1,"access_token":"access","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)
	err := h.execute("set-credential", "--ref", "other/default", "--key", "oauth_token", "--stdin")
	if exitcode.Code(err) != exitcode.Usage || len(h.requests) != 0 {
		t.Fatalf("error code = %d, store opens = %d", exitcode.Code(err), len(h.requests))
	}
}

func TestSetCredentialRejectsDisallowedKeyBeforeSecretInput(t *testing.T) {
	h := newHarness(t)
	h.in.WriteString("secret input must remain unread")
	err := h.execute("set-credential", "--ref", "spotify-cli/default", "--key", "other", "--stdin")
	if exitcode.Code(err) != exitcode.Usage {
		t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
	}
	if h.in.Len() == 0 || len(h.requests) != 0 {
		t.Fatalf("input remaining = %d, store opens = %d", h.in.Len(), len(h.requests))
	}
}

func TestSetCredentialStoreErrorsAreRedacted(t *testing.T) {
	for _, ingress := range []string{"stdin", "env"} {
		t.Run(ingress, func(t *testing.T) {
			h := newHarness(t)
			canary := "access-super-secret-canary"
			raw := `{"version":1,"access_token":"` + canary + `","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`
			h.store.setErr = errors.New("backend failed while handling " + canary)
			args := []string{"set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token"}
			if ingress == "stdin" {
				h.in.WriteString(raw)
				args = append(args, "--stdin")
			} else {
				const envName = "SPOTIFY_IMPORT_FAILURE"
				t.Setenv(envName, raw)
				args = append(args, "--from-env", envName)
			}
			err := h.execute(args...)
			if err == nil {
				t.Fatal("set-credential unexpectedly succeeded")
			}
			if strings.Contains(h.out.String()+h.errOut.String()+err.Error(), canary) {
				t.Fatalf("credential leaked: stdout=%q stderr=%q error=%q", h.out.String(), h.errOut.String(), err)
			}
		})
	}
}

func TestSetCredentialWrappedSentinelsAreRedactedAndClassified(t *testing.T) {
	for _, tt := range []struct {
		name  string
		cause error
		code  int
	}{
		{name: "exists", cause: credstore.ErrExists, code: exitcode.Generic},
		{name: "key not allowed", cause: credstore.ErrKeyNotAllowed, code: exitcode.Usage},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			canary := "wrapped-sentinel-secret-canary"
			h.store.setErr = fmt.Errorf("backend echoed %s: %w", canary, tt.cause)
			h.in.WriteString(`{"version":1,"access_token":"` + canary + `","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)
			err := h.execute("set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token", "--stdin", "--json")
			if exitcode.Code(err) != tt.code || !errors.Is(err, tt.cause) {
				t.Fatalf("error = %v, code = %d, matches = %v", err, exitcode.Code(err), errors.Is(err, tt.cause))
			}
			if strings.Contains(h.out.String()+h.errOut.String()+err.Error(), canary) {
				t.Fatalf("secret leaked: stdout=%q stderr=%q error=%q", h.out.String(), h.errOut.String(), err)
			}
		})
	}
}

func TestBackendFlagIsPassedAsExplicit(t *testing.T) {
	h := newHarness(t)
	cfg := config.Default()
	cfg.Keyring.Backend = "file"
	if err := config.Save(h.deps.Scope, cfg); err != nil {
		t.Fatal(err)
	}
	if err := h.execute("--backend", "pass", "config", "show"); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 1 || h.requests[0].Backend != "pass" || !h.requests[0].BackendSet || h.requests[0].Config.Keyring.Backend != "file" {
		t.Fatalf("open requests = %+v", h.requests)
	}
}

func TestConfigShowReportsFilePassphraseSource(t *testing.T) {
	for _, tt := range []struct {
		name   string
		value  string
		source string
	}{
		{name: "prompt", source: "prompt"},
		{name: "environment", value: "backend-passphrase", source: "environment"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.store.backend = credstore.BackendFile
			t.Setenv("SPOTIFY_CLI_KEYRING_PASSPHRASE", tt.value)
			if err := h.execute("config", "show", "--json"); err != nil {
				t.Fatal(err)
			}
			want := `"passphrase_source":"` + tt.source + `"`
			if !strings.Contains(h.out.String(), want) {
				t.Fatalf("stdout %q missing %q", h.out.String(), want)
			}
		})
	}
}

func TestSetCredentialFromEnvironment(t *testing.T) {
	h := newHarness(t)
	const envName = "SPOTIFY_IMPORT_ENVELOPE"
	t.Setenv(envName, `{"version":1,"access_token":"access","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)
	if err := h.execute("set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token", "--from-env", envName); err != nil {
		t.Fatal(err)
	}
	if h.store.values["default/oauth_token"] == "" {
		t.Fatal("credential was not stored")
	}
}

func TestSetCredentialTextFixture(t *testing.T) {
	h := newHarness(t)
	h.in.WriteString(`{"version":1,"access_token":"access","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)
	if err := h.execute("set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token", "--stdin"); err != nil {
		t.Fatal(err)
	}
	if h.out.Len() != 0 || h.errOut.String() != "wrote oauth_token to spotify-cli/default via memory\n" {
		t.Fatalf("stdout = %q, stderr = %q", h.out.String(), h.errOut.String())
	}
}

func TestConfigClearAllDryRunDoesNotOpenOrMutate(t *testing.T) {
	h := newHarness(t)
	if err := config.Save(h.deps.Scope, config.Default()); err != nil {
		t.Fatal(err)
	}
	configPath, _ := config.Path(h.deps.Scope)
	cachePath, _ := h.deps.Cache.CacheDirEnsured()
	cacheFile := filepath.Join(cachePath, "cache")
	if err := os.WriteFile(cacheFile, []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := h.execute("config", "clear", "--all", "--dry-run", "--json"); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 0 {
		t.Fatal("dry-run opened the credential store")
	}
	for _, path := range []string{configPath, cacheFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("dry-run mutated %s: %v", path, err)
		}
	}
}

func TestConfigClearDryRunReportsOnlyActiveRef(t *testing.T) {
	h := newHarness(t)
	cfg := config.Default()
	cfg.CredentialRef = "spotify-cli/work"
	if err := config.Save(h.deps.Scope, cfg); err != nil {
		t.Fatal(err)
	}
	h.store.values["work/oauth_token"] = "active"
	h.store.values["other/oauth_token"] = "inactive"
	if err := h.execute("config", "clear", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if got := h.out.String(); got != "would_remove\tcredential\tspotify-cli/work/oauth_token\n" {
		t.Fatalf("stdout = %q", got)
	}
	if h.store.values["work/oauth_token"] != "active" || h.store.values["other/oauth_token"] != "inactive" || len(h.requests) != 0 {
		t.Fatalf("dry-run mutated/opened store: values=%v opens=%d", h.store.values, len(h.requests))
	}
}

func TestConfigClearAllRecoversFromMalformedConfig(t *testing.T) {
	fixtures := map[string]string{
		"yaml":    "unknown: value\n",
		"ref":     "credential_ref: other/default\n",
		"backend": "keyring:\n  backend: unknown\n",
	}
	for name, contents := range fixtures {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry-run=%t", name, dryRun), func(t *testing.T) {
				h := newHarness(t)
				dir, _ := h.deps.Scope.ConfigDirEnsured()
				configPath := filepath.Join(dir, config.FileName)
				if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
				cachePath, _ := h.deps.Cache.CacheDirEnsured()
				if err := os.WriteFile(filepath.Join(cachePath, "cache"), []byte("cache"), 0o600); err != nil {
					t.Fatal(err)
				}

				args := []string{"config", "clear", "--all", "--json"}
				if dryRun {
					args = append(args, "--dry-run")
				}
				err := h.execute(args...)
				if exitcode.Code(err) != exitcode.Config {
					t.Fatalf("error = %v, code %d", err, exitcode.Code(err))
				}
				if len(h.requests) != 0 {
					t.Fatal("malformed config opened the credential store")
				}
				wantState := "removed"
				if dryRun {
					wantState = "would_remove"
					for _, path := range []string{configPath, cachePath} {
						if _, err := os.Stat(path); err != nil {
							t.Fatalf("dry-run mutated %s: %v", path, err)
						}
					}
				} else {
					for _, path := range []string{configPath, cachePath} {
						if _, err := os.Stat(path); !os.IsNotExist(err) {
							t.Fatalf("recovery did not remove %s: %v", path, err)
						}
					}
				}
				wantParts := []string{`"status":"skipped","type":"credential","target":"active/oauth_token"`, `"status":"` + wantState + `","type":"config"`, `"status":"` + wantState + `","type":"cache"`}
				for _, part := range wantParts {
					if !strings.Contains(h.out.String(), part) {
						t.Fatalf("stdout %q missing %q", h.out.String(), part)
					}
				}
			})
		}
	}
}

func TestConfigClearOnlyActiveCredential(t *testing.T) {
	h := newHarness(t)
	h.store.values["default/oauth_token"] = "active-canary"
	h.store.values["other/oauth_token"] = "other-canary"

	if err := h.execute("config", "clear"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.store.values["default/oauth_token"]; ok {
		t.Fatal("active credential remains")
	}
	if h.store.values["other/oauth_token"] != "other-canary" {
		t.Fatal("inactive credential was touched")
	}
	if got := h.out.String(); got != "removed\tcredential\tspotify-cli/default/oauth_token\n" {
		t.Fatalf("stdout = %q", got)
	}

	h.out.Reset()
	if err := h.execute("config", "clear"); err != nil {
		t.Fatal(err)
	}
	if got := h.out.String(); got != "absent\tcredential\tspotify-cli/default/oauth_token\n" {
		t.Fatalf("second clear stdout = %q", got)
	}
}

func TestSetCredentialExistingRequiresOverwrite(t *testing.T) {
	h := newHarness(t)
	h.store.values["default/oauth_token"] = "old"
	h.in.WriteString(`{"version":1,"access_token":"new","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)
	err := h.execute("set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token", "--stdin")
	if !errors.Is(err, credstore.ErrExists) || exitcode.Code(err) != exitcode.Generic {
		t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
	}
}

func TestSetCredentialOverwriteSucceeds(t *testing.T) {
	h := newHarness(t)
	h.store.values["default/oauth_token"] = "old"
	h.in.WriteString(`{"version":1,"access_token":"new","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`)
	if err := h.execute("set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token", "--stdin", "--overwrite"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.store.values["default/oauth_token"], `"access_token":"new"`) {
		t.Fatalf("stored credential = %q", h.store.values["default/oauth_token"])
	}
}

func TestConfigClearAllPreservesData(t *testing.T) {
	h := newHarness(t)
	if err := config.Save(h.deps.Scope, config.Default()); err != nil {
		t.Fatal(err)
	}
	dataPath, err := h.deps.Data.DataDirEnsured()
	if err != nil {
		t.Fatal(err)
	}
	dataFile := filepath.Join(dataPath, "history")
	if err := os.WriteFile(dataFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.execute("config", "clear", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataFile); err != nil {
		t.Fatalf("config clear --all removed data: %v", err)
	}
}
