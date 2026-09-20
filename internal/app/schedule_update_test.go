package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestDateReplacementPreservesPublishedSlotsAndBookings(t *testing.T) {
	f := setup(t)
	admin := f.student(t, 7401)
	if _, err := f.db.Exec(f.ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'admin')`, admin); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	day := f.start.In(loc)
	start := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, loc)
	windows, slots := f.publish(t, f.window(start))
	student := f.student(t, 7402)
	if _, err := f.bookSlot(f.ctx, student, slots[0]); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var result string
		if err := f.db.QueryRow(f.ctx, `SELECT jsonb_build_object('windows',(SELECT jsonb_agg(to_jsonb(w) ORDER BY id) FROM availability_windows w),'slots',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM slots s),'bookings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY id) FROM bookings b),'outbox',(SELECT jsonb_agg(to_jsonb(n) ORDER BY id) FROM notification_outbox n))::text`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	newDate := start.AddDate(0, 0, 4).Format("2006-01-02")
	rec := f.request(t, "PATCH", fmt.Sprintf("/api/v1/admin/homeworks/%d", f.hw), fmt.Sprintf(`{"number":1,"defense_dates":[%q]}`, newDate), admin, true)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if snapshot() != before {
		t.Fatal("editing dates changed windows, slots, bookings or notifications")
	}
	for _, path := range []string{fmt.Sprintf("/api/v1/slots/%d", slots[1]), "/api/v1/me/bookings", "/api/v1/calendar?month=" + start.Format("2006-01")} {
		r := f.request(t, "GET", path, "", student, true)
		if r.Code != 200 || r.Body.String() == "[]\n" {
			t.Fatal(path, r.Code, r.Body.String())
		}
	}
	// Remaining slots on removed dates still accept bookings.
	if _, err := f.bookSlot(f.ctx, f.student(t, 7403), slots[1]); err != nil {
		t.Fatal(err)
	}
	// A new, non-overlapping publication on the removed date is forbidden.
	_, err := f.publishWindow(f.ctx, f.assistant, randomToken(), f.window(start.Add(3*time.Hour)))
	assertConflict(t, err)
	f.publish(t, f.window(start.AddDate(0, 0, 4)))
	r := f.request(t, "GET", fmt.Sprintf("/api/v1/assistant/windows/%d", windows[0]), "", f.assistant, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestOwnWindowsIncludeAllDatesDescending(t *testing.T) {
	f := setup(t)
	ids, _ := f.publish(t, f.window(f.start), f.window(f.start.AddDate(0, 0, 1)), f.window(f.start.AddDate(0, 0, 2)), f.window(f.start.AddDate(0, 0, 3)))
	other := f.student(t, 7411)
	if _, err := f.db.Exec(f.ctx, `UPDATE availability_windows SET assistant_id=$2 WHERE id=$1`, ids[3], other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `UPDATE availability_windows SET starts_at=starts_at-interval '5 days',ends_at=ends_at-interval '5 days' WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `UPDATE slots SET starts_at=starts_at-interval '5 days',ends_at=ends_at-interval '5 days' WHERE window_id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `UPDATE availability_windows SET status='cancelled' WHERE id=$1`, ids[1]); err != nil {
		t.Fatal(err)
	}
	r := f.request(t, "GET", "/api/v1/assistant/windows", "", f.assistant, true)
	var rows []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &rows); err != nil || r.Code != 200 {
		t.Fatal(r.Code, r.Body.String(), err)
	}
	var got []int64
	for _, row := range rows {
		got = append(got, row.ID)
	}
	if !reflect.DeepEqual(got, []int64{ids[2], ids[1], ids[0]}) {
		t.Fatal("expected all own windows newest first", got)
	}
}
