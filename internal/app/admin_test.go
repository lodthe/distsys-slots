package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdminRolesAndStudentProfile(t *testing.T) {
	f := setup(t)
	admin := f.student(t, 5001)
	student := f.student(t, 5002)
	_, err := f.db.Exec(f.ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'admin')`, admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []int64{student, f.assistant} {
		rec := f.request(t, "POST", "/api/v1/admin/homeworks", `{"title":"Недоступное ДЗ"}`, user, true)
		if rec.Code != 403 {
			t.Fatal("nonadmin added homework", rec.Code)
		}
	}
	rec := f.request(t, "POST", "/api/v1/admin/homeworks", `{"number":2,"defense_dates":["2027-02-15"]}`, admin, true)
	if rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = f.request(t, "PATCH", "/api/v1/me", `{"full_name":"Иван Иванов","repository_username":"ivanov_ivan"}`, student, true)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = f.request(t, "GET", "/api/v1/me", "", student, true)
	if strings.Contains(rec.Body.String(), "student_group") {
		t.Fatal("group still exposed")
	}
	_, slots := f.publish(t, f.window(f.start))
	if _, err = f.bookSlot(f.ctx, student, slots[0]); err != nil {
		t.Fatal("name-only student cannot book", err)
	}
	rec = f.request(t, "GET", fmt.Sprintf("/api/v1/assistant/windows/%d", 1), "", admin, true)
	if rec.Code != 404 {
		t.Fatal("admin can see another assistant's students")
	}
	rec = f.request(t, "GET", "/api/v1/assistant/windows", "", admin, true)
	if rec.Code != 200 {
		t.Fatal("admin cannot use assistant interface", rec.Code, rec.Body.String())
	}
	input, _ := json.Marshal(f.window(f.start.AddDate(0, 0, 1)))
	reqToken := randomToken()
	_, err = f.db.Exec(f.ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '1 hour')`, digest(reqToken), admin)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/v1/assistant/windows", strings.NewReader(string(input)))
	req.Header.Set("Origin", f.cfg.BaseURL)
	req.Header.Set("Idempotency-Key", randomToken())
	req.Header.Set("X-CSRF-Token", csrf(reqToken))
	req.AddCookie(&http.Cookie{Name: "distsys_session", Value: reqToken})
	rec = httptest.NewRecorder()
	f.Handler().ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatal("admin cannot publish own windows", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	rec = f.request(t, "GET", fmt.Sprintf("/api/v1/assistant/windows/%d", created.ID), "", admin, true)
	if rec.Code != 200 {
		t.Fatal("admin cannot read own window", rec.Code)
	}
	rec = f.request(t, "GET", "/api/v1/me/bookings", "", admin, true)
	if rec.Code != 200 {
		t.Fatal("admin cannot use student interface", rec.Code)
	}
	rec = f.request(t, "GET", "/api/v1/assistant/windows", "", student, true)
	if rec.Code != 403 {
		t.Fatal("student gained assistant interface", rec.Code)
	}
	rec = f.request(t, "GET", "/api/v1/assistant/windows/1", "", f.assistant, true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Иван Иванов") {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "student_group") || strings.Contains(rec.Body.String(), "telegram_id") {
		t.Fatal("excess participant data exposed")
	}
	rec = f.request(t, "PATCH", "/api/v1/me", `{"full_name":"Публичное имя ассистента"}`, f.assistant, true)
	if rec.Code != 400 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = f.request(t, "PATCH", "/api/v1/me", `{"full_name":"Только имя"}`, student, true)
	if rec.Code != 400 {
		t.Fatal("student omitted first and last name")
	}
}

func TestHomeworkDatesAndConcurrentChanges(t *testing.T) {
	f := setup(t)
	admin := f.student(t, 5500)
	loc, _ := time.LoadLocation("Europe/Moscow")
	day := f.start.In(loc).Format("2006-01-02")
	other := f.start.In(loc).AddDate(0, 0, 2).Format("2006-01-02")
	_, err := f.saveHomework(f.ctx, admin, f.hw, HomeworkInput{Number: 1, Dates: []string{day}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.publishWindow(f.ctx, f.assistant, randomToken(), f.window(f.start.AddDate(0, 0, 1)))
	assertConflict(t, err)
	for i := 0; i < 8; i++ {
		_, err = f.saveHomework(f.ctx, admin, f.hw, HomeworkInput{Number: 1, Dates: []string{day, other}})
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, e := f.publishWindow(f.ctx, f.assistant, randomToken(), f.window(f.start))
			if e != nil {
				assertConflict(t, e)
			}
		}()
		go func() {
			defer wg.Done()
			_, e := f.saveHomework(f.ctx, admin, f.hw, HomeworkInput{Number: 1, Dates: []string{other}})
			if e != nil {
				t.Error(e)
			}
		}()
		wg.Wait()
		var removed bool
		err = f.db.QueryRow(f.ctx, `SELECT NOT EXISTS(SELECT 1 FROM homework_defense_dates WHERE homework_id=$1 AND defense_date=$2::date)`, f.hw, day).Scan(&removed)
		if err != nil || !removed {
			t.Fatal("date removal must succeed regardless of prior publication", err)
		}
		_, err = f.publishWindow(f.ctx, f.assistant, randomToken(), f.window(f.start))
		assertConflict(t, err)
	}
}

func TestOwnerBootstrapOnlyOnce(t *testing.T) {
	f := setup(t)
	f.cfg.AdminUsername = "lodthe"
	login := func(id int64, username string) int {
		raw, _ := json.Marshal(map[string]string{"init_data": signedData(f.cfg.BotToken, TelegramUser{ID: id, Username: username}, time.Now())})
		return f.request(t, "POST", "/api/v1/auth/telegram", string(raw), 0, false).Code
	}
	if login(601, "other") != 200 || login(602, "lodthe") != 200 || login(603, "lodthe") != 200 {
		t.Fatal("login failed")
	}
	var ids []int64
	rows, err := f.db.Query(f.ctx, `SELECT telegram_id FROM users u JOIN user_roles r ON r.user_id=u.id WHERE role='admin'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 1 || ids[0] != 602 {
		t.Fatal("incorrect owner", ids)
	}
}
