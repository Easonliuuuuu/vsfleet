// Package config loads and stores the vctui configuration: a list of vCenter
// contexts and, for each, how to reach it. It never holds a secret — only a
// reference to one.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Version is the configuration schema version this build writes.
const Version = 1

// EnvConfigPath overrides the configuration file location.
const EnvConfigPath = "VCTUI_CONFIG"

// ErrNotFound is returned when a named context does not exist.
var ErrNotFound = errors.New("context not found")

// Config is the whole configuration file.
type Config struct {
	Version        int        `toml:"version"`
	CurrentContext string     `toml:"current_context,omitempty"`
	Contexts       []*Context `toml:"contexts"`

	// path is where this config was loaded from, used by Save.
	path string
}

// DefaultPath returns the configuration file path, honouring VCTUI_CONFIG and
// then the platform user configuration directory.
func DefaultPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(EnvConfigPath)); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "vctui", "config.toml"), nil
}

// Load reads the configuration from path. An empty path means DefaultPath. A
// missing file is not an error: it yields an empty configuration, so a fresh
// install can run "vctui context add" immediately.
func Load(path string) (*Config, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	cfg := &Config{Version: Version, path: path}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return cfg, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.path = path
	if cfg.Version == 0 {
		cfg.Version = Version
	}
	if cfg.Version > Version {
		return nil, fmt.Errorf("%s: configuration version %d is newer than this build understands (%d)", path, cfg.Version, Version)
	}
	for _, c := range cfg.Contexts {
		c.Normalize()
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Path returns the file this configuration is bound to.
func (c *Config) Path() string { return c.path }

// SetPath binds the configuration to a file for a later Save.
func (c *Config) SetPath(p string) { c.path = p }

// Validate checks every context and the uniqueness of their names.
func (c *Config) Validate() error {
	seen := make(map[string]bool, len(c.Contexts))
	for _, ctx := range c.Contexts {
		if err := ctx.Validate(); err != nil {
			return err
		}
		if seen[ctx.Name] {
			return fmt.Errorf("duplicate context name %q", ctx.Name)
		}
		seen[ctx.Name] = true
	}
	if c.CurrentContext != "" && !seen[c.CurrentContext] {
		return fmt.Errorf("current_context %q does not name a configured context", c.CurrentContext)
	}
	return nil
}

// Context returns the context with the given name. An empty name returns the
// current context, or the only context when exactly one is configured.
func (c *Config) Context(name string) (*Context, error) {
	if name == "" {
		switch {
		case c.CurrentContext != "":
			name = c.CurrentContext
		case len(c.Contexts) == 1:
			return c.Contexts[0], nil
		case len(c.Contexts) == 0:
			return nil, fmt.Errorf("no contexts configured: run \"vctui context add\"")
		default:
			return nil, fmt.Errorf("no context selected: pass --context or run \"vctui context use <name>\"")
		}
	}
	for _, ctx := range c.Contexts {
		if ctx.Name == name {
			return ctx, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrNotFound, name)
}

// Names returns every configured context name, in file order.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Contexts))
	for _, ctx := range c.Contexts {
		names = append(names, ctx.Name)
	}
	return names
}

// Resolve turns a selection into a concrete list of contexts. Passing all
// selects every configured context; otherwise names are looked up in order,
// and an empty selection falls back to the current context.
func (c *Config) Resolve(names []string, all bool) ([]*Context, error) {
	if all {
		if len(c.Contexts) == 0 {
			return nil, fmt.Errorf("no contexts configured: run \"vctui context add\"")
		}
		out := make([]*Context, len(c.Contexts))
		copy(out, c.Contexts)
		return out, nil
	}
	if len(names) == 0 {
		ctx, err := c.Context("")
		if err != nil {
			return nil, err
		}
		return []*Context{ctx}, nil
	}
	out := make([]*Context, 0, len(names))
	for _, n := range names {
		ctx, err := c.Context(n)
		if err != nil {
			return nil, err
		}
		out = append(out, ctx)
	}
	return out, nil
}

// Add appends a context, replacing an existing one of the same name only when
// replace is set.
func (c *Config) Add(ctx *Context, replace bool) error {
	ctx.Normalize()
	if err := ctx.Validate(); err != nil {
		return err
	}
	for i, existing := range c.Contexts {
		if existing.Name == ctx.Name {
			if !replace {
				return fmt.Errorf("context %q already exists", ctx.Name)
			}
			c.Contexts[i] = ctx
			return nil
		}
	}
	c.Contexts = append(c.Contexts, ctx)
	return nil
}

// Remove deletes a context by name.
func (c *Config) Remove(name string) error {
	for i, ctx := range c.Contexts {
		if ctx.Name == name {
			c.Contexts = append(c.Contexts[:i], c.Contexts[i+1:]...)
			if c.CurrentContext == name {
				c.CurrentContext = ""
			}
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrNotFound, name)
}

// Sort orders contexts by name, which keeps hand edits and generated writes
// from fighting over the file.
func (c *Config) Sort() {
	sort.Slice(c.Contexts, func(i, j int) bool { return c.Contexts[i].Name < c.Contexts[j].Name })
}

// Save writes the configuration atomically with owner-only permissions.
//
// The mode is advisory on Windows, which has no POSIX permission bits: Chmod
// there only toggles the read-only attribute. The file holds credential
// references rather than secrets, so this is defence in depth either way.
func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("configuration has no path to save to")
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Version == 0 {
		c.Version = Version
	}
	b, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		return fmt.Errorf("replace %s: %w", c.path, err)
	}
	return nil
}
