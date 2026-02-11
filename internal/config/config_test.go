package config

import (
	"os"
	"testing"
)

func setEnvForTest(t *testing.T, key, value string) {
	t.Helper()
	oldValue, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("Setenv(%s) failed: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, oldValue)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	oldValue, had := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, oldValue)
		}
	})
}

func TestGetEnv(t *testing.T) {
	t.Parallel()

	unsetEnvForTest(t, "TEST_CONFIG_KEY")
	if got := getEnv("TEST_CONFIG_KEY", "fallback"); got != "fallback" {
		t.Fatalf("getEnv missing key: got %q want %q", got, "fallback")
	}

	setEnvForTest(t, "TEST_CONFIG_KEY", "value")
	if got := getEnv("TEST_CONFIG_KEY", "fallback"); got != "value" {
		t.Fatalf("getEnv set key: got %q want %q", got, "value")
	}
}

func TestLoadUsesDefaultsAndOverrides(t *testing.T) {
	t.Parallel()

	unsetEnvForTest(t, "PORT")
	unsetEnvForTest(t, "FRONTEND_URL")
	unsetEnvForTest(t, "ENABLE_SCHEDULER")
	unsetEnvForTest(t, "DEFAULT_UPDATE_FREQUENCY")

	cfgDefault := Load()
	if cfgDefault.Port != "8080" {
		t.Fatalf("default Port: got %q want %q", cfgDefault.Port, "8080")
	}
	if cfgDefault.FrontendURL != "http://localhost:3000" {
		t.Fatalf("default FrontendURL: got %q want %q", cfgDefault.FrontendURL, "http://localhost:3000")
	}
	if cfgDefault.EnableScheduler {
		t.Fatalf("default EnableScheduler: got true want false")
	}
	if cfgDefault.DefaultUpdateFrequency != "daily" {
		t.Fatalf("default DefaultUpdateFrequency: got %q want %q", cfgDefault.DefaultUpdateFrequency, "daily")
	}

	setEnvForTest(t, "PORT", "9090")
	setEnvForTest(t, "FRONTEND_URL", "https://example.app")
	setEnvForTest(t, "ENABLE_SCHEDULER", "true")
	setEnvForTest(t, "DEFAULT_UPDATE_FREQUENCY", "weekly")

	cfgOverride := Load()
	if cfgOverride.Port != "9090" {
		t.Fatalf("override Port: got %q want %q", cfgOverride.Port, "9090")
	}
	if cfgOverride.FrontendURL != "https://example.app" {
		t.Fatalf("override FrontendURL: got %q want %q", cfgOverride.FrontendURL, "https://example.app")
	}
	if !cfgOverride.EnableScheduler {
		t.Fatalf("override EnableScheduler: got false want true")
	}
	if cfgOverride.DefaultUpdateFrequency != "weekly" {
		t.Fatalf("override DefaultUpdateFrequency: got %q want %q", cfgOverride.DefaultUpdateFrequency, "weekly")
	}
}
