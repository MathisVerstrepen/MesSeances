package accounts

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"messeances/api/internal/schedule"
)

type followedActivityPosition struct {
	Version         int    `json:"v"`
	FollowsRevision string `json:"follows_revision"`
	UpperID         string `json:"upper_id"`
	LastDetectedAt  string `json:"last_detected_at"`
	LastID          string `json:"last_id"`
}

func (s *Service) activityMAC(owner int64, payload []byte) []byte {
	mac := hmac.New(sha256.New, s.hmacKey)
	_, _ = mac.Write([]byte("account-activity-v1\x00" + strconv.FormatInt(owner, 10) + "\x00"))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func (s *Service) encodeFollowedActivityCursor(owner int64, revision string, upper int64, event schedule.ActivityEvent) string {
	p := followedActivityPosition{Version: 1, FollowsRevision: revision, UpperID: strconv.FormatInt(upper, 10), LastDetectedAt: event.DetectedAt.UTC().Format(time.RFC3339Nano), LastID: event.EventID}
	payload, _ := json.Marshal(p)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(s.activityMAC(owner, payload))
}

func (s *Service) decodeFollowedActivityCursor(raw string, owner int64) (followedActivityPosition, error) {
	var p followedActivityPosition
	if len(raw) == 0 || len(raw) > 1024 || strings.ContainsAny(raw, "=\r\n") {
		return p, ErrInvalidInput
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return p, ErrInvalidInput
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil || !utf8.Valid(payload) {
		return p, ErrInvalidInput
	}
	mac, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || !hmac.Equal(mac, s.activityMAC(owner, payload)) {
		return p, ErrInvalidInput
	}
	d := json.NewDecoder(bytes.NewReader(payload))
	opening, err := d.Token()
	if err != nil || opening != json.Delim('{') {
		return p, ErrInvalidInput
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return p, ErrInvalidInput
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return p, ErrInvalidInput
		}
		switch name {
		case "v":
			if string(value) != "1" {
				return p, ErrInvalidInput
			}
		case "follows_revision", "upper_id", "last_detected_at", "last_id":
			if len(value) == 0 || value[0] != '"' {
				return p, ErrInvalidInput
			}
		default:
			return p, ErrInvalidInput
		}
	}
	closing, err := d.Token()
	var extra any
	if err != nil || closing != json.Delim('}') || len(seen) != 5 || d.Decode(&extra) != io.EOF || json.Unmarshal(payload, &p) != nil {
		return p, ErrInvalidInput
	}
	_, revisionErr := theaterRevision(p.FollowsRevision)
	upper, upperErr := schedule.ActivityID(p.UpperID)
	last, lastErr := schedule.ActivityID(p.LastID)
	at, timeErr := time.Parse(time.RFC3339Nano, p.LastDetectedAt)
	if revisionErr != nil || upperErr != nil || lastErr != nil || last > upper || timeErr != nil || at.IsZero() || !strings.HasSuffix(p.LastDetectedAt, "Z") || at.Nanosecond()%1000 != 0 || at.UTC().Format(time.RFC3339Nano) != p.LastDetectedAt {
		return p, ErrInvalidInput
	}
	return p, nil
}
