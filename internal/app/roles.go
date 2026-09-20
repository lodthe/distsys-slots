package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// GrantRole assigns a role through the operator CLI.
func (s *Server) GrantRole(ctx context.Context, telegramID int64, username, role string) error {
	username = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
	if role != "assistant" && role != "admin" {
		return bad("Неизвестная роль")
	}
	if telegramID < 0 || (telegramID == 0) == (username == "") {
		return bad("Укажите Telegram ID или username")
	}
	if username != "" {
		if len(username) > 32 || strings.IndexFunc(username, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
		}) >= 0 {
			return bad("Укажите Telegram username: латинские буквы, цифры и подчёркивание, до 32 символов")
		}
	}
	return transaction(ctx, s.db, func(tx pgx.Tx) error {
		var id int64
		if telegramID > 0 {
			if err := tx.QueryRow(ctx, `INSERT INTO users(telegram_id) VALUES($1) ON CONFLICT(telegram_id) DO UPDATE SET telegram_id=excluded.telegram_id RETURNING id`, telegramID).Scan(&id); err != nil {
				return err
			}
		} else {
			if err := lockRoleUsername(ctx, tx, username); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `SELECT id FROM users WHERE lower(username)=lower($1) FOR UPDATE`, username)
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				var pendingID int64
				if err := tx.QueryRow(ctx, `INSERT INTO pending_role_grants(username,role) VALUES($1,$2)
 ON CONFLICT(username,role) DO UPDATE SET username=excluded.username RETURNING id`, username, role).Scan(&pendingID); err != nil {
					return err
				}
				_, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,entity_type,entity_id) VALUES(NULL,$1,'pending_role',$2)`, "schedule_"+role, pendingID)
				return err
			}
			if len(ids) > 1 {
				return bad("Username неоднозначен. Укажите Telegram ID")
			}
			id = ids[0]
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, role); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,entity_type,entity_id) VALUES(NULL,$1,'user',$2)`, "grant_"+role, id)
		return err
	})
}

// Serialize granting and first login so neither can miss the other transaction.
func lockRoleUsername(ctx context.Context, tx pgx.Tx, username string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('role-username:'||lower($1),0))`, username)
	return err
}

func claimPendingRoles(ctx context.Context, tx pgx.Tx, userID int64, username string) error {
	rows, err := tx.Query(ctx, `DELETE FROM pending_role_grants WHERE username=lower($1) RETURNING role`, username)
	if err != nil {
		return err
	}
	roles, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, role := range roles {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,$2) ON CONFLICT DO NOTHING`, userID, role); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,entity_type,entity_id) VALUES(NULL,$1,'user',$2)`, "grant_"+role, userID); err != nil {
			return err
		}
	}
	return nil
}
