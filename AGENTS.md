# AGENTS.md

Group Ring: a Mattermost plugin (Go server in `server/`, React/TypeScript webapp in `webapp/`) that
rings people to join a call provided by the Calls plugin (`com.mattermost.calls`). Read
`docs/DESIGN.md` before changing how the plugin talks to Calls.

## Commands

```bash
go test ./server/...                     # server unit tests
go test ./server/ring -run TestRespond   # single test
go vet ./... && golangci-lint run ./...  # server lint (golangci-lint v2.9.0, built with Go >= 1.26)
cd webapp && npm run lint && npm run check-types && npm test
make dist                                # bundle in dist/
make mock                                # regenerate server/calls/mocks after changing calls.Client
```

## Architecture

- `plugin.json` is the source of truth for ID, version and settings. `make apply` generates
  `server/manifest.go` and `webapp/src/manifest.ts`; both are gitignored, so never edit them.
- `server/ring`: the ring service and its dependency interfaces (`API`, `Store`, `calls.Client`).
  Keep plugin API access behind these interfaces so the service stays unit-testable.
- `server/command`: `/ring` parsing and replies, shared by the server-side command and the REST
  endpoint used by the webapp's slash command hook.
- `server/calls`: everything that depends on Calls behavior. Calls has no usable server-side API
  for other plugins, so call starts and joins happen in the webapp (`webapp/src/calls_bridge.ts`).
- `server/kvstore`: transient state only; every key must have an expiry.
- User-facing strings live in `server/i18n/translations/en.json` and `webapp/i18n/en.json`.
  Everything in the code is in English.

## Tests

- Ring service tests use an in-memory plugin API fake (`server/ring/fakes_test.go`), the real
  KV store over an in-memory KV with compare-and-set, and the gomock Calls mock.
- There are no Docker-based integration tests; the manual test plan is in the README.
