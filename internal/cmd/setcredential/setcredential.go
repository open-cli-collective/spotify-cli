// Package setcredential implements the single-secret credential ingress path.
package setcredential

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/token"
)

const maxEnvelopeBytes = 1 << 20

// CredentialStore is the credential capability required by set-credential.
type CredentialStore interface {
	Backend() (credstore.Backend, credstore.Source)
	Close() error
	Set(profile, key, value string, opts ...credstore.SetOpt) error
}

// StoreOpener opens the credential capability required by set-credential.
type StoreOpener func(credentials.OpenRequest) (CredentialStore, error)

// Dependencies contains the runtime effects used by set-credential.
type Dependencies struct {
	Scope     statedir.Scope
	OpenStore StoreOpener
	Backend   *string
	Now       func() time.Time
}

type result struct {
	Ref     string `json:"ref"`
	Key     string `json:"key"`
	Backend string `json:"backend"`
	Written bool   `json:"written"`
	Error   string `json:"error,omitempty"`
}

type safeStoreError struct {
	message string
	cause   error
}

func (e safeStoreError) Error() string { return e.message }
func (e safeStoreError) Unwrap() error { return e.cause }

// New constructs the set-credential command.
func New(deps Dependencies) *cobra.Command {
	var ref, key, fromEnv string
	var stdin, overwrite, jsonOutput bool
	cmd := &cobra.Command{
		Use:   "set-credential",
		Short: "Write one OAuth credential without displaying it",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return exitcode.New(exitcode.Usage, errors.New("set-credential takes no arguments"))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, deps, ref, key, fromEnv, stdin, overwrite, jsonOutput)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&ref, "ref", "", "Credential ref (service/profile)")
	flags.StringVar(&key, "key", "", "Credential key (oauth_token)")
	flags.BoolVar(&stdin, "stdin", false, "Read the OAuth envelope from stdin")
	flags.StringVar(&fromEnv, "from-env", "", "Read the OAuth envelope from the named environment variable")
	flags.BoolVar(&overwrite, "overwrite", false, "Replace an existing credential")
	flags.BoolVar(&jsonOutput, "json", false, "Emit JSON")
	return cmd
}

func run(cmd *cobra.Command, deps Dependencies, ref, key, fromEnv string, stdin, overwrite, jsonOutput bool) error {
	response := result{Ref: ref, Key: key}
	fail := func(code int, err error) error {
		if !jsonOutput {
			return exitcode.New(code, err)
		}
		response.Written = false
		response.Error = err.Error()
		if encodeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(response); encodeErr != nil {
			return exitcode.New(exitcode.Generic, errors.New("writing JSON output failed"))
		}
		return exitcode.NewQuiet(code, err)
	}
	backendFlag := cmd.Flags().Lookup(credstore.BackendFlagName)
	if err := credentials.ValidateExplicitBackend(pointerValue(deps.Backend), backendFlag != nil && backendFlag.Changed); err != nil {
		return fail(exitcode.Usage, err)
	}

	if key != credentials.OAuthTokenKey {
		return fail(exitcode.Usage, errors.New("--key must be oauth_token"))
	}
	if stdin == (fromEnv != "") {
		return fail(exitcode.Usage, errors.New("exactly one of --stdin or --from-env is required"))
	}

	cfg, err := config.Load(deps.Scope)
	if err != nil {
		return fail(exitcode.Config, err)
	}
	if ref == "" {
		exists, err := config.Exists(deps.Scope)
		if err != nil {
			return fail(exitcode.Config, err)
		}
		if !exists {
			return fail(exitcode.Usage, errors.New("--ref is required when no config file exists"))
		}
		ref = cfg.CredentialRef
		response.Ref = ref
	}
	profile, err := credentials.ParseProfile(ref)
	if err != nil {
		return fail(exitcode.Usage, err)
	}

	raw, err := readEnvelope(cmd, fromEnv, stdin)
	if err != nil {
		return fail(exitcode.Usage, err)
	}
	now := time.Now()
	if deps.Now != nil {
		now = deps.Now()
	}
	envelope, err := token.Decode(raw, now)
	if err != nil {
		return fail(exitcode.Usage, err)
	}
	canonical, err := token.Encode(envelope, now)
	if err != nil {
		return fail(exitcode.Usage, err)
	}

	store, err := deps.OpenStore(credentials.OpenRequest{
		Config:     cfg,
		Backend:    pointerValue(deps.Backend),
		BackendSet: backendFlag != nil && backendFlag.Changed,
	})
	if err != nil {
		return fail(exitcode.Config, fmt.Errorf("opening credential store: %w", err))
	}
	defer func() { _ = store.Close() }()
	backend, _ := store.Backend()
	response.Backend = string(backend)
	redactor := credstore.NewRedactor(string(raw), string(canonical), envelope.AccessToken, envelope.RefreshToken)
	setOptions := []credstore.SetOpt(nil)
	if overwrite {
		setOptions = append(setOptions, credstore.WithOverwrite())
	}
	if err := store.Set(profile, key, string(canonical), setOptions...); err != nil {
		message := redactor.Redact(err.Error())
		if message == "" {
			message = "credential store write failed"
		}
		if errors.Is(err, credstore.ErrExists) {
			return fail(exitcode.Generic, safeStoreError{message: message, cause: credstore.ErrExists})
		}
		if errors.Is(err, credstore.ErrKeyNotAllowed) {
			return fail(exitcode.Usage, safeStoreError{message: message, cause: credstore.ErrKeyNotAllowed})
		}
		return fail(exitcode.Config, errors.New(message))
	}
	response.Written = true
	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(response)
	}
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s to %s via %s\n", key, ref, backend); err != nil {
		return exitcode.New(exitcode.Generic, errors.New("writing text output failed"))
	}
	return nil
}

func readEnvelope(cmd *cobra.Command, fromEnv string, stdin bool) ([]byte, error) {
	if fromEnv != "" {
		if !config.ValidEnvName(fromEnv) {
			return nil, errors.New("--from-env must name an environment variable")
		}
		value, ok := os.LookupEnv(fromEnv)
		if !ok {
			return nil, fmt.Errorf("environment variable %s is not set", fromEnv)
		}
		return []byte(strings.TrimSpace(value)), nil
	}
	if !stdin {
		return nil, errors.New("--stdin is required")
	}
	data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxEnvelopeBytes+1))
	if err != nil {
		return nil, errors.New("reading OAuth token envelope failed")
	}
	if len(data) > maxEnvelopeBytes {
		return nil, errors.New("OAuth token envelope exceeds 1 MiB")
	}
	return []byte(strings.TrimSpace(string(data))), nil
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
