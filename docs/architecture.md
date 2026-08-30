## Architecture

- `cmd/bulk-mail`: process startup, flags, browser launch, signals and shutdown
- `internal/config`: configuration defaults, loading, environment overrides and validation
- `internal/local`: working-directory, data, executable and temporary-path resolution
- `internal/database` and `internal/store`: SQLite schema, persisted models and queries
- `internal/server`: loopback HTTP transport, request protection, API handlers and SSE
- `internal/app`: campaign preparation and execution, personalization and attachment
  orchestration, sender construction and message-delivery workflows
- `internal/tasks`: task models, durable input storage, bounded FIFO queueing, execution,
  cancellation and shutdown
- `internal/document`: DOCX validation, personalization, LibreOffice conversion and archives
- `internal/templates`: text, HTML and placeholder rendering
- `internal/mail`: provider-neutral messages, MIME formatting, sender contracts and delivery errors
- `internal/mail/smtp`: SMTP discovery, secure connection probing, authentication and delivery
- `internal/mail/gmail`: Gmail API delivery and Google OAuth
- `internal/credentials`: encrypted local secrets
- `frontend/src`: authoritative Vue and TypeScript source, including immutable form presets
- `frontend/dist`: generated, ignored Vite output embedded through `frontend/embed.go`

### Campaign task lifecycle

Campaigns are saved definitions containing an address list, sender profile, message,
attachments and personalization options. A new campaign uses ID `-1` until the backend
saves it and assigns a positive ID. Execution status belongs to tasks, not campaigns.

Launching a campaign creates an immutable task snapshot of its address-list entries, settings,
message content and non-secret sender configuration. Source attachments are staged under
`data/task-queue/`, while SQLite remains the authoritative FIFO queue. Workers load and
prepare a task only after claiming one of the configured concurrent execution slots.
`tasks.max_queued` bounds waiting tasks separately from
`tasks.max_concurrent` preparation/execution slots.

The `internal/tasks` package owns the queue, runner, staged-input lifecycle and task
types. `internal/app/campaign_tasks.go` only translates a campaign into and out of the
generic task payload before invoking campaign preparation or delivery. SQLite queries
remain in `internal/store` with the rest of the persistence layer.

Queued tasks survive application restarts. Tasks that had reached `preparing` or
`running` are marked interrupted instead of being resumed, because delivery may already
have produced external side effects. Task input files are removed after every terminal
outcome.

### Credential storage

SMTP passwords and Gmail refresh tokens are sealed with AES-256-GCM and stored in
SQLite. The random master key remains in `bulk-mail.key` under the data directory;
decrypted values stay server-side and are used only while verifying a profile or
authenticating delivery.

This separation protects credentials when SQLite is disclosed without the key. It does
not protect against an attacker who can read the entire data directory or compromise the
operating-system account running Bulk Mail.

### Sender profiles

Saved profiles contain concrete connection and identity values. Their `profileType` selects
Custom SMTP, Gmail App Password, or Gmail OAuth behavior; it does not identify a mail
provider. SMTP presets are frontend-only form defaults, and SMTP detection only suggests a
reachable TLS-protected endpoint. Neither preset provenance nor detection results are
persisted separately from the saved concrete profile values.
