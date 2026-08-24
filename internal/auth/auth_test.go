package auth

import "testing"

func validConfig() Config {
	return Config{
		Domain:       "tenant.eu.auth0.com",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		CallbackURL:  "http://localhost:8080/auth/callback",
	}
}

func TestNewManager_RequiresFullConfig(t *testing.T) {
	if _, err := NewManager(validConfig()); err != nil {
		t.Fatalf("expected a fully populated config to succeed, got %v", err)
	}

	blank := func(mutate func(*Config)) Config {
		c := validConfig()
		mutate(&c)
		return c
	}
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing domain", blank(func(c *Config) { c.Domain = "" })},
		{"missing client id", blank(func(c *Config) { c.ClientID = "" })},
		{"missing client secret", blank(func(c *Config) { c.ClientSecret = "" })},
		{"missing callback url", blank(func(c *Config) { c.CallbackURL = "" })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewManager(tc.cfg); err == nil {
				t.Fatalf("expected an error with %s, got none", tc.name)
			}
		})
	}
}

func TestBaseURL(t *testing.T) {
	cases := []struct {
		domain string
		want   string
	}{
		{"tenant.eu.auth0.com", "https://tenant.eu.auth0.com"},
		{"http://localhost:9999", "http://localhost:9999"},
		{"https://example.test", "https://example.test"},
		{"https://example.test/", "https://example.test"},
	}
	for _, tc := range cases {
		if got := baseURL(tc.domain); got != tc.want {
			t.Errorf("baseURL(%q) = %q, want %q", tc.domain, got, tc.want)
		}
	}
}

func TestNewManager_UsesBaseURLForEndpoints(t *testing.T) {
	cfg := validConfig()
	cfg.Domain = "http://localhost:9999"
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if got, want := m.oauthCfg.Endpoint.AuthURL, "http://localhost:9999/authorize"; got != want {
		t.Errorf("AuthURL = %q, want %q", got, want)
	}
	if got, want := m.oauthCfg.Endpoint.TokenURL, "http://localhost:9999/oauth/token"; got != want {
		t.Errorf("TokenURL = %q, want %q", got, want)
	}
	if got, want := m.issuerBase, "http://localhost:9999"; got != want {
		t.Errorf("issuerBase = %q, want %q", got, want)
	}
}
