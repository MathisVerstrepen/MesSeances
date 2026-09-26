package config

import (
	"bytes"
	"encoding/base64"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"messeances/api/internal/accounts"
)

type AccountsConfig struct {
	Enabled             bool
	AvatarDir           string
	SecureCookies       bool
	GoogleClientID      string
	GoogleClientSecret  string
	GoogleCallbackURL   string
	AWSRegion           string
	SESFromEmail        string
	SESFromName         string
	SESConfigurationSet string
	SESFeedbackQueueURL string
	SESFeedbackTopicARN string
	SESIdentityARN      string
	OutboxKeyID         string
	OutboxKey           [32]byte
	AddressHMACKey      [32]byte
}

func loadAccounts(origin string, getenv func(string) string) (AccountsConfig, error) {
	var cfg AccountsConfig
	switch getenv("ACCOUNTS_ENABLED") {
	case "", "false":
		return cfg, nil
	case "true":
		cfg.Enabled = true
	default:
		return AccountsConfig{}, configurationError()
	}
	u, err := url.Parse(origin)
	if err != nil || !validOrigin(origin) || (u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") {
		return AccountsConfig{}, configurationError()
	}
	cfg.SecureCookies = u.Scheme == "https"
	cfg.AvatarDir = getenv("ACCOUNT_AVATAR_DIR")
	if !filepath.IsAbs(cfg.AvatarDir) || filepath.Clean(cfg.AvatarDir) == "/" || strings.ContainsAny(cfg.AvatarDir, "\x00\r\n") {
		return AccountsConfig{}, configurationError()
	}
	cfg.GoogleCallbackURL = origin + "/api/v1/auth/google/callback"
	cfg.GoogleClientID = getenv("GOOGLE_CLIENT_ID")
	cfg.GoogleClientSecret = getenv("GOOGLE_CLIENT_SECRET")
	if !safeConfigValue(cfg.GoogleClientID, 256) || !strings.HasSuffix(cfg.GoogleClientID, ".apps.googleusercontent.com") || !safeConfigValue(cfg.GoogleClientSecret, 512) {
		return AccountsConfig{}, configurationError()
	}
	cfg.AWSRegion = getenv("AWS_REGION")
	if !regexp.MustCompile(`^[a-z]{2}-[a-z]+-[1-9][0-9]*$`).MatchString(cfg.AWSRegion) {
		return AccountsConfig{}, configurationError()
	}
	cfg.SESFromEmail = getenv("SES_FROM_EMAIL")
	if email, err := accounts.NormalizeEmail(cfg.SESFromEmail); err != nil || email != cfg.SESFromEmail {
		return AccountsConfig{}, configurationError()
	}
	cfg.SESFromName = getenv("SES_FROM_NAME")
	if !validSenderName(cfg.SESFromName) {
		return AccountsConfig{}, configurationError()
	}
	cfg.SESConfigurationSet = getenv("SES_CONFIGURATION_SET")
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(cfg.SESConfigurationSet) {
		return AccountsConfig{}, configurationError()
	}
	cfg.SESFeedbackTopicARN = getenv("SES_FEEDBACK_TOPIC_ARN")
	topic := strings.Split(cfg.SESFeedbackTopicARN, ":")
	if len(topic) != 6 || topic[0] != "arn" || topic[1] != "aws" || topic[2] != "sns" || topic[3] != cfg.AWSRegion || !regexp.MustCompile(`^[0-9]{12}$`).MatchString(topic[4]) || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`).MatchString(topic[5]) {
		return AccountsConfig{}, configurationError()
	}
	cfg.SESIdentityARN = getenv("SES_IDENTITY_ARN")
	prefix := "arn:aws:ses:" + cfg.AWSRegion + ":" + topic[4] + ":identity/"
	if !strings.HasPrefix(cfg.SESIdentityARN, prefix) {
		return AccountsConfig{}, configurationError()
	}
	identity := strings.TrimPrefix(cfg.SESIdentityARN, prefix)
	_, senderDomain, _ := strings.Cut(cfg.SESFromEmail, "@")
	if identity != cfg.SESFromEmail && identity != senderDomain {
		return AccountsConfig{}, configurationError()
	}
	cfg.SESFeedbackQueueURL = getenv("SES_FEEDBACK_QUEUE_URL")
	queue, err := url.Parse(cfg.SESFeedbackQueueURL)
	if err != nil || queue.Scheme != "https" || queue.Host != "sqs."+cfg.AWSRegion+".amazonaws.com" || queue.User != nil || queue.RawQuery != "" || queue.ForceQuery || queue.Fragment != "" || queue.RawPath != "" || strings.Contains(cfg.SESFeedbackQueueURL, "#") {
		return AccountsConfig{}, configurationError()
	}
	path := strings.Split(queue.Path, "/")
	if len(path) != 3 || path[0] != "" || path[1] != topic[4] || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`).MatchString(path[2]) {
		return AccountsConfig{}, configurationError()
	}
	cfg.OutboxKeyID = getenv("ACCOUNT_OUTBOX_KEY_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(cfg.OutboxKeyID) {
		return AccountsConfig{}, configurationError()
	}
	outbox, err := decodeAccountKey(getenv("ACCOUNT_OUTBOX_KEY"))
	if err != nil {
		return AccountsConfig{}, err
	}
	hmacKey, err := decodeAccountKey(getenv("ACCOUNT_ADDRESS_HMAC_KEY"))
	if err != nil || outbox == hmacKey {
		return AccountsConfig{}, configurationError()
	}
	cfg.OutboxKey, cfg.AddressHMACKey = outbox, hmacKey
	return cfg, nil
}

func validSenderName(name string) bool {
	if name == "" || len(name) > 256 || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func safeConfigValue(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for _, c := range []byte(value) {
		if c <= 32 || c >= 127 {
			return false
		}
	}
	return true
}

func decodeAccountKey(raw string) ([32]byte, error) {
	var key [32]byte
	decoded, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != len(key) || base64.StdEncoding.EncodeToString(decoded) != raw || bytes.Equal(decoded, key[:]) {
		return key, configurationError()
	}
	copy(key[:], decoded)
	return key, nil
}
