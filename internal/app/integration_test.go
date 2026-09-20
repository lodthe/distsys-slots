package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	*Server
	ctx           context.Context
	assistant, hw int64
	start         time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	connection := os.Getenv("TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + randomToken()[:16]
	if _, err = root.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(connection)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 60
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = root.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); root.Close() })
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	f := &fixture{Server: NewServer(db, Config{BaseURL: "https://example.com", BotToken: "test-token", WebhookSecret: strings.Repeat("s", 32)}), ctx: ctx, start: time.Now().UTC().Truncate(time.Minute).Add(24 * time.Hour)}
	f.assistant = f.student(t, 1000)
	if _, err = db.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'assistant')`, f.assistant); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `INSERT INTO homeworks(number) VALUES(1) RETURNING id`).Scan(&f.hw); err != nil {
		t.Fatal(err)
	}
	f.setDates(t, f.hw)
	return f
}
func (f *fixture) setDates(t *testing.T, homework int64) {
	t.Helper()
	_, err := f.db.Exec(f.ctx, `INSERT INTO homework_defense_dates(homework_id,defense_date)
		SELECT $1, ($2::timestamptz AT TIME ZONE 'Europe/Moscow')::date + n
		FROM generate_series(0,59) AS n`, homework, f.start)
	if err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) student(t *testing.T, tg int64) int64 {
	t.Helper()
	var id int64
	if err := f.db.QueryRow(f.ctx, `INSERT INTO users(telegram_id,full_name,repository_username) VALUES($1,$2,'repo_'||$3) RETURNING id`, tg, fmt.Sprintf("Student %d", tg), fmt.Sprintf("%d", tg)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *fixture) window(at time.Time) WindowInput {
	return WindowInput{HomeworkID: f.hw, StartsAt: at, EndsAt: at.Add(90 * time.Minute), SlotMinutes: 8}
}
func (f *fixture) publish(t *testing.T, in ...WindowInput) ([]int64, []int64) {
	t.Helper()
	var ids []int64
	for _, window := range in {
		id, err := f.publishWindow(f.ctx, f.assistant, randomToken(), window)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows, err := f.db.Query(f.ctx, `SELECT id FROM slots WHERE window_id=ANY($1) ORDER BY starts_at,id`, ids)
	if err != nil {
		t.Fatal(err)
	}
	slots, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		t.Fatal(err)
	}
	return ids, slots
}
func assertConflict(t *testing.T, err error) {
	t.Helper()
	var p *problem
	if !errors.As(err, &p) || p.Status != 409 {
		t.Fatalf("expected conflict, got %v", err)
	}
}
func TestConcurrentBooking(t *testing.T) {
	f := setup(t)
	_, slots := f.publish(t, f.window(f.start))
	var students []int64
	for i := 0; i < 10; i++ {
		students = append(students, f.student(t, int64(i+2000)))
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, id := range students {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			<-start
			_, err := f.bookSlot(f.ctx, id, slots[0])
			if err == nil {
				wins.Add(1)
			} else {
				var p *problem
				if !errors.As(err, &p) || p.Status != 409 {
					t.Errorf("unexpected booking error: %v", err)
				}
			}
		}(id)
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("winners: %d", wins.Load())
	}
	var count int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM bookings WHERE status='confirmed'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("confirmed=%d err=%v", count, err)
	}
	var notices int
	_ = f.db.QueryRow(f.ctx, `SELECT count(*) FROM notification_outbox`).Scan(&notices)
	if notices != 1 {
		t.Fatal("outbox events", notices)
	}
}
func TestStudentParallelSlotsAndCancellation(t *testing.T) {
	f := setup(t)
	_, slots := f.publish(t, f.window(f.start))
	student := f.student(t, 2001)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, slot := range slots[:2] {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			if _, err := f.bookSlot(f.ctx, student, id); err == nil {
				wins.Add(1)
			} else {
				assertConflict(t, err)
				if !strings.Contains(err.Error(), "У вас уже есть запись на это ДЗ") {
					t.Error("missing actionable conflict reason", err)
				}
			}
		}(slot)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("parallel student bookings", wins.Load())
	}
	var booking, slot int64
	_ = f.db.QueryRow(f.ctx, `SELECT id,slot_id FROM bookings WHERE student_id=$1 AND status='confirmed'`, student).Scan(&booking, &slot)
	same, err := f.bookSlot(f.ctx, student, slot)
	if err != nil || same != booking {
		t.Fatal("booking not idempotent", err)
	}
	if err = f.cancelStudent(f.ctx, student, booking); err != nil {
		t.Fatal(err)
	}
	if err = f.cancelStudent(f.ctx, student, booking); err != nil {
		t.Fatal(err)
	}
	other := f.student(t, 2002)
	if _, err = f.bookSlot(f.ctx, other, slot); err != nil {
		t.Fatal("slot did not reopen", err)
	}
	if err = f.cancelStudent(f.ctx, other, booking); err == nil {
		t.Fatal("foreign cancellation succeeded")
	}
}
func TestPublishWindowAndIdempotency(t *testing.T) {
	f := setup(t)
	in := f.window(f.start)
	key := randomToken()
	id, err := f.publishWindow(f.ctx, f.assistant, key, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.publishWindow(f.ctx, f.assistant, key, in)
	if err != nil || again != id {
		t.Fatal("idempotency", err)
	}
	var count int
	_ = f.db.QueryRow(f.ctx, `SELECT count(*) FROM slots`).Scan(&count)
	if count != 11 {
		t.Fatal("slot count", count)
	}
	var end time.Time
	_ = f.db.QueryRow(f.ctx, `SELECT max(ends_at) FROM slots`).Scan(&end)
	if !end.Equal(f.start.Add(88 * time.Minute)) {
		t.Fatal("unexpected remainder", end)
	}
	changed := in
	changed.StartsAt = changed.StartsAt.Add(time.Minute)
	_, err = f.publishWindow(f.ctx, f.assistant, key, changed)
	assertConflict(t, err)
	invalid := in
	invalid.EndsAt = invalid.StartsAt
	_, err = f.publishWindow(f.ctx, f.assistant, randomToken(), invalid)
	if err == nil {
		t.Fatal("invalid window accepted")
	}
	_ = f.db.QueryRow(f.ctx, `SELECT count(*) FROM availability_windows`).Scan(&count)
	if count != 1 {
		t.Fatal("invalid publication changed windows", count)
	}
	f.publish(t, in, in, f.window(in.EndsAt))
}
func TestConcurrentOverlappingWindowPublication(t *testing.T) {
	f := setup(t)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.publishWindow(f.ctx, f.assistant, randomToken(), f.window(f.start))
			if err == nil {
				wins.Add(1)
			} else {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 2 {
		t.Fatal("overlapping windows must be accepted", wins.Load())
	}
}
func TestBookingVersusWindowCancellation(t *testing.T) {
	f := setup(t)
	for i := 0; i < 12; i++ {
		windows, slots := f.publish(t, f.window(f.start.Add(time.Duration(i)*24*time.Hour)))
		student := f.student(t, int64(3000+i))
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := f.bookSlot(f.ctx, student, slots[0])
			if err != nil {
				assertConflict(t, err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := f.cancelAssistant(f.ctx, f.assistant, windows[0], true, "Изменилось расписание"); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
	}
	var invalid int
	err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM bookings b JOIN slots s ON s.id=b.slot_id JOIN availability_windows w ON w.id=s.window_id WHERE b.status='confirmed' AND (s.status='cancelled' OR w.status='cancelled')`).Scan(&invalid)
	if err != nil || invalid != 0 {
		t.Fatal("active booking in cancelled window", invalid, err)
	}
}
func TestParallelStudentAssistantCancellation(t *testing.T) {
	f := setup(t)
	windows, slots := f.publish(t, f.window(f.start))
	student := f.student(t, 2001)
	booking, err := f.bookSlot(f.ctx, student, slots[0])
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := f.cancelStudent(f.ctx, student, booking); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := f.cancelAssistant(f.ctx, f.assistant, windows[0], true, "Отмена"); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	_, err = f.bookSlot(f.ctx, f.student(t, 2002), slots[0])
	assertConflict(t, err)
}
func TestCrossHomeworkOverlapAndReason(t *testing.T) {
	f := setup(t)
	_, slots := f.publish(t, f.window(f.start))
	student := f.student(t, 2001)
	if _, err := f.bookSlot(f.ctx, student, slots[0]); err != nil {
		t.Fatal(err)
	}
	var secondHW int64
	_ = f.db.QueryRow(f.ctx, `INSERT INTO homeworks(number) VALUES(2) RETURNING id`).Scan(&secondHW)
	secondAssistant := f.student(t, 2002)
	in := f.window(f.start)
	in.HomeworkID = secondHW
	f.setDates(t, secondHW)
	id, err := f.publishWindow(f.ctx, secondAssistant, randomToken(), in)
	if err != nil {
		t.Fatal(err)
	}
	var first, second int64
	_ = f.db.QueryRow(f.ctx, `SELECT min(id),min(id)+1 FROM slots WHERE window_id=$1`, id).Scan(&first, &second)
	_, err = f.bookSlot(f.ctx, student, first)
	assertConflict(t, err)
	if !strings.Contains(err.Error(), "пересекается") {
		t.Fatal("missing specific overlap explanation", err)
	}
	if _, err = f.bookSlot(f.ctx, student, second); err != nil {
		t.Fatal("adjacent booking rejected", err)
	}
	if err = f.cancelAssistant(f.ctx, f.assistant, slots[0], false, ""); err != nil {
		t.Fatal("empty reason should be allowed", err)
	}
	if err = f.cancelAssistant(f.ctx, secondAssistant, slots[0], false, "No"); err == nil {
		t.Fatal("foreign assistant cancellation succeeded")
	}
}
func TestWaitingUntilSlotStarts(t *testing.T) {
	f := setup(t)
	windows, slots := f.publish(t, f.window(f.start))
	student := f.student(t, 2001)
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `SELECT id FROM availability_windows WHERE id=$1 FOR UPDATE`, windows[0]); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := f.bookSlot(f.ctx, student, slots[0]); done <- err }()
	if _, err = tx.Exec(f.ctx, `UPDATE slots SET starts_at=clock_timestamp()-interval '1 second' WHERE id=$1`, slots[0]); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	assertConflict(t, <-done)
}
func (f *fixture) request(t *testing.T, method, path, body string, student int64, csrfOK bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Origin", f.cfg.BaseURL)
	if student != 0 {
		token := randomToken()
		_, err := f.db.Exec(f.ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '1 hour')`, digest(token), student)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: "distsys_session", Value: token})
		if csrfOK {
			req.Header.Set("X-CSRF-Token", csrf(token))
		}
	}
	rec := httptest.NewRecorder()
	f.Handler().ServeHTTP(rec, req)
	return rec
}
func TestHTTPAuthorizationAndPrivacy(t *testing.T) {
	f := setup(t)
	windows, slots := f.publish(t, f.window(f.start))
	student := f.student(t, 2001)
	other := f.student(t, 2002)
	if _, err := f.bookSlot(f.ctx, student, slots[0]); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, path, body string
		user               int64
		csrf               bool
		want               int
	}{{"GET", "/api/v1/me", "", 0, false, 401}, {"POST", "/api/v1/assistant/windows", "{}", student, true, 403}, {"PATCH", "/api/v1/me", `{"full_name":"Иван Иванов","student_group":"БПМИ"}`, student, false, 403}, {"GET", fmt.Sprintf("/api/v1/assistant/windows/%d", windows[0]), "", other, true, 403}, {"GET", "/api/v1/slots/1", "", student, true, 200}}
	for _, c := range cases {
		rec := f.request(t, c.method, c.path, c.body, c.user, c.csrf)
		if rec.Code != c.want {
			t.Fatalf("%s: %d %s", c.path, rec.Code, rec.Body.String())
		}
	}
	rec := f.request(t, "GET", fmt.Sprintf("/api/v1/slots/%d", slots[0]), "", other, true)
	for _, forbidden := range []string{"Student 2001", "student_group", "telegram_id"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatal("student data exposed", rec.Body.String())
		}
	}
	raw, _ := json.Marshal(map[string]string{"init_data": signedData(f.cfg.BotToken, TelegramUser{ID: 9999}, time.Now())})
	rec = f.request(t, "POST", "/api/v1/auth/telegram", string(raw), 0, false)
	if rec.Code != 200 || len(rec.Result().Cookies()) != 1 {
		t.Fatal("login", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly {
		t.Fatal("insecure cookie")
	}
	req := httptest.NewRequest("POST", "/api/v1/auth/telegram", bytes.NewReader(raw))
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	f.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("cross-origin accepted")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
func TestOutboxRetryAndBlockedBot(t *testing.T) {
	f := setup(t)
	_, slots := f.publish(t, f.window(f.start))
	if _, err := f.bookSlot(f.ctx, f.student(t, 2001), slots[0]); err != nil {
		t.Fatal(err)
	}
	f.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New("network offline") })}
	found, err := f.deliverOne(f.ctx)
	if err != nil || !found {
		t.Fatal(err)
	}
	var status string
	var attempts int
	_ = f.db.QueryRow(f.ctx, `SELECT status,attempts FROM notification_outbox`).Scan(&status, &attempts)
	if status != "pending" || attempts != 1 {
		t.Fatal(status, attempts)
	}
	_, _ = f.db.Exec(f.ctx, `UPDATE notification_outbox SET next_attempt_at=now()`)
	f.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error_code":403}`)), Header: http.Header{}}, nil
	})}
	_, err = f.deliverOne(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.db.QueryRow(f.ctx, `SELECT status FROM notification_outbox`).Scan(&status)
	if status != "failed" {
		t.Fatal(status)
	}
	var count int
	_ = f.db.QueryRow(f.ctx, `SELECT count(*) FROM bookings WHERE status='confirmed'`).Scan(&count)
	if count != 1 {
		t.Fatal("delivery failure changed booking")
	}
}

