package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAssistantCancellationOptionalReason(t *testing.T) {
	for _, whole := range []bool{false, true} {
		for _, reason := range []string{"", "Изменилось расписание"} {
			t.Run(fmt.Sprintf("whole=%v/reason=%s", whole, reason), func(t *testing.T) {
				f := setup(t)
				windows, slots := f.publish(t, f.window(f.start))
				student := f.student(t, 99001)
				booking, err := f.bookSlot(f.ctx, student, slots[0])
				if err != nil {
					t.Fatal(err)
				}
				resource, id := "slots", slots[0]
				if whole {
					resource, id = "windows", windows[0]
				}
				body := "{}"
				if reason != "" {
					raw, _ := json.Marshal(map[string]string{"reason": reason})
					body = string(raw)
				}
				rec := f.request(t, "POST", fmt.Sprintf("/api/v1/assistant/%s/%d/cancel", resource, id), body, f.assistant, true)
				if rec.Code != 200 {
					t.Fatal(rec.Code, rec.Body.String())
				}
				var status, saved, payload string
				if err = f.db.QueryRow(f.ctx, `SELECT status,cancellation_reason FROM bookings WHERE id=$1`, booking).Scan(&status, &saved); err != nil || status != "cancelled_by_assistant" || saved != reason {
					t.Fatal(status, saved, err)
				}
				if err = f.db.QueryRow(f.ctx, `SELECT payload::text FROM notification_outbox ORDER BY id DESC LIMIT 1`).Scan(&payload); err != nil {
					t.Fatal(err)
				}
				if reason != "" && !strings.Contains(payload, reason) {
					t.Fatal("reason missing in notification", payload)
				}
				if reason == "" && strings.Contains(payload, "Причина:") {
					t.Fatal("empty reason label", payload)
				}
			})
		}
	}
}
