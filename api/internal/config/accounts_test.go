package config

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func accountConfigFixture() map[string]string {
	return map[string]string{
		"ACCOUNT_AVATAR_DIR": "/private/synthetic/avatars",
		"DATABASE_URL":       "postgres://unused", "ACCOUNTS_ENABLED": "true", "WEB_ORIGIN": "https://messeances.fr",
		"GOOGLE_CLIENT_ID": "synthetic.apps.googleusercontent.com", "GOOGLE_CLIENT_SECRET": "synthetic-client-secret",
		"AWS_REGION": "eu-west-3", "SES_FROM_EMAIL": "comptes@example.com", "SES_CONFIGURATION_SET": "accounts",
		"SES_FEEDBACK_TOPIC_ARN": "arn:aws:sns:eu-west-3:123456789012:accounts", "SES_IDENTITY_ARN": "arn:aws:ses:eu-west-3:123456789012:identity/example.com",
		"SES_FEEDBACK_QUEUE_URL": "https://sqs.eu-west-3.amazonaws.com/123456789012/accounts",
		"ACCOUNT_OUTBOX_KEY_ID":  "key-1", "ACCOUNT_OUTBOX_KEY": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)),
		"ACCOUNT_ADDRESS_HMAC_KEY": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)),
	}
}

func TestAccountsDisabledRequiresNoProviders(t *testing.T) {
	for _, enabled := range []string{"", "false"} {
		cfg, err := Load(APIBase, func(key string) string {
			switch key {
			case "DATABASE_URL":
				return "postgres://unused"
			case "ACCOUNTS_ENABLED":
				return enabled
			case "ACCOUNT_AVATAR_DIR", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "AWS_REGION", "ACCOUNT_OUTBOX_KEY", "ACCOUNT_ADDRESS_HMAC_KEY":
				t.Fatalf("disabled accounts read provider secret: %s", key)
			}
			return ""
		})
		if err != nil || cfg.Accounts != (AccountsConfig{}) {
			t.Fatalf("disabled config=%+v error=%v", cfg.Accounts, err)
		}
	}
}

func TestEnabledAccountConfiguration(t *testing.T) {
	env := accountConfigFixture()
	cfg, err := Load(APIBase, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Accounts.Enabled || !cfg.Accounts.SecureCookies || cfg.Accounts.GoogleCallbackURL != "https://messeances.fr/api/v1/auth/google/callback" {
		t.Fatal("enabled config mismatch")
	}
	for _, origin := range []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://[::1]:3000"} {
		env["WEB_ORIGIN"] = origin
		cfg, err := Load(APIBase, func(key string) string { return env[key] })
		if err != nil || cfg.Accounts.SecureCookies || cfg.Accounts.GoogleCallbackURL != origin+"/api/v1/auth/google/callback" {
			t.Fatalf("localhost configuration failed: %v", err)
		}
	}
}

func TestAccountConfigurationFailsGenerically(t *testing.T) {
	for key := range accountConfigFixture() {
		if key == "WEB_ORIGIN" || key == "ACCOUNTS_ENABLED" {
			continue
		}
		t.Run("missing "+key, func(t *testing.T) {
			env := accountConfigFixture()
			delete(env, key)
			if _, err := Load(APIBase, func(key string) string { return env[key] }); err == nil || err.Error() != "configuration error" {
				t.Fatalf("error=%v", err)
			}
		})
	}
	for _, tc := range []struct{ key, value string }{
		{"ACCOUNTS_ENABLED", "TRUE"}, {"WEB_ORIGIN", "http://messeances.fr"}, {"GOOGLE_CLIENT_SECRET", "private\nvalue"},
		{"ACCOUNT_AVATAR_DIR", "/"}, {"ACCOUNT_AVATAR_DIR", "relative"}, {"ACCOUNT_AVATAR_DIR", "/private\npath"},
		{"SES_FROM_EMAIL", "Admin <admin@example.com>"}, {"SES_FEEDBACK_QUEUE_URL", "https://attacker.example/queue"},
		{"SES_FEEDBACK_QUEUE_URL", "https://sqs.eu-west-3.amazonaws.com/123456789012/accounts#"},
		{"SES_FEEDBACK_QUEUE_URL", "https://sqs.eu-west-3.amazonaws.com/999999999999/accounts"},
		{"SES_FEEDBACK_QUEUE_URL", "https://sqs.eu-west-3.amazonaws.com/123456789012/accounts.fifo"},
		{"SES_FEEDBACK_TOPIC_ARN", "arn:aws:sns:eu-west-3:123456789012:accounts.fifo"},
		{"SES_FEEDBACK_TOPIC_ARN", "arn:aws:sns:eu-west-1:123456789012:accounts"},
		{"SES_IDENTITY_ARN", "arn:aws:ses:eu-west-3:123456789012:identity/attacker.example"},
		{"ACCOUNT_OUTBOX_KEY", strings.Repeat("x", 44)}, {"ACCOUNT_OUTBOX_KEY_ID", "key/1"},
		{"ACCOUNT_ADDRESS_HMAC_KEY", accountConfigFixture()["ACCOUNT_OUTBOX_KEY"]},
	} {
		env := accountConfigFixture()
		env[tc.key] = tc.value
		if _, err := Load(APIBase, func(key string) string { return env[key] }); err == nil || err.Error() != "configuration error" {
			t.Errorf("invalid %s error=%v", tc.key, err)
		}
	}
}
