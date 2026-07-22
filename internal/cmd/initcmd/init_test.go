package initcmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/open-cli-collective/cli-common/statedirtest"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/token"
)

func TestInteractiveInitPromptsVerifiesThenCommits(t *testing.T) {
	harness := newInitHarness(t)
	harness.interactive = true
	harness.prompt = func(input *Setup) error {
		harness.events = append(harness.events, "prompt")
		input.ClientID = "client-id"
		input.Backend = "file"
		return nil
	}
	harness.authorize = func(_ context.Context, request auth.Request) (token.Envelope, error) {
		harness.events = append(harness.events, "authorize")
		if request.ClientID != "client-id" || request.RedirectURI != config.DefaultRedirectURI {
			t.Fatalf("authorize request = %+v", request)
		}
		return harness.envelope(), nil
	}
	harness.verify = func(_ context.Context, cfg config.Config, _ token.Envelope) (client.User, error) {
		harness.events = append(harness.events, "verify")
		if cfg.ClientID != "client-id" || cfg.Keyring.Backend != "file" {
			t.Fatalf("verify config = %+v", cfg)
		}
		return client.User{AccountID: "account"}, nil
	}
	if err := harness.execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(harness.events, ","); got != "prompt,authorize,verify,set,save" {
		t.Fatalf("events = %s", got)
	}
	if !strings.Contains(harness.stderr.String(), "Authenticated as account") || harness.stdout.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", harness.stdout.String(), harness.stderr.String())
	}
	loaded, err := config.Load(harness.scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientID != "client-id" || loaded.Keyring.Backend != "file" {
		t.Fatalf("saved config = %+v", loaded)
	}
}

func TestNonInteractiveInitHasFlagParityAndNeverPrompts(t *testing.T) {
	harness := newInitHarness(t)
	harness.prompt = func(*Setup) error { t.Fatal("non-interactive init prompted"); return nil }
	harness.verify = func(context.Context, config.Config, token.Envelope) (client.User, error) {
		t.Fatal("--no-verify invoked verifier")
		return client.User{}, nil
	}
	harness.authorize = func(_ context.Context, request auth.Request) (token.Envelope, error) {
		if !request.NoBrowser || !request.AuthCodeStdin || request.ClientID != "client-id" || request.RedirectURI != "https://callback.example/spotify" {
			t.Fatalf("authorize request = %+v", request)
		}
		return harness.envelope(), nil
	}
	if err := harness.execute(
		"--non-interactive", "--client-id", "client-id", "--redirect-uri", "https://callback.example/spotify",
		"--credential-ref", "spotify-cli/automation", "--no-browser", "--auth-code-stdin", "--no-verify",
	); err != nil {
		t.Fatal(err)
	}
	if _, ok := harness.store.values["automation/"+credentials.OAuthTokenKey]; !ok {
		t.Fatal("credential was not written to requested ref")
	}
}

