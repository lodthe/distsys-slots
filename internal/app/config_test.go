package app

import "testing"

func TestConfigRequiresExplicitHTTPSOrigin(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	for _, origin := range []string{"", "http://example.com", "https://", "https://example.com/app", "https://user:password@example.com", "https://example.com?secret=value", "https://example.com?", "https://example.com#fragment"} {
		t.Run(origin, func(t *testing.T) {
			t.Setenv("APP_BASE_URL", origin)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatal("accepted missing or invalid HTTPS origin")
			}
		})
	}
	for _, origin := range []string{"https://example.com", "https://example.com:8443"} {
		t.Run(origin, func(t *testing.T) {
			t.Setenv("APP_BASE_URL", origin+"/")
			cfg, err := ConfigFromEnv()
			if err != nil || cfg.BaseURL != origin {
				t.Fatalf("origin = %q, error = %v", cfg.BaseURL, err)
			}
		})
	}
}
