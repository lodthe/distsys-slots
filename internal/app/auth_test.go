package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signedData(token string, u TelegramUser, at time.Time) string {
	raw, _ := json.Marshal(u)
	v := url.Values{"auth_date": {strconv.FormatInt(at.Unix(), 10)}, "user": {string(raw)}, "query_id": {"test-query"}}
	var lines []string
	for k := range v {
		lines = append(lines, k+"="+v.Get(k))
	}
	sort.Strings(lines)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	v.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return v.Encode()
}
func TestValidateInitData(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	token := "test-token"
	valid := signedData(token, TelegramUser{ID: 123}, now)
	u, err := ValidateInitData(valid, token, now)
	if err != nil || u.ID != 123 {
		t.Fatalf("valid auth: %v", err)
	}
	cases := map[string]string{"wrong_token": signedData("wrong", TelegramUser{ID: 123}, now), "expired": signedData(token, TelegramUser{ID: 123}, now.Add(-6*time.Minute)), "future": signedData(token, TelegramUser{ID: 123}, now.Add(time.Minute)), "duplicate": valid + "&auth_date=1", "invalid_user": signedData(token, TelegramUser{ID: 0}, now), "tampered": strings.Replace(valid, "test-query", "different", 1)}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateInitData(raw, token, now); err == nil {
				t.Fatal("invalid auth accepted")
			}
		})
	}
}
func TestWindowValidation(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	in := WindowInput{HomeworkID: 1, StartsAt: now.Add(time.Hour), EndsAt: now.Add(150 * time.Minute), SlotMinutes: 8}
	if err := in.validate(now); err != nil {
		t.Fatal(err)
	}
	end := in.EndsAt
	for _, invalid := range []time.Time{in.StartsAt, in.StartsAt.Add(-time.Minute)} {
		in.EndsAt = invalid
		if in.validate(now) == nil {
			t.Fatal("end must be strictly after start")
		}
	}
	in.EndsAt = end
	in.SlotMinutes = 0
	if in.validate(now) == nil {
		t.Fatal("zero duration accepted")
	}
}
