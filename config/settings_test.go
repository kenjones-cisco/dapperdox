package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
)

func TestLoadSettingsPrecedenceAndIsolation(t *testing.T) {
	config.Restore()
	t.Cleanup(config.Restore)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("log-level: warn\nsite-url: https://file.example/\nproxy:\n  path:\n    /api: https://upstream.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	viper.Set("config-dir", root)
	t.Setenv("LOGLEVEL", "debug")

	flags := pflag.NewFlagSet("settings", pflag.ContinueOnError)
	flags.String(config.LogLevel, "info", "")

	if err := flags.Parse([]string{"--log-level=error"}); err != nil {
		t.Fatal(err)
	}

	if err := viper.BindPFlags(flags); err != nil {
		t.Fatal(err)
	}

	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	if settings.LogLevel != "error" || settings.SiteURL != "https://file.example/" || settings.DiscoveryNamespace != "default" {
		t.Fatalf("configuration precedence changed: %+v", settings)
	}

	cloned := settings.Clone()

	settings.ProxyPath["/api"] = "changed"
	if cloned.ProxyPath["/api"] != "https://upstream.example" {
		t.Fatal("snapshot shares mutable configuration")
	}

	config.Restore()
	viper.Set("config-dir", root)

	settings, err = config.Load()
	if err != nil || settings.LogLevel != "debug" {
		t.Fatalf("environment did not override file: %q, %v", settings.LogLevel, err)
	}
}

func TestLoadRejectsMalformedConfiguration(t *testing.T) {
	for _, contents := range []string{
		"discovery: [",
		"discovery:\n  periodtime: not-a-duration\n",
		"discovery:\n  enabled: not-a-boolean\n",
		"discovery:\n  enabled: true\n  periodtime: 0s\n",
	} {
		t.Run(contents, func(t *testing.T) {
			config.Restore()
			t.Cleanup(config.Restore)

			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}

			viper.Set("config-dir", root)

			if _, err := config.Load(); err == nil {
				t.Fatal("invalid configuration was silently accepted")
			}
		})
	}
}
