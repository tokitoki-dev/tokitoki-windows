package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"strings"
)

// Client runs the shared CLI, one short-lived process per operation.
type Client struct {
	logger *slog.Logger
}

// NewClient creates a Client.
func NewClient(logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{logger: logger}
}

// Sync scans the given provider directories and uploads what they hold — the
// CLI's default command. Nothing to scan is a no-op, as on macOS.
func (c *Client) Sync(ctx context.Context, providerDirs map[string][]string) error {
	args := SyncArgs(providerDirs)
	if len(args) == 0 {
		return nil
	}
	out, err := c.run(ctx, "sync", args)
	if err != nil {
		return err
	}
	return requireOK(out)
}

// APIKey returns the configured API key, or ErrMissingAPIKey.
func (c *Client) APIKey(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	out, err := c.run(ctx, "get key", []string{"get", "key"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SetAPIKey stores the API key in the shared agent state.
func (c *Client) SetAPIKey(ctx context.Context, apiKey string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	out, err := c.run(ctx, "set key", []string{"set", "key", apiKey})
	if err != nil {
		return err
	}
	return requireOK(out)
}

// DashboardURL exchanges the stored API key for a one-time browser login
// URL, validated the way the macOS app validates it.
func (c *Client) DashboardURL(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	out, err := c.run(ctx, "get dashboard-url", []string{"get", "dashboard-url"})
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(string(out))
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("tokitoki get dashboard-url: not a URL")
	}
	return raw, nil
}

// Update asks the shared CLI to update itself against the server. The CLI
// owns the whole check-download-verify-swap sequence; a missing shared CLI
// or a dev build is a quiet no-op for the caller to log at most.
func (c *Client) Update(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()
	out, err := c.run(ctx, "update", []string{"update"})
	if err != nil {
		return err
	}
	return requireOK(out)
}

// run invokes the shared CLI once and returns its standard output. label
// names the operation in errors — never the raw arguments, which may carry
// the API key.
func (c *Client) run(ctx context.Context, label string, args []string) ([]byte, error) {
	binary, err := SharedBinary()
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	hideConsole(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == exitNoAPIKey {
			return nil, ErrMissingAPIKey
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("tokitoki %s: %w", label, ctx.Err())
		}
		if detail := truncate(stderr.String()); detail != "" {
			return nil, fmt.Errorf("tokitoki %s: %w: %s", label, err, detail)
		}
		return nil, fmt.Errorf("tokitoki %s: %w", label, err)
	}
	return stdout.Bytes(), nil
}

// requireOK enforces the CLI's JSON contract: exit 0 and {"ok":true}.
func requireOK(output []byte) error {
	var response struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("tokitoki returned invalid JSON: %w", err)
	}
	if !response.OK {
		return errors.New("tokitoki reported failure")
	}
	return nil
}

// truncate keeps stderr snippets short enough for a log line or balloon.
func truncate(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 200 {
		return message
	}
	return message[:197] + "..."
}
