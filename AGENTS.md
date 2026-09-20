# Agent instructions

These instructions apply to the entire repository. Reply to the user in Russian,
using clear and concise language.

## Project

This is a Telegram Mini App for booking homework defense slots. Administrators
choose homework numbers and allowed dates, assistants publish availability,
and students book slots. The bot sends booking updates and reminders.

The backend uses Go and PostgreSQL. The frontend is plain HTML, CSS and JavaScript,
without a framework or build step. Use the Go version from `go.mod`.
See [README.md](README.md) for configuration and deployment.

## Working rules

- Start with `git status` and the relevant code. Preserve existing user changes,
  including deletions; do not restore files without a reason.
- Complete the requested change across callers, tests and documentation.
  Routine reversible edits do not need separate confirmation.
- Keep the project small. Remove unused code along with its tests, configuration
  and references. Search for callers and references with `rg` before deleting it.
- There are no external API clients. Change the backend and frontend together;
  do not add unused endpoints, legacy compatibility or speculative abstractions.
- Prefer existing code and tools over new dependencies. Keep the current structure.
- Keep README focused on simple setup and usage. Put agent guidance in this file.

## Code map

Paths below are relative to the repository root. Backend files are in `internal/app`.

| Location | Purpose |
| --- | --- |
| `cmd/distsys/main.go` | Server entry point, bot setup and role assignment CLI. |
| `internal/app/server.go`, `auth.go`, `roles.go` | HTTP routes, Telegram login, sessions and permissions. |
| `internal/app/admin.go`, `schedule.go`, `calendar.go` | Homework, availability windows, slots and calendar. |
| `internal/app/bookings.go` | Booking and cancellation, transactions and locks. |
| `internal/app/reminders.go`, `telegram.go` | Reminders and delivery from the database notification queue. |
| `internal/app/db.go`, `internal/app/migrations/` | Database access and schema creation. |
| `internal/app/web/` | Frontend assets, embedded in the Go binary by `static.go`. |
| `docs/openapi.json` | API contract; update it when changing the API. |
| `internal/app/*_test.go`, `tests/` | Go, browser and end-to-end tests. |
| `compose.yaml`, `deploy/` | Deployment, HTTPS, Docker logging and backups. |

## Behavior to preserve

- Enforce permissions on the server. Preserve Telegram signature validation,
  sessions, Origin and CSRF checks. Roles are assigned through the CLI;
  `ADMIN_TELEGRAM_USERNAME` can also bootstrap the first administrator.
- A slot can have only one confirmed booking. A student cannot have overlapping
  bookings or multiple unfinished confirmed bookings for the same homework.
- Booking, cancellation and schedule edits can happen concurrently. Preserve
  transactions, lock ordering and time checks after waiting for locks.
- Retries must not duplicate bookings, windows or notifications. Store booking
  notifications in the same database transaction as the booking change.
- Interpret defense dates and display times in `Europe/Moscow`, independently
  of the server or browser timezone. Keep this technical detail out of README
  and general UI or bot copy; use `MSK` (Cyrillic in the UI) only beside a time.
- When changing the schedule UI, check mobile layout, overlapping slots and
  lists that span more than one API page.
- Repository links currently use `https://distsys.ru/hse-2026/`. When changing
  that prefix, update the backend, frontend and related tests together.

## Database schema

Keep only the current initial schema:
`internal/app/migrations/001_current_schema.sql`. Historical migrations and
legacy upgrade tests were deliberately removed. Do not restore them unless
required by a new task.

The current setup expects a fresh database. Editing the initial SQL file does
not update an existing database: applied migrations are tracked by filename.
Use disposable databases for development. If a task requires preserving and
upgrading existing data, handle that explicitly; never reset the live database.

## Checks

Run checks appropriate to the change. For documentation-only edits, check links,
command syntax and `git diff --check`; a full test run is unnecessary.

| Command | Use |
| --- | --- |
| `make check` | Go formatting, vet, Staticcheck, tests with the race detector, build and shell syntax. |
| `TEST_DATABASE_URL='postgres://USER:PASSWORD@HOST:5432/TEST_DB?sslmode=disable' make check` | Database and backend behavior; replace the URL with a dedicated test database. |
| `make browser-check` | Frontend tests using Chromium and a mocked API, without external network access. |
| `make e2e-check` | Real backend, PostgreSQL and browser in disposable containers. |
| `make security-check` | Publication, dependency or configuration changes; secret and vulnerability scanning. |

Without `TEST_DATABASE_URL`, Go database tests are **skipped**. Do not report
that run as full backend verification. Each integration fixture creates and
drops its own schema, so the test database user needs permission to do this.
E2E creates and removes its own database and does not load the live `.env`.

Preserve coverage for permissions, concurrent bookings, cancellation and
notifications. Avoid repeated scenarios for every data combination and long
load tests without a concrete need. In browser tests, wait for the relevant
element or request rather than fixed delays or `networkidle`.

## Containers, data and secrets

- Compose stores the database and backups in named Docker volumes.
  No host UID/GID settings or manual directory permissions are needed.
  Keep the application running as its built-in non-root user.
- Logs use Docker's `journald` driver, which requires systemd journal on the
  Docker host. View current container logs with `docker compose logs`; use
  `sudo journalctl CONTAINER_NAME=distsys-slots-app-1` for historical app logs
  with the default Compose project name. Journal retention is controlled by
  the host, independently of container removal. Persistence across host reboots
  requires persistent journal storage. Do not change host journal settings
  without a deployment task, or reintroduce log wrappers and dedicated volumes.
- `docker compose down` preserves volumes; `down -v` deletes them. Never use
  volume deletion against the live deployment as part of testing.
- Existing host directories and old log volumes may contain historical data.
  Do not delete them or silently move their contents.
- The workspace may contain a live `.env`, and the host may run the application.
  Use isolated containers and databases for checks. `make up` and the root
  Compose project operate the live deployment; use them only when deployment
  is part of the task. The same applies to `configure-bot` and role assignment.
- Do not print secrets or include them in examples, Git or archives. Use
  `.env.example` and fake values for tests.
- Keep logs, dumps, local tools and test artifacts out of Git and Docker images.
  Preserve `.gitignore` and `.dockerignore` restrictions.
- `make security-check` scans working files, the Git index and available history.
  Account for differences between these when preparing publication.

Finish with a short explanation of the changes, checks that passed and anything
left unverified. Never present a skipped or unrun check as successful.
