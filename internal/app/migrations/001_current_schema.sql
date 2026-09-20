CREATE TABLE users (
 id bigserial PRIMARY KEY, telegram_id bigint NOT NULL UNIQUE,
 username text NOT NULL DEFAULT '', full_name text NOT NULL DEFAULT '',
 repository_username text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE user_roles (
 user_id bigint REFERENCES users(id), role text CHECK (role IN ('assistant','admin')),
 PRIMARY KEY (user_id,role)
);
CREATE TABLE homeworks (
 id bigserial PRIMARY KEY, number integer NOT NULL UNIQUE CHECK(number > 0),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE homework_defense_dates (
 homework_id bigint NOT NULL REFERENCES homeworks(id), defense_date date NOT NULL,
 PRIMARY KEY(homework_id,defense_date)
);
CREATE TABLE availability_windows (
 id bigserial PRIMARY KEY, assistant_id bigint NOT NULL REFERENCES users(id),
 homework_id bigint NOT NULL REFERENCES homeworks(id), starts_at timestamptz NOT NULL,
 ends_at timestamptz NOT NULL, slot_minutes integer NOT NULL CHECK(slot_minutes BETWEEN 1 AND 180),
 comment text NOT NULL DEFAULT '' CHECK(char_length(comment) <= 2000), status text NOT NULL DEFAULT 'published' CHECK(status IN ('published','cancelled')),
 cancellation_reason text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(), CHECK(starts_at < ends_at)
);
CREATE INDEX windows_assistant_time ON availability_windows(assistant_id,starts_at,ends_at);
CREATE TABLE slots (
 id bigserial PRIMARY KEY, window_id bigint NOT NULL REFERENCES availability_windows(id),
 starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'open' CHECK(status IN ('open','cancelled')),
 cancellation_reason text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(window_id,starts_at), CHECK(starts_at < ends_at)
);
CREATE INDEX slots_time ON slots(starts_at);
CREATE TABLE bookings (
 id bigserial PRIMARY KEY, slot_id bigint NOT NULL REFERENCES slots(id), student_id bigint NOT NULL REFERENCES users(id),
 status text NOT NULL DEFAULT 'confirmed' CHECK(status IN ('confirmed','cancelled_by_student','cancelled_by_assistant')),
 cancelled_at timestamptz, cancelled_by bigint REFERENCES users(id), cancellation_reason text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX one_booking_per_slot ON bookings(slot_id) WHERE status='confirmed';
CREATE INDEX bookings_student ON bookings(student_id,created_at);
CREATE TABLE sessions (
 token_hash text PRIMARY KEY, user_id bigint NOT NULL REFERENCES users(id),
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE idempotency_keys (
 user_id bigint NOT NULL REFERENCES users(id), key text NOT NULL, request_hash text NOT NULL,
 response jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,key)
);
CREATE TABLE notification_outbox (
 id bigserial PRIMARY KEY, event_key text NOT NULL, telegram_id bigint NOT NULL,
 reminder_booking_id bigint REFERENCES bookings(id),
 reminder_window_id bigint REFERENCES availability_windows(id),
 payload jsonb NOT NULL, status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sent','failed')),
 attempts integer NOT NULL DEFAULT 0, next_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), sent_at timestamptz,
 UNIQUE(event_key,telegram_id)
);
CREATE INDEX outbox_due ON notification_outbox(next_attempt_at) WHERE status='pending';
CREATE TABLE telegram_updates(update_id bigint PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE app_settings(key text PRIMARY KEY, value text NOT NULL);
CREATE TABLE audit_events (
 id bigserial PRIMARY KEY, actor_id bigint REFERENCES users(id), action text NOT NULL,
 entity_type text NOT NULL, entity_id bigint NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE pending_role_grants (
 id bigserial PRIMARY KEY,
 username text NOT NULL CHECK(username ~ '^[a-z0-9_]{1,32}$'),
 role text NOT NULL CHECK(role IN ('assistant','admin')),
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(username,role)
);
