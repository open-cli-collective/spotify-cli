package main

import (
	"bytes"
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

	"github.com/open-cli-collective/spotify-cli/internal/cmd/root"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
)

type processFailStore struct{ err error }

func (s processFailStore) Backend() (credstore.Backend, credstore.Source) {
	return credstore.BackendMemory, credstore.SourceExplicit
}
func (s processFailStore) Close() error { return nil }
func (s processFailStore) Get(string, string) (string, error) {
	return "", credstore.ErrNotFound
}
func (s processFailStore) Set(string, string, string, ...credstore.SetOpt) error { return s.err }
func (s processFailStore) Delete(string, string) error                           { return nil }
func (s processFailStore) Exists(string, string) (bool, error)                   { return false, nil }

func TestUnknownCommandsExitUsage(t *testing.T) {
	for _, args := range [][]string{{"frobnicate"}, {"config", "frobnicate"}} {
		var out, errOut bytes.Buffer
		cmd := root.New(root.Dependencies{
			In:     &bytes.Buffer{},
			Out:    &out,
			ErrOut: &errOut,
			Scope:  statedir.Scope{Name: config.Service},
			Cache:  statedir.Cache{Tool: config.Tool},
			Data:   statedir.Data{Tool: config.Tool},
		})
		cmd.SetArgs(args)
		if code := executeCommand(cmd); code != exitcode.Usage {
			t.Fatalf("args %v: exit = %d, stderr = %q", args, code, errOut.String())
		}
	}
}

func TestMeMissingConfigurationAtProcessBoundary(t *testing.T) {
	statedirtest.Hermetic(t)
	var out, errOut bytes.Buffer
	cmd := root.New(root.Dependencies{
		In: &bytes.Buffer{}, Out: &out, ErrOut: &errOut,
		Scope: statedir.Scope{Name: config.Service}, Cache: statedir.Cache{Tool: config.Tool}, Data: statedir.Data{Tool: config.Tool},
	})
	cmd.SetArgs([]string{"me"})
	if code := executeCommand(cmd); code != exitcode.Config {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "spotify client ID is not configured; run sptfy init") {
		t.Fatalf("stdout = %q, stderr = %q", out.String(), errOut.String())
	}
}

func TestSpotifyHTTPClientDoesNotForwardOAuthPostOnRedirect(t *testing.T) {
	const secret = "oauth-post-secret-sentinel"
	forwarded := false
	destination := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		forwarded = true
		body, _ := io.ReadAll(request.Body)
		if strings.Contains(string(body), secret) {
			t.Error("redirect destination received OAuth secret")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", destination.URL)
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	response, err := spotifyHTTPClient().Post(source.URL, "application/x-www-form-urlencoded", strings.NewReader("code="+secret))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusTemporaryRedirect || forwarded {
		t.Fatalf("status = %d, forwarded = %t", response.StatusCode, forwarded)
	}
}

func TestCredentialStoreFailureIsSecretSafeAtProcessBoundary(t *testing.T) {
	for _, ingress := range []string{"stdin", "env"} {
		t.Run(ingress, func(t *testing.T) {
			statedirtest.Hermetic(t)
			canary := "access-process-secret-canary"
			raw := `{"version":1,"access_token":"` + canary + `","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`
			var in, out, errOut bytes.Buffer
			args := []string{"set-credential", "--ref", "spotify-cli/default", "--key", "oauth_token"}
			if ingress == "stdin" {
				in.WriteString(raw)
				args = append(args, "--stdin")
			} else {
				const envName = "SPOTIFY_PROCESS_FAILURE"
				t.Setenv(envName, raw)
				args = append(args, "--from-env", envName)
			}
			cmd := root.New(root.Dependencies{
				In: &in, Out: &out, ErrOut: &errOut,
				Scope: statedir.Scope{Name: config.Service}, Cache: statedir.Cache{Tool: config.Tool}, Data: statedir.Data{Tool: config.Tool},
				Now: func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) },
				OpenStore: func(credentials.OpenRequest) (credentials.Store, error) {
					return processFailStore{err: errors.New("backend echoed " + canary)}, nil
				},
			})
			cmd.SetArgs(args)
			if code := executeCommand(cmd); code != exitcode.Config {
				t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
			}
			if strings.Contains(out.String()+errOut.String(), canary) {
				t.Fatalf("secret leaked: stdout=%q stderr=%q", out.String(), errOut.String())
			}
		})
	}
}
