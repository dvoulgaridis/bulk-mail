## AGENTS.md

### Architecture

Read the [architecture](docs/architecture.md) before making
architectural or cross-layer changes. Respect the module ownership described there.

Keep HTTP handlers focused on transport and orchestration. Keep delivery behavior in
`internal/mail` and its transport subpackages, persistence in `internal/store`, and
filesystem resolution in `internal/local`.

### Important invariants

Preserve these unless the task explicitly changes them:

* The server binds to loopback by default and validates local Host, Origin, and token rules.
* Application data defaults to `data/` under the current working directory on every platform.
* SMTP passwords and Gmail refresh tokens are not stored as plaintext profile fields.
* Address-list entry and campaign data remain local except when sent to the configured mail provider.
* Files under `frontend/src/` are authoritative; `frontend/dist/` is generated.

### Code quality

Go code must remain `gofmt`-formatted and pass `go vet`. TypeScript must pass the checked-in
compiler configuration. Do not weaken type checking or add broad ignores to make code pass.

Format long source lines across multiple lines instead of leaving dense declarations,
calls, composite literals, conditions, or SQL on one line. Keep handwritten Go lines at
or below 120 characters where practical, use trailing commas for multiline Go syntax,
and run `gofmt` after editing. Preserve the exact value of whitespace-sensitive strings.

Avoid unnecessary dependencies and abstractions. Do not log credentials, access tokens,
message bodies, or unnecessary address-list entry data.

### Validation

Run the relevant checks for your changes.

```sh
pnpm run build:ui
pnpm run check:ui

go vet ./...
go test ./...
```

Do not claim a check passed unless it was actually run.

### Change discipline

Fix the underlying problem rather than adding workarounds.

Never create, edit, patch, rename, or selectively delete files under
`frontend/dist/`. Treat the directory as disposable
generated output. When the frontend output must change, edit only the authoritative sources under `frontend/src/`, then run `pnpm run build:ui` to replace `frontend/dist/` with a fresh build.
