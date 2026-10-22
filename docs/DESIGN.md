# Group Ring: design note

## Versions checked (22 October 2026)

| Component | Version | Source checked |
| --- | --- | --- |
| Mattermost server (latest stable tag) | v12.0.1 | `mattermost/mattermost` at tag `v12.0.1` (`server/channels/app/plugin_api.go`, `plugin_requests.go`, `webapp/channels/src/plugins/registry.ts`, `actions/hooks.ts`, `actions/websocket_actions.ts`) |
| Calls plugin (latest stable tag) | v1.12.6, `min_server_version` 12.0.0 | `mattermost/mattermost-plugin-calls` at tag `v1.12.6` (`server/api_router.go`, `api.go`, `websocket.go`, `push_notifications.go`, `plugin.go`, `webapp/src/index.tsx`, `slash_commands.tsx`) |
| Plugin API module | `github.com/mattermost/mattermost/server/public` v0.5.0 (from the starter template) | |

## Findings about the Calls plugin

1. **Native ringing covers only part of the problem.** The Calls setting `EnableRinging`
   ("Enable call ringing") defaults to `false` and needs a Calls plugin restart when changed. When
   it is on, Calls rings members of **direct and group messages only**, and only **when a call
   starts** (`push_notifications.go`). This covers the desktop alert, the ringtone and a VoIP push
   on mobile. Calls has no ringing for public or private channels, cannot ring a subset of members,
   and cannot ring someone into a call that is already running. Its decline endpoint
   (`POST /calls/{channel_id}/decline`) works for DMs only.
2. **There is no server API to start a call.** A call starts when a client sends the Calls
   `join` WebSocket message and sets up WebRTC (`websocket.go`, `handleJoin`/`addUserSession`).
   Starting a call from the server would mean reimplementing the client, which is out of scope.
3. **The Calls HTTP API cannot be used from another plugin's server side.** Every Calls route
   except `/version`, `/metrics` and `/standalone/` requires the `Mattermost-User-Id` header. In
   Mattermost v12, `PluginHTTP` (inter-plugin requests) always sets that header to an empty string
   (`ServeInterPluginRequest` → `ServeInternalPluginRequest("", ...)`), so Calls answers 401.
   From a user session (the webapp), the non-deprecated `GET /plugins/com.mattermost.calls/calls/{channel_id}/active`
   returns whether a call is running; the call ID is only exposed by deprecated routes.
4. **The Calls webapp accepts commands from other code in the same window.** It listens for
   same-origin `window.postMessage({type: 'calls-run-slash-command', message, args})` and runs the
   message through its own `/call` handler (`index.tsx`, `windowEventHandler`). `/call join` joins
   the call in `args.channel_id`, or starts one when none is running. This is the integration
   point used to start and join calls.
5. **Calls broadcasts WebSocket events to channel members:** `custom_com.mattermost.calls_call_start`
   (with `id` = call ID, `channelID`, `post_id`), `..._call_end` and `..._user_joined` (`user_id`).
   The Mattermost webapp dispatches every plugin WebSocket event to every plugin that registered a
   handler for that event name, so another plugin can listen to them.
6. **Calls posts a `custom_calls` post when a call starts** (props `start_at`, `title`) and updates
   it with `end_at` when the call ends. Other plugins see these through the standard
   `MessageHasBeenPosted` and `MessageHasBeenUpdated` hooks, which gives the server a reliable,
   API-only signal of active calls and a permalink to each call.
7. **Test mode:** with `DefaultEnabled=false`, only system admins can start calls.
8. **On an unlicensed (Team Edition) server, Calls v1 only allows calls in 1:1 direct messages.**
   `userCanStartOrJoin` (`server/session.go`) refuses every other channel type with
   "unlicensed servers only allow calls in DMs" unless `GroupCallsAllowed()` is true
   (`server/enterprise/license.go`). That function returns true with a Professional or higher
   license, or when the Mattermost server process has the environment variable
   `MM_CALLS_GROUP_CALLS_ALLOWED=true`. That variable appears only in the Calls source, not in the
   product documentation. Calls exposes the result to webapps as `GroupCallsAllowed` in
   `GET /plugins/com.mattermost.calls/config`.

   **Consequence:** on a stock Team Edition server, calls (and therefore rings) are only possible
   in DMs, where native ringing already exists. Group Ring itself has no license checks and works
   unchanged in every channel type as soon as Calls allows calls there. Group Ring reads Calls'
   own `GroupCallsAllowed` value in the webapp. When it is false, Group Ring hides the entry
   points outside DMs and the server refuses such rings, so nobody is rung into a call that cannot
   start.

## What this plugin adds, and what it leaves to Calls

