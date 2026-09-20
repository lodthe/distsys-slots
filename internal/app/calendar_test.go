package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCalendarCountsAndPrivacy(t *testing.T) {
	f := setup(t)
	loc, _ := time.LoadLocation("Europe/Moscow")
	// 180 slots: calendar counts must not be limited by the 100-item slot page.
	day := f.start.In(loc).Format("2006-01-02")
	at, _ := time.ParseInLocation("2006-01-02 15:04", day+" 20:00", loc)
	in := f.window(at)
	in.SlotMinutes = 1
	in.EndsAt = at.Add(3 * time.Hour)
	windows, slots := f.publish(t, in)
	student := f.student(t, 8888)
	get := func(free int) {
		t.Helper()
		rec := f.request(t, "GET", "/api/v1/calendar?month="+day[:7], "", student, true)
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
		var items []struct {
			Day   string `json:"day"`
			Free  int    `json:"free_slots"`
			Total int    `json:"total_slots"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Day != day || items[0].Free != free || items[0].Total != 180 {
			t.Fatal(rec.Body.String())
		}
		for _, private := range []string{"Student 8888", "8888", "username", "student_id", "booking_id", "first_name", "last_name"} {
			if strings.Contains(rec.Body.String(), private) {
				t.Fatal("calendar leaked participant data", private)
			}
		}
	}
	get(180)
	booking, err := f.bookSlot(f.ctx, student, slots[0])
	if err != nil {
		t.Fatal(err)
	}
	get(179)
	rec := f.request(t, "POST", fmt.Sprintf("/api/v1/bookings/%d/cancel", booking), "{}", student, true)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	get(180)
	for _, query := range []string{"month=" + day[:7] + "&homework_id=99999", "month=" + at.AddDate(0, 1, 0).Format("2006-01")} {
		rec = f.request(t, "GET", "/api/v1/calendar?"+query, "", student, true)
		if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	rec = f.request(t, "GET", "/api/v1/calendar?month=invalid", "", student, true)
	if rec.Code != 400 {
		t.Fatal("invalid month accepted", rec.Code)
	}
	rec = f.request(t, "GET", "/api/v1/calendar", "", 0, false)
	if rec.Code != 401 {
		t.Fatal("anonymous calendar access", rec.Code)
	}
	rec = f.request(t, "POST", fmt.Sprintf("/api/v1/assistant/windows/%d/cancel", windows[0]), "{}", f.assistant, true)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = f.request(t, "GET", "/api/v1/calendar?month="+day[:7], "", student, true)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatal("cancelled window still in calendar", rec.Body.String())
	}
}

func TestHomeworkNumberAndDates(t *testing.T) {
	f := setup(t)
	dates := []string{f.start.Format("2006-01-02")}
	for _, in := range []HomeworkInput{{Number: 0, Dates: dates}, {Number: 2}, {Number: 2, Dates: []string{"not-a-date"}}, {Number: 2, Dates: []string{dates[0], dates[0]}}} {
		if _, err := f.saveHomework(f.ctx, f.assistant, 0, in); err == nil {
			t.Fatal("invalid homework accepted", in)
		}
	}
	_, err := f.saveHomework(f.ctx, f.assistant, 0, HomeworkInput{Number: 1, Dates: dates})
	assertConflict(t, err)
	id, err := f.saveHomework(f.ctx, f.assistant, 0, HomeworkInput{Number: 2, Dates: dates})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.saveHomework(f.ctx, f.assistant, id, HomeworkInput{Number: 3, Dates: dates})
	if err != nil {
		t.Fatal(err)
	}
	rec := f.request(t, "GET", "/api/v1/homeworks", "", f.assistant, true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "ДЗ №3") || !strings.Contains(rec.Body.String(), "defense_dates") {
		t.Fatal(rec.Code, rec.Body.String())
	}
}