func TestInteractiveInitUsesPromptedBackendForStoreAndConfig(t *testing.T) {
	harness := newInitHarness(t)
	harness.interactive = true
	harness.prompt = func(setup *Setup) error {
		if setup.Backend != "file" {
			t.Fatalf("seeded backend = %q", setup.Backend)
		}
		setup.ClientID = "client-id"
		setup.Backend = "pass"
		return nil
	}
	if err := harness.execute("--backend", "file", "--no-verify"); err != nil {
		t.Fatal(err)
	}
	if len(harness.requests) != 1 || harness.requests[0].Backend != "pass" || !harness.requests[0].BackendSet || harness.requests[0].Config.Keyring.Backend != "pass" {
		t.Fatalf("open requests = %+v", harness.requests)
	}
	loaded, err := config.Load(harness.scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Keyring.Backend != "pass" {
		t.Fatalf("saved backend = %q", loaded.Keyring.Backend)
	}
}

func TestNonInteractiveInitNamesMissingClientID(t *testing.T) {
	harness := newInitHarness(t)
	called := false
	harness.authorize = func(context.Context, auth.Request) (token.Envelope, error) {
		called = true
		return token.Envelope{}, nil
	}
	err := harness.execute("--non-interactive")
	if exitcode.Code(err) != exitcode.Usage || !strings.Contains(err.Error(), "--client-id") || called {
		t.Fatalf("error = %v code=%d authorize=%t", err, exitcode.Code(err), called)
	}
}

func TestInitRefusesExistingCredentialBeforeAuthorization(t *testing.T) {
	harness := newInitHarness(t)
	harness.store.values["default/"+credentials.OAuthTokenKey] = "old-secret"
	called := false
	harness.authorize = func(context.Context, auth.Request) (token.Envelope, error) {
		called = true
		return token.Envelope{}, nil
	}
	err := harness.execute("--client-id", "client-id", "--no-verify")
	if !errors.Is(err, credstore.ErrExists) || called {
		t.Fatalf("error = %v authorize=%t", err, called)
	}
}

func TestInitOverwritesExistingCredentialWhenRequested(t *testing.T) {
	harness := newInitHarness(t)
	key := "default/" + credentials.OAuthTokenKey
	harness.store.values[key] = "old-secret"
	if err := harness.execute("--client-id", "client-id", "--no-verify", "--overwrite"); err != nil {
		t.Fatal(err)
	}
	if harness.store.values[key] == "old-secret" || harness.store.setCalls != 1 {
		t.Fatalf("credential = %q, set calls = %d", harness.store.values[key], harness.store.setCalls)
	}
}

func TestInitVerificationFailureWritesNothing(t *testing.T) {
	harness := newInitHarness(t)
	harness.verify = func(context.Context, config.Config, token.Envelope) (client.User, error) {
		harness.events = append(harness.events, "verify")
		return client.User{}, errors.New("verification failed")
	}
	err := harness.execute("--client-id", "client-id")
	if exitcode.Code(err) != exitcode.Upstream || harness.store.setCalls != 0 || containsEvent(harness.events, "save") {
		t.Fatalf("error=%v events=%v setCalls=%d", err, harness.events, harness.store.setCalls)
	}
}

func TestInitClassifiesAuthorizationFailures(t *testing.T) {
	tests := []struct {
		err  error
		code int
	}{
		{auth.ErrInvalidCallback, exitcode.Usage},
		{auth.ErrStateMismatch, exitcode.Usage},
		{auth.ErrAccessDenied, exitcode.Config},
		{auth.ErrAuthorizationTimeout, exitcode.Upstream},
		{auth.ErrExchange, exitcode.Upstream},
		{errors.New("listener failed"), exitcode.Generic},
	}
	for _, test := range tests {
		harness := newInitHarness(t)
		harness.authorize = func(context.Context, auth.Request) (token.Envelope, error) {
			return token.Envelope{}, test.err
		}
		err := harness.execute("--client-id", "client-id", "--no-verify")
		if exitcode.Code(err) != test.code || harness.store.setCalls != 0 {
			t.Fatalf("source %v: error=%v code=%d setCalls=%d", test.err, err, exitcode.Code(err), harness.store.setCalls)
		}
	}
}

func TestInitRollsBackCredentialWhenConfigSaveFails(t *testing.T) {
	tests := []struct {
		name     string
		oldValue string
		want     string
	}{
		{name: "new credential is deleted"},
		{name: "overwritten credential is restored", oldValue: "old-secret", want: "old-secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newInitHarness(t)
			key := "default/" + credentials.OAuthTokenKey
			if test.oldValue != "" {
				harness.store.values[key] = test.oldValue
			}
			harness.saveConfig = func(config.Config) error {
				harness.events = append(harness.events, "save")
				return errors.New("disk full")
			}
			args := []string{"--client-id", "client-id", "--no-verify"}
			if test.oldValue != "" {
				args = append(args, "--overwrite")
			}
			err := harness.execute(args...)
			if exitcode.Code(err) != exitcode.Config {
				t.Fatalf("error = %v code=%d", err, exitcode.Code(err))
			}
			if got := harness.store.values[key]; got != test.want {
				t.Fatalf("credential after rollback = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInitCredentialAndRollbackFailuresAreSecretSafe(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*initHarness)
		args      []string
	}{
		{
			name: "credential write",
			configure: func(harness *initHarness) {
				harness.store.setErr = errors.New("backend echoed access-secret and refresh-secret")
			},
			args: []string{"--client-id", "client-id", "--no-verify"},
		},
		{
			name: "new credential rollback",
			configure: func(harness *initHarness) {
				harness.saveConfig = func(config.Config) error { return errors.New("config echoed access-secret") }
				harness.store.deleteErr = errors.New("delete echoed refresh-secret")
			},
			args: []string{"--client-id", "client-id", "--no-verify"},
		},
		{
			name: "overwritten credential rollback",
			configure: func(harness *initHarness) {
				harness.store.values["default/"+credentials.OAuthTokenKey] = "old-secret"
				harness.saveConfig = func(config.Config) error { return errors.New("config echoed old-secret") }
				harness.store.setErrors = []error{nil, errors.New("restore echoed old-secret and access-secret")}
			},
			args: []string{"--client-id", "client-id", "--no-verify", "--overwrite"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newInitHarness(t)
			test.configure(harness)
			err := harness.execute(test.args...)
			if exitcode.Code(err) != exitcode.Config {
				t.Fatalf("error = %v, code = %d", err, exitcode.Code(err))
			}
			message := err.Error()
			for _, secret := range []string{"access-secret", "refresh-secret", "old-secret"} {
				if strings.Contains(message, secret) {
					t.Fatalf("error leaked %q: %v", secret, err)
				}
			}
		})
	}
}

type initHarness struct {
	scope       statedir.Scope
	store       *initStore
	now         time.Time
	interactive bool
	backend     string
	prompt      func(*Setup) error
	authorize   func(context.Context, auth.Request) (token.Envelope, error)
	verify      func(context.Context, config.Config, token.Envelope) (client.User, error)
	saveConfig  func(config.Config) error
	events      []string
	requests    []credentials.OpenRequest
	stdout      bytes.Buffer
	stderr      bytes.Buffer
}

func newInitHarness(t *testing.T) *initHarness {
	t.Helper()
	statedirtest.Hermetic(t)
	harness := &initHarness{
		scope: statedir.Scope{Name: config.Service},
		store: &initStore{values: map[string]string{}},
		now:   time.Now().UTC(),
	}
	harness.authorize = func(context.Context, auth.Request) (token.Envelope, error) {
		harness.events = append(harness.events, "authorize")
		return harness.envelope(), nil
	}
	harness.verify = func(context.Context, config.Config, token.Envelope) (client.User, error) {
		harness.events = append(harness.events, "verify")
		return client.User{AccountID: "account"}, nil
	}
	harness.saveConfig = func(value config.Config) error {
		harness.events = append(harness.events, "save")
		return config.Save(harness.scope, value)
	}
	harness.store.onSet = func() { harness.events = append(harness.events, "set") }
	return harness
}

func (harness *initHarness) envelope() token.Envelope {
	return token.Envelope{
		AccessToken: "access-secret", TokenType: "Bearer", RefreshToken: "refresh-secret",
		ExpiresAt: harness.now.Add(time.Hour), Scopes: []string{auth.ScopeUserReadPrivate},
	}
}

func (harness *initHarness) execute(args ...string) error {
	command := New(Dependencies{
		Scope: harness.scope,
		OpenStore: func(request credentials.OpenRequest) (credentials.Store, error) {
			harness.requests = append(harness.requests, request)
			return harness.store, nil
		},
		Backend: &harness.backend, Now: func() time.Time { return harness.now }, Interactive: harness.interactive,
		Prompt: harness.prompt, Authorize: harness.authorize, Verify: harness.verify, SaveConfig: harness.saveConfig,
	})
	command.SetIn(&bytes.Buffer{})
	command.SetOut(&harness.stdout)
	command.SetErr(&harness.stderr)
	command.Flags().StringVar(&harness.backend, credstore.BackendFlagName, "", "")
	command.SetArgs(args)
	return command.ExecuteContext(context.Background())
}

type initStore struct {
	values    map[string]string
	setCalls  int
	setErr    error
	setErrors []error
	deleteErr error
	onSet     func()
}

func (*initStore) Backend() (credstore.Backend, credstore.Source) {
	return credstore.BackendMemory, credstore.SourceExplicit
}
func (*initStore) Close() error { return nil }
func (store *initStore) Get(profile, key string) (string, error) {
	value, ok := store.values[profile+"/"+key]
	if !ok {
		return "", credstore.ErrNotFound
	}
	return value, nil
}
func (store *initStore) Set(profile, key, value string, _ ...credstore.SetOpt) error {
	store.setCalls++
	if store.onSet != nil {
		store.onSet()
	}
	err := store.setErr
	if len(store.setErrors) > 0 {
		err = store.setErrors[0]
		store.setErrors = store.setErrors[1:]
	}
	if err != nil {
		return err
	}
	store.values[profile+"/"+key] = value
	return nil
}
func (store *initStore) Delete(profile, key string) error {
	if store.deleteErr != nil {
		return store.deleteErr
	}
	delete(store.values, profile+"/"+key)
	return nil
}
func (store *initStore) Exists(profile, key string) (bool, error) {
	_, ok := store.values[profile+"/"+key]
	return ok, nil
}

func containsEvent(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}
