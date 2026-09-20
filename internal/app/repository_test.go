package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRepositoryUsernameValidationAndURL(t *testing.T) {
	for _, value := range []string{"ivanov_ivan_i", "имя.фамилия-🙂", "a?b#c%2F@:+&=!'\"()[]{}", ".", ".."} {
		if err := validateRepositoryUsername(value); err != nil {
			t.Fatal(value, err)
		}
		link := repositoryURL(value)
		if !strings.HasPrefix(link, repositoryBaseURL) {
			t.Fatal(link)
		}
		decoded, err := url.PathUnescape(strings.TrimPrefix(link, repositoryBaseURL))
		if err != nil || decoded != value {
			t.Fatal(link, decoded, err)
		}
		parsed, err := url.Parse(link)
		if err != nil || parsed.Host != "distsys.ru" || parsed.RawQuery != "" || parsed.Fragment != "" {
			t.Fatal(link, parsed, err)
		}
	}
	for _, value := range []string{"", "a b", " a", "a ", "a/b", `a\b`, "a\tb", "a\nb", "a\u00a0b", "a\u0085b", "a\ufeffb"} {
		if validateRepositoryUsername(value) == nil {
			t.Fatal("accepted invalid repository", value)
		}
	}
}

func TestRepositoryProfilePrivacyAndBooking(t *testing.T) {
	f := setup(t)
	student := f.student(t, 9401)
	if _, err := f.db.Exec(f.ctx, `UPDATE users SET repository_username='' WHERE id=$1`, student); err != nil {
		t.Fatal(err)
	}
	windows, slots := f.publish(t, f.window(f.start))
	if _, err := f.bookSlot(f.ctx, student, slots[0]); err == nil {
		t.Fatal("booking without repository allowed")
	}
	for _, raw := range []string{`{"full_name":"Иван"}`, `{"full_name":" ","repository_username":"ivan"}`, `{"full_name":"Иван","repository_username":"bad/name"}`, `{"full_name":"Иван","repository_username":"name surname"}`} {
		r := f.request(t, "PATCH", "/api/v1/me", raw, student, true)
		if r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	name, repo := "Иванов Иван Отчество", "private_repo_9401?#%+🙂"
	raw, _ := json.Marshal(map[string]string{"full_name": " " + name + " ", "repository_username": repo})
	r := f.request(t, "PATCH", "/api/v1/me", string(raw), student, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = f.request(t, "GET", "/api/v1/me", "", student, true)
	var me struct {
		User User `json:"user"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &me); err != nil || me.User.Name != name || me.User.RepositoryUsername != repo || me.User.RepositoryURL != repositoryURL(repo) {
		t.Fatal(me, err)
	}
	if _, err := f.bookSlot(f.ctx, student, slots[0]); err != nil {
		t.Fatal(err)
	}
	r = f.request(t, "GET", fmt.Sprintf("/api/v1/assistant/windows/%d", windows[0]), "", f.assistant, true)
	var details struct {
		Slots []struct {
			Name       string `json:"full_name"`
			Repository string `json:"repository_url"`
		}
	}
	if err := json.Unmarshal(r.Body.Bytes(), &details); err != nil || len(details.Slots) == 0 || details.Slots[0].Name != name || details.Slots[0].Repository != repositoryURL(repo) {
		t.Fatal(r.Code, r.Body.String(), err)
	}
	other := f.student(t, 9402)
	loc, _ := time.LoadLocation("Europe/Moscow")
	for _, path := range []string{"/api/v1/slots", fmt.Sprintf("/api/v1/slots/%d", slots[0]), "/api/v1/calendar?month=" + f.start.In(loc).Format("2006-01"), "/api/v1/homeworks"} {
		r = f.request(t, "GET", path, "", other, true)
		if r.Code != 200 || strings.Contains(r.Body.String(), "private_repo_9401") || strings.Contains(r.Body.String(), name) || strings.Contains(r.Body.String(), "repository_") {
			t.Fatal("private profile leaked", path, r.Code, r.Body.String())
		}
	}
	if _, err := f.db.Exec(f.ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'admin')`, other); err != nil {
		t.Fatal(err)
	}
	r = f.request(t, "GET", fmt.Sprintf("/api/v1/assistant/windows/%d", windows[0]), "", other, true)
	if r.Code != 404 {
		t.Fatal("foreign admin can read repository", r.Code)
	}
}
