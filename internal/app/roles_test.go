package app

import (
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestGrantRoleRejectsAmbiguousUsername(t *testing.T) {
	f := setup(t)
	first, second := f.student(t, 8001), f.student(t, 8002)
	if _, err := f.db.Exec(f.ctx, `UPDATE users SET username='reused_name' WHERE id=ANY($1)`, []int64{first, second}); err != nil {
		t.Fatal(err)
	}
	if err := f.GrantRole(f.ctx, 0, "@reused_name", "admin"); err == nil {
		t.Fatal("ambiguous username granted admin rights")
	}
	var count int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM user_roles WHERE role='admin'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed grant changed roles", count, err)
	}
	for range 2 {
		if err := f.GrantRole(f.ctx, 8002, "", "admin"); err != nil {
			t.Fatal(err)
		}
	}
	var owner int64
	if err := f.db.QueryRow(f.ctx, `SELECT user_id FROM user_roles WHERE role='admin'`).Scan(&owner); err != nil || owner != second {
		t.Fatal("numeric Telegram ID did not select the intended user", owner, err)
	}
}

func TestPendingRoleClaimRequiresVerifiedUsername(t *testing.T) {
	f := setup(t)
	for _, name := range []string{"@Future_Helper", "future_helper"} {
		if err := f.GrantRole(f.ctx, 0, name, "assistant"); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM pending_role_grants WHERE username='future_helper'`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	login := func(token string, telegramID int64, username string, want int) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"init_data": signedData(token, TelegramUser{ID: telegramID, Username: username}, time.Now())})
		rec := f.request(t, "POST", "/api/v1/auth/telegram", string(raw), 0, false)
		if rec.Code != want {
			t.Fatal(rec.Code, rec.Body.String())
		}
		return rec
	}
	login("forged-token", 9001, "future_helper", 401)
	login(f.cfg.BotToken, 9002, "another_helper", 200)
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM pending_role_grants WHERE username='future_helper'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("unverified or different username consumed invitation", count, err)
	}
	rec := login(f.cfg.BotToken, 9001, "FUTURE_helper", 200)
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	me := httptest.NewRecorder()
	f.Handler().ServeHTTP(me, req)
	var body struct {
		User User `json:"user"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &body); err != nil || me.Code != 200 || !body.User.Assistant || body.User.Admin {
		t.Fatal("role not available on first login", me.Body.String(), err)
	}
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM pending_role_grants`).Scan(&count); err != nil || count != 0 {
		t.Fatal("grant not consumed", count, err)
	}
	login(f.cfg.BotToken, 9001, "new_username", 200)
	login(f.cfg.BotToken, 9003, "future_helper", 200)
	var owner int64
	if err := f.db.QueryRow(f.ctx, `SELECT u.telegram_id FROM user_roles r JOIN users u ON u.id=r.user_id WHERE r.role='assistant' AND u.telegram_id IN (9001,9003)`).Scan(&owner); err != nil || owner != 9001 {
		t.Fatal("role did not stay with Telegram ID", owner, err)
	}
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM user_roles r JOIN users u ON u.id=r.user_id WHERE u.telegram_id=9003`).Scan(&count); err != nil || count != 0 {
		t.Fatal("username reuse inherited role", count, err)
	}
}

func TestUsernameGrantAndFirstLoginRace(t *testing.T) {
	f := setup(t)
	for i := range 8 {
		telegramID := int64(9100 + i)
		username := "race_helper_" + string(rune('a'+i))
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if err := f.GrantRole(f.ctx, 0, username, "assistant"); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			raw, _ := json.Marshal(map[string]string{"init_data": signedData(f.cfg.BotToken, TelegramUser{ID: telegramID, Username: username}, time.Now())})
			if rec := f.request(t, "POST", "/api/v1/auth/telegram", string(raw), 0, false); rec.Code != 200 {
				t.Error(rec.Code, rec.Body.String())
			}
		}()
		close(start)
		wg.Wait()
		var granted bool
		if err := f.db.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM user_roles r JOIN users u ON u.id=r.user_id WHERE u.telegram_id=$1 AND r.role='assistant')`, telegramID).Scan(&granted); err != nil || !granted {
			t.Fatal("first login missed assignment", granted, err)
		}
	}
	for _, username := range []string{"@", "bad name", "https://t.me/name", "@@name", "юзернейм"} {
		if err := f.GrantRole(f.ctx, 0, username, "assistant"); err == nil {
			t.Fatal("invalid username accepted", username)
		}
	}
	var count int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM pending_role_grants`).Scan(&count); err != nil || count != 0 {
		t.Fatal("unexpected pending grants", count, err)
	}
}
