package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type reloadProbe struct {
	Application struct {
		Version string `mapstructure:"version"`
	} `mapstructure:"application"`
}

func writeProbeConfig(t *testing.T, path, version string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("application:\n  version: "+version+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func probeOptions(t *testing.T, dir string) *LoaderOptions[reloadProbe] {
	t.Helper()
	opts := DefaultLoaderOptions[reloadProbe]()
	opts.ConfigName = "config"
	opts.ConfigType = "yml"
	opts.ConfigPaths = []string{dir}
	opts.ReloadDebounce = 50 * time.Millisecond
	return opts
}

// TestReloadRereadsTheFile pins the core contract. Reload used to return nil
// while leaving GetCurrent on the values from the first load: loadConfig
// replaced the loader's file-bound viper with a buffer-backed one, so every
// later read found no file and silently re-unmarshalled the stale buffer.
// A reload that reports success without reloading is worse than one that
// fails, because nothing downstream can detect it.
func TestReloadRereadsTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	writeProbeConfig(t, path, "v1")

	loader := NewViper(probeOptions(t, dir))

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}
	if cfg.Application.Version != "v1" {
		t.Fatalf("initial version = %q, want v1", cfg.Application.Version)
	}

	writeProbeConfig(t, path, "v2")

	if err := loader.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := loader.GetCurrent().Application.Version; got != "v2" {
		t.Errorf("after reload version = %q, want v2", got)
	}

	// A third round proves the loader did not merely survive one reload: the
	// original bug left it detached from the file permanently.
	writeProbeConfig(t, path, "v3")
	if err := loader.Reload(); err != nil {
		t.Fatalf("second reload: %v", err)
	}
	if got := loader.GetCurrent().Application.Version; got != "v3" {
		t.Errorf("after second reload version = %q, want v3", got)
	}
}

// TestWatchConfigFiresOnChange pins that WatchConfig actually registers a
// watch. It used to run against the detached viper and give up without a word.
func TestWatchConfigFiresOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	writeProbeConfig(t, path, "v1")

	var fired atomic.Int32

	opts := probeOptions(t, dir)
	opts.WatchConfig = true
	opts.OnConfigChange = func(*reloadProbe) { fired.Add(1) }

	loader := NewViper(opts)
	defer loader.Stop()

	if _, err := loader.Load(); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	writeProbeConfig(t, path, "v2")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if loader.GetCurrent().Application.Version == "v2" && fired.Load() > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Errorf("watcher did not reload: callbacks=%d version=%q",
		fired.Load(), loader.GetCurrent().Application.Version)
}

// TestWatchConfigFailsLoudlyWithoutAFile pins the second half of the fix:
// asking to watch something unwatchable is an error, not a silent no-op.
func TestWatchConfigFailsLoudlyWithoutAFile(t *testing.T) {
	opts := DefaultLoaderOptions[reloadProbe]()
	opts.ConfigName = "does-not-exist"
	opts.ConfigType = "yml"
	opts.ConfigPaths = []string{t.TempDir()}
	opts.WatchConfig = true

	if _, err := NewViper(opts).Load(); !errors.Is(err, ErrConfigWatchUnavailable) {
		t.Errorf("error = %v, want ErrConfigWatchUnavailable", err)
	}
}

// TestReloadKeepsEnvExpansion guards the mechanism the fix moved around: the
// expanded content, not the raw file, is what gets unmarshalled.
func TestReloadKeepsEnvExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")

	t.Setenv("GOBOX_PROBE_VERSION", "from-env")
	if err := os.WriteFile(path,
		[]byte("application:\n  version: ${GOBOX_PROBE_VERSION}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	loader := NewViper(probeOptions(t, dir))
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Application.Version != "from-env" {
		t.Fatalf("initial version = %q, want from-env", cfg.Application.Version)
	}

	if err := loader.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := loader.GetCurrent().Application.Version; got != "from-env" {
		t.Errorf("after reload version = %q, want from-env", got)
	}
}

// TestReloadRejectsInvalidConfigAndKeepsTheOldOne pins that a broken edit does
// not take effect: validation runs before the swap.
func TestReloadRejectsInvalidConfigAndKeepsTheOldOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	writeProbeConfig(t, path, "v1")

	loader := NewViper(probeOptions(t, dir))

	validate := func(c *reloadProbe) error {
		if c.Application.Version == "bad" {
			return errors.New("version is not acceptable")
		}
		return nil
	}

	if _, err := loader.LoadWithValidation(validate); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	writeProbeConfig(t, path, "bad")

	if err := loader.Reload(); !errors.Is(err, ErrConfigValidationFailed) {
		t.Errorf("reload error = %v, want ErrConfigValidationFailed", err)
	}
	if got := loader.GetCurrent().Application.Version; got != "v1" {
		t.Errorf("current version = %q, want the previous v1 to be kept", got)
	}
}
