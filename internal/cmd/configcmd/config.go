// Package configcmd implements the local configuration control plane.
package configcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
)

const filePassphraseEnv = "SPOTIFY_CLI_KEYRING_PASSPHRASE" // #nosec G101 -- environment variable name.

// Dependencies contains the runtime effects used by config commands.
type Dependencies struct {
	Scope     statedir.Scope
	Cache     statedir.Cache
	Data      statedir.Data
	OpenStore credentials.Opener
	Backend   *string
}

// New constructs the config command group.
func New(deps Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use: "config", Short: "Manage configuration", Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.PersistentPreRunE = func(leaf *cobra.Command, _ []string) error {
		flag := leaf.Flags().Lookup(credstore.BackendFlagName)
		if err := credentials.ValidateExplicitBackend(value(deps.Backend), flag != nil && flag.Changed); err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
		return nil
	}
	cmd.AddCommand(newShow(deps), newPath(deps), newClear(deps))
	return cmd
}

type showResult struct {
	ClientID          string               `json:"client_id"`
	RedirectURI       string               `json:"redirect_uri"`
	CredentialRef     string               `json:"credential_ref"`
	Backend           string               `json:"backend"`
	BackendSource     string               `json:"backend_source"`
	PassphraseSource  string               `json:"passphrase_source,omitempty"`
	OAuthTokenPresent bool                 `json:"oauth_token_present"`
	Keyring           config.KeyringConfig `json:"keyring"`
}

