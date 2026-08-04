package config

import "testing"

func TestLoadRequiresNamedEnvironmentVariable(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", "localhost:9094")
	t.Setenv("JWT_SECRET", "test-secret")

	if _, err := Load(); err == nil {
		t.Fatal("expected missing environment variable error")
	} else if got, want := MissingKey(err), "POSTGRES_USER"; got != want {
		t.Fatalf("missing key: got %q, want %q", got, want)
	}
}

func TestLoadParsesConfiguredEnvironment(t *testing.T) {
	for key, value := range map[string]string{
		"KAFKA_BROKERS":     "localhost:9094,localhost:9095",
		"JWT_SECRET":        "test-secret",
		"POSTGRES_USER":     "auction",
		"POSTGRES_PASSWORD": "password",
		"POSTGRES_PORT":     "5432",
	} {
		t.Setenv(key, value)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.KafkaBrokers) != 2 || cfg.KafkaBrokers[0] != "localhost:9094" {
		t.Fatalf("unexpected brokers: %#v", cfg.KafkaBrokers)
	}
	if cfg.JWTSecret != "test-secret" || cfg.PostgresUser != "auction" || cfg.PostgresPort != 5432 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}
