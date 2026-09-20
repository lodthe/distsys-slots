package app

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReminderTimingAndDelivery(t *testing.T) {
	f := setup(t)
	_, slots := f.publish(t, f.window(f.start))
	student := f.student(t, 2001)
	if _, err := f.bookSlot(f.ctx, student, slots[0]); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		before time.Duration
		want   int
	}{{30*time.Minute + time.Second, 0}, {30 * time.Minute, 1}, {5*time.Minute + time.Second, 1}, {5 * time.Minute, 2}} {
		if err := f.enqueueReminders(f.ctx, f.start.Add(-check.before)); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM notification_outbox WHERE event_key LIKE 'reminder:%'`).Scan(&count); err != nil || count != check.want {
			t.Fatal("reminder timing", check.before, count, err)
		}
	}
	// Simulate restarts and several schedulers racing on the same due reminders.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			restarted := NewServer(f.db, f.cfg)
			if err := restarted.enqueueReminders(f.ctx, f.start.Add(-time.Minute)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var sent []struct {
		Chat int64  `json:"chat_id"`
		Text string `json:"text"`
	}
	f.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Chat int64  `json:"chat_id"`
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		sent = append(sent, payload)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})}
	for i := 0; i < 4; i++ {
		found, err := f.deliverOne(f.ctx)
		if err != nil || found != (i < 3) {
			t.Fatal("delivery", i, found, err)
		}
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	if len(sent) != 3 || sent[1].Chat != 1000 || sent[2].Chat != 2001 {
		t.Fatal("wrong recipients or duplicate reminder", sent)
	}
	for _, message := range sent[1:] {
		if !strings.Contains(message.Text, f.start.In(loc).Format("02.01.2006 15:04")+" МСК") || !strings.Contains(message.Text, "ДЗ №1") {
			t.Fatal("missing time or homework", message)
		}
	}
	if strings.Contains(sent[1].Text, "Студент") || strings.Contains(sent[1].Text, "2001") || !strings.HasPrefix(sent[1].Text, "Напоминание об окне защиты\n") || !strings.Contains(sent[2].Text, "Ассистент:") {
		t.Fatal("incorrect reminder content", sent)
	}
}

func TestObsoleteRemindersAreNotSent(t *testing.T) {
	for _, reason := range []string{"student_cancel", "slot_cancel", "window_cancel", "started"} {
		t.Run(reason, func(t *testing.T) {
			f := setup(t)
			windows, slots := f.publish(t, f.window(f.start))
			student := f.student(t, 2001)
			booking, err := f.bookSlot(f.ctx, student, slots[0])
			if err != nil {
				t.Fatal(err)
			}
			if err = f.enqueueReminders(f.ctx, f.start.Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, `UPDATE notification_outbox SET status='sent' WHERE reminder_booking_id IS NULL`); err != nil {
				t.Fatal(err)
			}
			// A failed attempt must not later retry an obsolete reminder.
			calls := 0
			f.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error_code":500}`))}, nil
			})}
			if found, err := f.deliverOne(f.ctx); err != nil || !found || calls != 1 {
				t.Fatal(found, calls, err)
			}
			switch reason {
			case "student_cancel":
				err = f.cancelStudent(f.ctx, student, booking)
			case "slot_cancel":
				err = f.cancelAssistant(f.ctx, f.assistant, slots[0], false, "")
			case "window_cancel":
				err = f.cancelAssistant(f.ctx, f.assistant, windows[0], true, "")
			case "started":
				_, err = f.db.Exec(f.ctx, `UPDATE slots SET starts_at=now()-interval '1 minute' WHERE id=$1`, slots[0])
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, `UPDATE notification_outbox SET status='sent' WHERE reminder_booking_id IS NULL; UPDATE notification_outbox SET next_attempt_at=now() WHERE reminder_booking_id IS NOT NULL`); err != nil {
				t.Fatal(err)
			}
			if found, err := f.deliverOne(f.ctx); err != nil || !found {
				t.Fatal(found, err)
			}
			if calls != 1 {
				t.Fatal("obsolete reminder sent", calls)
			}
			var count int
			if err = f.db.QueryRow(f.ctx, `SELECT count(*) FROM notification_outbox WHERE last_error='reminder_obsolete'`).Scan(&count); err != nil || count != 1 {
				t.Fatal(count, err)
			}
			// An already cancelled/started booking must not be scheduled at all.
			if _, err = f.db.Exec(f.ctx, `DELETE FROM notification_outbox WHERE reminder_booking_id IS NOT NULL`); err != nil {
				t.Fatal(err)
			}
			if err = f.enqueueReminders(f.ctx, f.start.Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}
			if err = f.db.QueryRow(f.ctx, `SELECT count(*) FROM notification_outbox WHERE reminder_booking_id IS NOT NULL`).Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestWindowRemindersIndependentOfBookings(t *testing.T) {
	f := setup(t)
	windows, slots := f.publish(t, f.window(f.start))
	// An empty window gets a reminder, even when its first slot is cancelled.
	if err := f.cancelAssistant(f.ctx, f.assistant, slots[0], false, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.enqueueReminders(f.ctx, f.start.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var windowID int64
	var body string
	if err := f.db.QueryRow(f.ctx, `SELECT reminder_window_id,payload->>'text' FROM notification_outbox`).Scan(&windowID, &body); err != nil || windowID != windows[0] {
		t.Fatal("empty window reminder", windowID, err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	if !strings.Contains(body, f.start.In(loc).Format("02.01.2006 15:04")) || strings.Contains(body, "Студент") {
		t.Fatal("incorrect window message", body)
	}
	// Several bookings and their cancellations must not create extra reminders or invalidate the window reminder.
	for i, slot := range slots[1:3] {
		student := f.student(t, int64(2100+i))
		booking, err := f.bookSlot(f.ctx, student, slot)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.cancelStudent(f.ctx, student, booking); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.enqueueReminders(f.ctx, f.start.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `UPDATE notification_outbox SET status='sent' WHERE reminder_window_id IS NULL`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})}
	if found, err := f.deliverOne(f.ctx); err != nil || !found || calls != 1 {
		t.Fatal(found, calls, err)
	}
	if found, err := f.deliverOne(f.ctx); err != nil || found {
		t.Fatal("duplicate window reminder", found, err)
	}
}

func TestObsoleteWindowRemindersAreNotRetried(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		f := setup(t)
		windows, _ := f.publish(t, f.window(f.start))
		if err := f.enqueueReminders(f.ctx, f.start.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		calls := 0
		f.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error_code":500}`))}, nil
		})}
		if found, err := f.deliverOne(f.ctx); err != nil || !found || calls != 1 {
			t.Fatal(found, calls, err)
		}
		if cancelled {
			if err := f.cancelAssistant(f.ctx, f.assistant, windows[0], true, ""); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := f.db.Exec(f.ctx, `UPDATE availability_windows SET starts_at=now()-interval '1 minute' WHERE id=$1`, windows[0]); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.db.Exec(f.ctx, `UPDATE notification_outbox SET next_attempt_at=now()`); err != nil {
			t.Fatal(err)
		}
		if found, err := f.deliverOne(f.ctx); err != nil || !found || calls != 1 {
			t.Fatal("obsolete window reminder retried", found, calls, err)
		}
		var status, reason string
		if err := f.db.QueryRow(f.ctx, `SELECT status,last_error FROM notification_outbox`).Scan(&status, &reason); err != nil || status != "failed" || reason != "reminder_obsolete" {
			t.Fatal(status, reason, err)
		}
	}
}