func newShow(deps Dependencies) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show non-secret configuration and credential status",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(deps.Scope)
			if err != nil {
				return exitcode.New(exitcode.Config, err)
			}
			profile, err := credentials.ParseProfile(cfg.CredentialRef)
			if err != nil {
				return exitcode.New(exitcode.Config, err)
			}
			store, err := deps.OpenStore(openRequest(cmd, deps, cfg))
			if err != nil {
				return exitcode.New(exitcode.Config, fmt.Errorf("opening credential store: %w", err))
			}
			defer func() { _ = store.Close() }()
			present, err := store.Exists(profile, credentials.OAuthTokenKey)
			if err != nil {
				return exitcode.New(exitcode.Config, fmt.Errorf("checking OAuth credential: %w", err))
			}
			backend, source := store.Backend()
			result := showResult{
				ClientID:          cfg.ClientID,
				RedirectURI:       cfg.RedirectURI,
				CredentialRef:     cfg.CredentialRef,
				Backend:           string(backend),
				BackendSource:     string(source),
				OAuthTokenPresent: present,
				Keyring:           cfg.Keyring,
			}
			if backend == credstore.BackendFile {
				if os.Getenv(filePassphraseEnv) != "" {
					result.PassphraseSource = "environment"
				} else {
					result.PassphraseSource = "prompt"
				}
			}
			if jsonOutput {
				return writeJSON(cmd, result)
			}
			if err := writeShowText(cmd, result); err != nil {
				return exitcode.New(exitcode.Generic, err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit JSON")
	return cmd
}

func writeShowText(cmd *cobra.Command, result showResult) error {
	var rendered strings.Builder
	lines := [][2]string{
		{"client_id", result.ClientID},
		{"redirect_uri", result.RedirectURI},
		{"credential_ref", result.CredentialRef},
		{"backend", result.Backend},
		{"backend_source", result.BackendSource},
	}
	if result.PassphraseSource != "" {
		lines = append(lines, [2]string{"passphrase_source", result.PassphraseSource})
	}
	lines = append(lines, [2]string{"oauth_token_present", fmt.Sprintf("%t", result.OAuthTokenPresent)})
	for _, line := range lines {
		_, _ = fmt.Fprintf(&rendered, "%s\t%s\n", line[0], line[1])
	}
	writeKeyringText(&rendered, result.Keyring)
	if _, err := io.WriteString(cmd.OutOrStdout(), rendered.String()); err != nil {
		return errors.New("writing text output failed")
	}
	return nil
}

type pathResult struct {
	Config string `json:"config"`
	Cache  string `json:"cache"`
	Data   string `json:"data"`
}

func newPath(deps Dependencies) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Show resolved state paths",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := resolvePaths(deps)
			if err != nil {
				return exitcode.New(exitcode.Config, err)
			}
			if jsonOutput {
				return writeJSON(cmd, result)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "config\t%s\ncache\t%s\ndata\t%s\n", result.Config, result.Cache, result.Data); err != nil {
				return exitcode.New(exitcode.Generic, errors.New("writing text output failed"))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit JSON")
	return cmd
}

type clearAction struct {
	Status string `json:"status"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

type clearResult struct {
	DryRun  bool          `json:"dry_run"`
	Actions []clearAction `json:"actions"`
}

func newClear(deps Dependencies) *cobra.Command {
	var all, dryRun, jsonOutput bool
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear the active OAuth credential",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, runErr := runClear(cmd, deps, all, dryRun)
			var renderErr error
			if jsonOutput {
				renderErr = writeJSON(cmd, result)
			} else {
				var rendered strings.Builder
				for _, action := range result.Actions {
					_, _ = fmt.Fprintf(&rendered, "%s\t%s\t%s\n", action.Status, action.Type, action.Target)
				}
				if _, err := io.WriteString(cmd.OutOrStdout(), rendered.String()); err != nil {
					renderErr = errors.New("writing text output failed")
				}
			}
			if runErr != nil {
				return exitcode.New(exitcode.Config, errors.Join(runErr, renderErr))
			}
			if renderErr != nil {
				return exitcode.New(exitcode.Generic, renderErr)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Also clear configuration and cache")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report actions without changing state")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit JSON")
	return cmd
}

func runClear(cmd *cobra.Command, deps Dependencies, all, dryRun bool) (clearResult, error) {
	result := clearResult{DryRun: dryRun, Actions: []clearAction{}}
	paths, err := resolvePaths(deps)
	if err != nil {
		return result, err
	}
	cfg, loadErr := config.Load(deps.Scope)
	if loadErr != nil && !all {
		return result, loadErr
	}

	var failures []error
	if loadErr != nil {
		result.Actions = append(result.Actions, clearAction{Status: "skipped", Type: "credential", Target: "active/oauth_token"})
		failures = append(failures, fmt.Errorf("loading active config: %w", loadErr))
	} else {
		profile, refErr := credentials.ParseProfile(cfg.CredentialRef)
		if refErr != nil {
			if !all {
				return result, refErr
			}
			result.Actions = append(result.Actions, clearAction{Status: "skipped", Type: "credential", Target: "active/oauth_token"})
			failures = append(failures, refErr)
		} else if dryRun {
			result.Actions = append(result.Actions, clearAction{Status: "would_remove", Type: "credential", Target: cfg.CredentialRef + "/" + credentials.OAuthTokenKey})
		} else {
			action, clearErr := clearCredential(cmd, deps, cfg, profile)
			result.Actions = append(result.Actions, action)
			if clearErr != nil {
				if !all {
					return result, clearErr
				}
				failures = append(failures, clearErr)
			}
		}
	}

	if all {
		configAction, configErr := fileAction(paths.Config, "config", dryRun, false)
		result.Actions = append(result.Actions, configAction)
		if configErr != nil {
			failures = append(failures, configErr)
		}
		cacheAction, cacheErr := fileAction(paths.Cache, "cache", dryRun, true)
		result.Actions = append(result.Actions, cacheAction)
		if cacheErr != nil {
			failures = append(failures, cacheErr)
		}
		if !dryRun {
			_ = os.Remove(filepath.Dir(paths.Config))
		}
	}
	return result, errors.Join(failures...)
}

func clearCredential(cmd *cobra.Command, deps Dependencies, cfg config.Config, profile string) (clearAction, error) {
	target := cfg.CredentialRef + "/" + credentials.OAuthTokenKey
	store, err := deps.OpenStore(openRequest(cmd, deps, cfg))
	if err != nil {
		return clearAction{Status: "skipped", Type: "credential", Target: target}, fmt.Errorf("opening credential store: %w", err)
	}
	defer func() { _ = store.Close() }()
	present, err := store.Exists(profile, credentials.OAuthTokenKey)
	if err != nil {
		return clearAction{Status: "failed", Type: "credential", Target: target}, fmt.Errorf("checking OAuth credential: %w", err)
	}
	if !present {
		return clearAction{Status: "absent", Type: "credential", Target: target}, nil
	}
	if err := store.Delete(profile, credentials.OAuthTokenKey); err != nil && !errors.Is(err, credstore.ErrNotFound) {
		return clearAction{Status: "failed", Type: "credential", Target: target}, fmt.Errorf("deleting OAuth credential: %w", err)
	}
	return clearAction{Status: "removed", Type: "credential", Target: target}, nil
}

func fileAction(path, kind string, dryRun, recursive bool) (clearAction, error) {
	action := clearAction{Type: kind, Target: path}
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		action.Status = "absent"
		return action, nil
	}
	if err != nil {
		action.Status = "failed"
		return action, fmt.Errorf("checking %s path: %w", kind, err)
	}
	if dryRun {
		action.Status = "would_remove"
		return action, nil
	}
	if recursive {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		action.Status = "failed"
		return action, fmt.Errorf("removing %s path: %w", kind, err)
	}
	action.Status = "removed"
	return action, nil
}

func resolvePaths(deps Dependencies) (pathResult, error) {
	configPath, err := config.Path(deps.Scope)
	if err != nil {
		return pathResult{}, err
	}
	cachePath, err := deps.Cache.CacheDir()
	if err != nil {
		return pathResult{}, err
	}
	dataPath, err := deps.Data.DataDir()
	if err != nil {
		return pathResult{}, err
	}
	return pathResult{Config: configPath, Cache: cachePath, Data: dataPath}, nil
}

func openRequest(cmd *cobra.Command, deps Dependencies, cfg config.Config) credentials.OpenRequest {
	flag := cmd.Flags().Lookup(credstore.BackendFlagName)
	return credentials.OpenRequest{Config: cfg, Backend: value(deps.Backend), BackendSet: flag != nil && flag.Changed}
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}

func noArgs(_ *cobra.Command, args []string) error {
	if len(args) != 0 {
		return exitcode.New(exitcode.Usage, errors.New("command takes no arguments"))
	}
	return nil
}

func writeJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func writeKeyringText(out interface{ Write([]byte) (int, error) }, keyring config.KeyringConfig) {
	values := [][2]string{
		{"backend", keyring.Backend},
		{"onepassword.timeout", durationString(keyring.OnePassword.Timeout)},
		{"onepassword.vault_id", keyring.OnePassword.VaultID},
		{"onepassword.item_title_prefix", keyring.OnePassword.ItemTitlePrefix},
		{"onepassword.item_tag", keyring.OnePassword.ItemTag},
		{"onepassword.item_field_title", keyring.OnePassword.ItemFieldTitle},
		{"onepassword.connect_host", keyring.OnePassword.ConnectHost},
		{"onepassword.connect_token_env", keyring.OnePassword.ConnectTokenEnv},
		{"onepassword.service_token_env", keyring.OnePassword.ServiceTokenEnv},
		{"onepassword.desktop_account_id", keyring.OnePassword.DesktopAccountID},
	}
	for _, pair := range values {
		if strings.TrimSpace(pair[1]) != "" {
			_, _ = fmt.Fprintf(out, "keyring.%s\t%s\n", pair[0], pair[1])
		}
	}
}

func durationString(value config.Duration) string {
	if value.IsZero() {
		return ""
	}
	return value.String()
}