| Need | Native Calls (v1.12.6) | Group Ring |
| --- | --- | --- |
| Ring everyone when a call starts in a DM or GM | Yes, when `EnableRinging` is on | Defers to Calls by default (`DeferToNativeRinging`), to avoid double ringing |
| Ring members of a public or private channel | No | Yes (`/ring @channel`, `/ring ~channel`, header button), wherever Calls allows the call (finding 8) |
| Ring selected people | No | Yes (`/ring @a @b`, people picker modal, post menu) |
| Ring someone into a call that is already running | No | Yes, in every channel type |
| Fallback for people without a web or desktop session | VoIP push in DMs and GMs | Bot DM with a call link, which also triggers a regular push notification |
| Limits, rate limit, role restriction, audit log | No | Yes |
| Starting, joining and media (WebRTC) | Yes | Not reimplemented: uses Calls through `/call join` |

## Integration approach

```
Caller webapp                       Group Ring server                      Recipient webapp(s)
-------------                       -----------------                      -------------------
/ring @a @b  (intercepted by
SlashCommandWillBePosted hook)
  ├─ GET calls/{ch}/active (Calls)
  └─ POST /api/v1/rings ───────────▶ parse, permission checks, recipients,
                                     limits, rate limit, KV record (TTL)
                                     PublishWebSocketEvent(UserId) ───────▶ incoming_ring
                                     (offline / DND→DM: bot DM now)          ├─ POST respond {ack}
     ◀── outcome (ringing) ──────────                                       └─ modal + ringtone
  navigate + postMessage
  "calls-run-slash-command /call join"
  (Calls starts or joins the call)
                                     MessageHasBeenPosted(custom_calls)
                                     → track active call + post permalink
  Calls call_start event ──▶ POST /rings/{id}/call {call_id}
                                     PublishWebSocketEvent ───────────────▶ ring_updated {call_id}
                                     after 6 s: bot DM to recipients
                                     that never acknowledged
                                                                           Accept: navigate +
                                                                           postMessage /call join
                                     ◀────────────────────────── respond {accept|decline|dismiss|missed}
                                     ring_stop to the recipient's
                                     other sessions; ephemeral
                                     "declined" notice to the caller
```

* **Server** (Go): `command` (parser and replies), `ring` (service: permissions, recipient
  resolution and filtering, limits, delivery, responses), `calls` (Calls client: availability via
  `GetPluginStatus`, settings from the server config, active-call tracking from posts; mocked with
  gomock in tests), `kvstore` (transient state with expiry and compare-and-set), `i18n` (English
  catalog, fallback to English).
* **Webapp** (React + TypeScript): channel header button, channel header menu ("Ring channel",
  "Ring people..."), post menu ("Ring people..."), a root component with the people picker, the
  confirmation dialog and the incoming-call modal, a `/ring` slash command hook, and WebSocket
  handlers for this plugin's events and the Calls events listed above.
* **Mobile apps** do not load plugin webapps. `/ring` from mobile runs the server-side command:
  recipients are rung the same way, and the caller is told to start or join the call. Mobile
  recipients get the bot DM, whose push notification opens the call post.

## Team Edition compatibility

Only APIs available on Team Edition are used, and there are no license checks: `RegisterCommand`,
`PublishWebSocketEvent`, `SendEphemeralPost`, `CreatePost`, `GetDirectChannel`, `EnsureBotUser`,
`GetChannel*`, `GetUser*`, `GetUserStatusesByIds`, `HasPermissionTo*`, `GetPluginStatus`,
`GetConfig`, `KVSetWithOptions` (atomic and expiring), `KVGet`, `KVDelete`, the
`MessageHasBeenPosted`/`MessageHasBeenUpdated` hooks, and the documented webapp plugin registry.
Calls recording, transcription and other licensed Calls features are not used.

The limiting factor on Team Edition is the Calls plugin, not this plugin: see finding 8. Calls
DMs work on every edition. Calls in group messages and channels need Calls' `GroupCallsAllowed`
to be true.

## Key decisions

* **The webapp starts the call; the server never does.** This keeps WebRTC entirely in Calls.
* **Ring first, then start the call.** Recipients are rung immediately; whoever joins first
  starts the call, and Calls serializes concurrent joins with its per-channel lock. The call ID is
  informational: it is attached once the caller's webapp sees `call_start`.
* **"No active webapp session" is detected by acknowledgement, not by status.** Each recipient
  webapp acknowledges the ring; recipients that did not acknowledge within 6 seconds get the bot
  DM. Offline users (and DND users with the `dm_only` setting) get the DM right away.
* **All state is transient.** Ring records expire 2 minutes after the ring timeout, rate-limit
  windows after 61 seconds, and tracked calls after 24 hours if their end is never observed.
  Updates use compare-and-set, so this works across cluster nodes.
