package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
)

type TelegramUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}
type User struct {
	ID                 int64  `json:"id"`
	TelegramID         int64  `json:"telegram_id"`
	Name               string `json:"full_name"`
	RepositoryUsername string `json:"repository_username"`
	RepositoryURL      string `json:"repository_url"`
	Username           string `json:"username"`
	Assistant          bool   `json:"assistant"`
	Admin              bool   `json:"admin"`
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(v string) string { s := sha256.Sum256([]byte(v)); return hex.EncodeToString(s[:]) }
func csrf(v string) string   { return digest("csrf:" + v) }

func ValidateInitData(raw, botToken string, now time.Time) (TelegramUser, error) {
	var u TelegramUser
	data, err := url.ParseQuery(raw)
	if err != nil {
		return u, errors.New("invalid init data")
	}
	for _, values := range data {
		if len(values) != 1 {
			return u, errors.New("duplicate init field")
		}
	}
	supplied, err := hex.DecodeString(data.Get("hash"))
	if err != nil || len(supplied) != 32 {
		return u, errors.New("invalid hash")
	}
	var lines []string
	for key, values := range data {
		if key != "hash" {
			lines = append(lines, key+"="+values[0])
		}
	}
	sort.Strings(lines)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	if !hmac.Equal(supplied, mac.Sum(nil)) {
		return u, errors.New("invalid signature")
	}
	ts, err := strconv.ParseInt(data.Get("auth_date"), 10, 64)
	if err != nil {
		return u, errors.New("invalid date")
	}
	at := time.Unix(ts, 0)
	if now.Sub(at) > 5*time.Minute || at.After(now.Add(30*time.Second)) {
		return u, errors.New("expired init data")
	}
	if err = json.Unmarshal([]byte(data.Get("user")), &u); err != nil || u.ID <= 0 {
		return u, errors.New("invalid user")
	}
	return u, nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	if s.cfg.BotToken == "" {
		return &problem{503, "bot_not_configured", "Бот ещё не настроен"}
	}
	var in struct {
		InitData string `json:"init_data"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	tu, err := ValidateInitData(in.InitData, s.cfg.BotToken, time.Now())
	if err != nil {
		return &problem{401, "invalid_telegram_auth", "Откройте приложение заново из Telegram"}
	}
	token := randomToken()
	var id int64
	err = transaction(r.Context(), s.db, func(tx pgx.Tx) error {
		// A configured owner may claim the first admin role only once, using signed Telegram identity.
		bootstrap := s.cfg.AdminUsername != "" && strings.EqualFold(tu.Username, s.cfg.AdminUsername)
		if bootstrap {
			if _, err := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(781198235)`); err != nil {
				return err
			}
		}
		if tu.Username != "" {
			if err := lockRoleUsername(r.Context(), tx, tu.Username); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(r.Context(), `INSERT INTO users(telegram_id,username) VALUES($1,$2) ON CONFLICT(telegram_id) DO UPDATE SET username=excluded.username,updated_at=now() RETURNING id`, tu.ID, tu.Username).Scan(&id); err != nil {
			return err
		}
		if tu.Username != "" {
			if err := claimPendingRoles(r.Context(), tx, id, tu.Username); err != nil {
				return err
			}
		}
		if bootstrap {
			var claimed bool
			if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM app_settings WHERE key='admin_bootstrapped') OR EXISTS(SELECT 1 FROM user_roles WHERE role='admin')`).Scan(&claimed); err != nil {
				return err
			}
			if !claimed {
				if _, err := tx.Exec(r.Context(), `INSERT INTO user_roles(user_id,role) VALUES($1,'admin')`, id); err != nil {
					return err
				}
				if _, err := tx.Exec(r.Context(), `INSERT INTO app_settings(key,value) VALUES('admin_bootstrapped',$1)`, strconv.FormatInt(tu.ID, 10)); err != nil {
					return err
				}
			}
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '1 hour')`, digest(token), id)
		return err
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "distsys_session", Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 3600})
	return respond(w, 200, map[string]any{"csrf_token": csrf(token)})
}
func (s *Server) user(r *http.Request) (User, error) {
	var u User
	cookie, err := r.Cookie("distsys_session")
	if err != nil {
		return u, &problem{401, "unauthorized", "Откройте приложение из Telegram"}
	}
	err = s.db.QueryRow(r.Context(), `SELECT u.id,u.telegram_id,u.full_name,u.repository_username,u.username,EXISTS(SELECT 1 FROM user_roles WHERE user_id=u.id AND role='assistant'),EXISTS(SELECT 1 FROM user_roles WHERE user_id=u.id AND role='admin') FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>clock_timestamp()`, digest(cookie.Value)).Scan(&u.ID, &u.TelegramID, &u.Name, &u.RepositoryUsername, &u.Username, &u.Assistant, &u.Admin)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, &problem{401, "unauthorized", "Сессия истекла. Откройте приложение заново"}
	}
	if err != nil {
		return u, err
	}
	if r.Method != "GET" && !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf(cookie.Value))) {
		return u, &problem{403, "csrf", "Обновите приложение"}
	}
	u.RepositoryURL = repositoryURL(u.RepositoryUsername)
	return u, nil
}

type userKey struct{}

func current(r *http.Request) User { return r.Context().Value(userKey{}).(User) }
func (s *Server) authorized(assistant bool, next endpoint) endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		u, err := s.user(r)
		if err != nil {
			return err
		}
		if assistant && !u.Assistant && !u.Admin {
			return &problem{403, "forbidden", "Доступно только ассистентам"}
		}
		if !s.allow("user:"+strconv.FormatInt(u.ID, 10), 180) {
			return &problem{429, "rate_limited", "Слишком много запросов. Подождите минуту"}
		}
		return next(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	}
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	cookie, _ := r.Cookie("distsys_session")
	return respond(w, 200, map[string]any{"user": current(r), "csrf_token": csrf(cookie.Value)})
}
func (s *Server) profile(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		FullName           string `json:"full_name"`
		RepositoryUsername string `json:"repository_username"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	in.FullName = strings.TrimSpace(in.FullName)
	u := current(r)
	if in.FullName == "" || len([]rune(in.FullName)) > 200 {
		return bad("Укажите ФИО: от 1 до 200 символов")
	}
	if err := validateRepositoryUsername(in.RepositoryUsername); err != nil {
		return err
	}
	_, err := s.db.Exec(r.Context(), `UPDATE users SET full_name=$2,repository_username=$3,updated_at=now() WHERE id=$1`, u.ID, in.FullName, in.RepositoryUsername)
	if err != nil {
		return err
	}
	return respond(w, 200, map[string]bool{"ok": true})
}

const repositoryBaseURL = "https://distsys.ru/hse-2026/"

func validateRepositoryUsername(value string) error {
	if value == "" {
		return bad("Укажите юзернейм репозитория")
	}
	for _, r := range value {
		if unicode.IsSpace(r) || r == '\uFEFF' || r == '/' || r == '\\' {
			return bad("В юзернейме репозитория нельзя использовать пробелы и слеши")
		}
	}
	return nil
}

func repositoryURL(username string) string {
	if username == "" {
		return ""
	}
	return repositoryBaseURL + url.PathEscape(username)
}
