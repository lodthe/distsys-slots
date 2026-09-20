package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWindowCommentAccessAndValidation(t *testing.T) {
	f := setup(t)
	input := f.window(f.start)
	input.Comment = "  https://meet.example/defense\nКомната ожидания включена  "
	windows, slots := f.publish(t, input)
	path := fmt.Sprintf("/api/v1/assistant/windows/%d/comment", windows[0])
	student := f.student(t, 2001)
	other := f.student(t, 2002)
	if err := f.GrantRole(f.ctx, 2002, "", "assistant"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		user int64
		csrf bool
		body string
		want int
	}{
		{0, false, `{"comment":"bad"}`, 401},
		{student, true, `{"comment":"bad"}`, 403},
		{other, true, `{"comment":"bad"}`, 404},
		{f.assistant, false, `{"comment":"bad"}`, 403},
		{f.assistant, true, `{}`, 400},
		{f.assistant, true, `{"comment":null}`, 400},
		{f.assistant, true, `{"comment":"` + strings.Repeat("я", 2001) + `"}`, 400},
	} {
		if rec := f.request(t, "PATCH", path, c.body, c.user, c.csrf); rec.Code != c.want {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	var comment string
	if err := f.db.QueryRow(f.ctx, `SELECT comment FROM availability_windows WHERE id=$1`, windows[0]).Scan(&comment); err != nil || comment != strings.TrimSpace(input.Comment) {
		t.Fatal(comment, err)
	}
	// The meeting link is not exposed to unrelated students browsing slots.
	if rec := f.request(t, "GET", fmt.Sprintf("/api/v1/slots/%d", slots[0]), "", student, true); rec.Code != 200 || strings.Contains(rec.Body.String(), "meet.example") || strings.Contains(rec.Body.String(), `"comment"`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	for _, value := range []string{strings.Repeat("я", 2000), ""} {
		raw, _ := json.Marshal(map[string]string{"comment": value})
		if rec := f.request(t, "PATCH", path, string(raw), f.assistant, true); rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if err := f.db.QueryRow(f.ctx, `SELECT comment FROM availability_windows WHERE id=$1`, windows[0]).Scan(&comment); err != nil || comment != value {
			t.Fatal("comment not saved", err)
		}
	}
	input.Comment = strings.Repeat("я", 2001)
	if _, err := f.publishWindow(f.ctx, f.assistant, randomToken(), input); err == nil {
		t.Fatal("oversized creation comment accepted")
	}
}

func TestStudentReminderUsesLatestComment(t *testing.T) {
	for _, cleared := range []bool{false, true} {
		f := setup(t)
		input := f.window(f.start)
		input.Comment = "https://meet.example/old"
		windows, slots := f.publish(t, input)
		if _, err := f.bookSlot(f.ctx, f.student(t, 2001), slots[0]); err != nil {
			t.Fatal(err)
		}
		if err := f.enqueueReminders(f.ctx, f.start.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		var leaks int
		if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM notification_outbox WHERE reminder_booking_id IS NULL AND payload::text LIKE '%meet.example%'`).Scan(&leaks); err != nil || leaks != 0 {
			t.Fatal("comment in other notifications", leaks, err)
		}
		if _, err := f.db.Exec(f.ctx, `UPDATE notification_outbox SET status='sent' WHERE reminder_booking_id IS NULL`); err != nil {
			t.Fatal(err)
		}
		path := fmt.Sprintf("/api/v1/assistant/windows/%d/comment", windows[0])
		save := func(value string) {
			t.Helper()
			raw, _ := json.Marshal(map[string]string{"comment": value})
			if rec := f.request(t, "PATCH", path, string(raw), f.assistant, true); rec.Code != 200 {
				t.Fatal(rec.Code, rec.Body.String())
			}
		}
		save("https://meet.example/current")
		var messages []string
		f.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			var payload struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			messages = append(messages, payload.Text)
			if len(messages) == 1 {
				return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error_code":500}`))}, nil
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
		})}
		if found, err := f.deliverOne(f.ctx); err != nil || !found {
			t.Fatal(found, err)
		}
		if len(messages) != 1 || !strings.Contains(messages[0], "https://meet.example/current") || strings.Contains(messages[0], "https://meet.example/old") {
			t.Fatal(messages)
		}
		value := "https://meet.example/new"
		if cleared {
			value = ""
		}
		save(value)
		if _, err := f.db.Exec(f.ctx, `UPDATE notification_outbox SET next_attempt_at=now() WHERE status='pending'`); err != nil {
			t.Fatal(err)
		}
		if found, err := f.deliverOne(f.ctx); err != nil || !found {
			t.Fatal(found, err)
		}
		if len(messages) != 2 || strings.Contains(messages[1], "https://meet.example/current") {
			t.Fatal(messages)
		}
		if cleared {
			if strings.Contains(messages[1], "Комментарий ассистента:") || strings.Contains(messages[1], "meet.example") {
				t.Fatal("deleted comment sent", messages[1])
			}
		} else if !strings.Contains(messages[1], value) || strings.Count(messages[1], "Комментарий ассистента:") != 1 {
			t.Fatal(messages[1])
		}
	}
}