func TestReadinessAndSessionExpiry(t *testing.T) {
	f := setup(t)
	if err := Migrate(f.ctx, f.db); err != nil {
		t.Fatal("migration reapply", err)
	}
	rec := httptest.NewRecorder()
	f.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 200 {
		t.Fatal("healthy database not ready")
	}
	token := randomToken()
	_, err := f.db.Exec(f.ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()-interval '1 second')`, digest(token), f.assistant)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: "distsys_session", Value: token})
	rec = httptest.NewRecorder()
	f.Handler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("expired session accepted")
	}
	f.db.Close()
	rec = httptest.NewRecorder()
	f.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 503 {
		t.Fatal("unavailable database marked ready")
	}
}

func TestWebhookDeduplicationAndSuccessfulDelivery(t *testing.T) {
	f := setup(t)
	body := `{"update_id":1,"message":{"text":"/start","chat":{"id":777,"type":"private"}}}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/telegram/webhook", strings.NewReader(body))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", f.cfg.WebhookSecret)
		rec := httptest.NewRecorder()
		f.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	var count int
	_ = f.db.QueryRow(f.ctx, `SELECT count(*) FROM notification_outbox`).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate webhook event", count)
	}
	req := httptest.NewRequest("POST", "/telegram/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()
	f.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("webhook without secret accepted")
	}
	f.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":1}}`))}, nil
	})}
	found, err := f.deliverOne(f.ctx)
	if err != nil || !found {
		t.Fatal("delivery", err)
	}
	var status string
	_ = f.db.QueryRow(f.ctx, `SELECT status FROM notification_outbox`).Scan(&status)
	if status != "sent" {
		t.Fatal(status)
	}
	found, err = f.deliverOne(f.ctx)
	if err != nil || found {
		t.Fatal("sent message retried", err)
	}
}
