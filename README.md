# Group Ring for Mattermost

Group Ring lets you ring several people, or a whole channel, to join a call. It starts the call
in the channel, or joins the one that is already running, and every recipient gets an incoming-call
dialog with a ringtone and **Accept**, **Decline** and **Dismiss**.

It builds on the official [Calls plugin](https://github.com/mattermost/mattermost-plugin-calls)
(`com.mattermost.calls`) and adds what native ringing does not cover. It does not fork Calls or
reimplement WebRTC, and it has no license checks of its own.

* Design note, with the Calls API findings and the chosen integration:
  [docs/DESIGN.md](docs/DESIGN.md)

> [!IMPORTANT]
> **Calls outside direct messages on Team Edition.** On a server without a Professional or
> Enterprise license, Calls v1 (checked: v1.12.6) only allows calls in 1:1 direct messages.
> Group messages, public channels and private channels are refused by Calls itself
> ("unlicensed servers only allow calls in DMs"). The Calls source also enables them when the
> Mattermost server process runs with the environment variable `MM_CALLS_GROUP_CALLS_ALLOWED=true`.
> That variable is not in the product documentation, so check Mattermost's terms for your
> deployment before relying on it.
>
> Group Ring reads the Calls `GroupCallsAllowed` value. When it is false, Group Ring hides its
> channel actions outside DMs and refuses those rings, so nobody is rung into a call that cannot
> start. See finding 8 in [docs/DESIGN.md](docs/DESIGN.md#findings-about-the-calls-plugin).

## Requirements

* Mattermost Server 12.0.0 or later (developed against v12.0.1), any edition, including Team Edition.
* Calls plugin installed, enabled and running (developed against v1.12.6).
* For the fallback direct messages to link to the right place, **Site URL** must be set
  (System Console > Environment > Web Server).

## Install

1. Build the bundle (see [Build and test](#build-and-test)), or use a release `.tar.gz`:
   ```bash
   make dist
   # -> dist/ch.utaku.group-ring-0.1.0.tar.gz
   ```
2. In the System Console, go to **Plugins > Plugin Management**. Make sure **Enable Plugin Uploads**
   is true (or set `PluginSettings.EnableUploads` in `config.json`), upload the bundle and click
   **Enable**.
   Alternatively, with [mmctl](https://docs.mattermost.com/manage/mmctl-command-line-tool.html):
   ```bash
   mmctl plugin add dist/ch.utaku.group-ring-0.1.0.tar.gz
   mmctl plugin enable ch.utaku.group-ring
   ```
3. Configure the plugin under **Plugins > Group Ring**.
4. Optional: in **Plugins > Calls**, decide whether **Enable call ringing** (native DM/GM ringing)
   should be on. Group Ring defers to it for new calls in DMs and GMs (see `DeferToNativeRinging`).

## Configuration

All settings are in **System Console > Plugins > Group Ring**.

| Setting | Default | Description |
| --- | --- | --- |
| Enable Group Ring | `true` | Turns the command, buttons and menu actions on or off. |
| Ring timeout (seconds) | `30` | How long a ring lasts before it stops. Range 5 to 300. |
| Maximum recipients per ring | `50` | Larger rings are rejected. Range 1 to 1000. |
| Ask for confirmation above | `10` | Rings to more people than this must be confirmed. `0` never asks. |
| Rings per caller per minute | `3` | Sliding-window rate limit per user, shared across cluster nodes. `0` disables it. |
| Allow in public / private channels / direct messages / group messages | all `true` | Channel types where ringing is allowed. |
| Who can ring | Everyone | Optional role restriction: everyone who can post, channel admins and above, team admins and above, or system admins only. |
| Recipients in Do Not Disturb | Show without sound | `silent`: the dialog appears without ringtone. `dm_only`: only the bot DM. `skip`: not rung. |
| Defer to Calls native ringing in DMs and GMs | `true` | When Calls' **Enable call ringing** is on, a ring that starts a *new* call in a DM or GM only starts the call and lets Calls ring. Rings into a running call are always handled by Group Ring. |

Out-of-range values are clamped to safe defaults.

## Usage

### Slash command

| Command | Effect |
| --- | --- |
| `/ring @alice @bob` | Rings the listed members of the current channel. Non-members are never rung and are listed as skipped. |
| `/ring @channel` (or `@all`) | Rings every member of the current channel. |
| `/ring ~channel-name` | Rings every member of another channel in the current team and starts the call there. You must be a member. |
| `/ring ... --confirm` | Skips the confirmation for large rings (useful on mobile). |
| `/ring --cancel` | Stops your latest ring. |
| `/ring --help` | Shows usage. |

### Buttons and menus

* **Channel header button** (phone icon; it may appear in the App Bar on the right):
  opens the **Ring people** dialog. Pick channel members, or choose **Everyone in this channel**.
* **Channel menu** (click the channel name): **Ring channel** and **Ring people...**.
* **Message menu** (the "..." on a post): **Ring people...**, with the post's author preselected.

### What recipients see

* **Web and desktop**: a centered incoming-call dialog with the caller's picture and name,
  the channel, and a ringtone (no sound in Do Not Disturb with the default setting). **Accept**
  opens the channel and joins the call. **Decline** stops the ring and tells the caller.
  **Dismiss** (×, or Esc) stops the ring silently. The ring stops by itself after the timeout,
  when the call ends, when the user joins the call from another device, or when the caller cancels.
  If the browser tab is in the background and notifications are allowed, a desktop notification
  is shown as well.
* **No web or desktop session** (for example, mobile only), or **offline**: a direct message from
  the **Group Ring** bot with a **Join the call** link. It also triggers a push notification
  according to the user's notification preferences.

### What the caller sees

An ephemeral summary in the channel ("Ringing 3 people...", who got a DM instead, who was skipped
and why), and an ephemeral notice for each person who declines. The caller's web or desktop
client opens the channel and starts or joins the call automatically.

## Limitations

* **Team Edition and calls outside DMs:** see the note at the top. This is a Calls plugin
  restriction, not a Group Ring one.
* **Mobile callers:** the mobile apps do not run plugin webapps. `/ring` still works from mobile
  (recipients are rung the same way), but the caller has to start or join the call manually.
  The reply says so.
* **Mobile recipients** get the bot DM and its regular push notification, not a native VoIP
  incoming-call screen. Calls' own VoIP ringing only exists for DMs and GMs; with the default
  settings, Group Ring leaves new DM and GM calls to it.
* **Call ID:** Calls exposes no server-side, non-deprecated API to read call IDs. The call ID in
  the ring payload is filled in once the caller's webapp sees Calls' `call_start` event. Until
  then, it is empty and recipients join by channel, which is equivalent.
* **Already in another call:** accepting while connected to a different call shows Calls' own
  error. Leave the current call first.
* **Desktop app:** the "already in this call" check reads `window.callsClient`, which only exists
  in the window that runs the call. In the Desktop app, calls run in a separate widget window, so
  this check is best effort there.
* **Browser autoplay:** browsers may block the ringtone until the user has interacted with the
  page. The dialog is still shown.
* **Plugin restarted during a call:** call tracking is rebuilt from the next Calls post. The
  webapp's active-call hint covers most cases.
* **Calls test mode** (`DefaultEnabled=false`): only system admins can start calls, so other
  users can only ring into a call that is already running.

## Build and test

Toolchains: Go as required by `go.mod` (1.26.7; with `GOTOOLCHAIN=auto`, the default, `go`
downloads it automatically), Node.js 22 or later (`.nvmrc` pins 24.13.1), and GNU make. None of
these steps needs root or Docker.

```bash
# Server: unit tests, vet and lint (golangci-lint v2.9.0, installed by make)
go test ./server/...
go test -race ./server/...
go vet ./...
make check-style            # golangci-lint + webapp eslint

# Webapp
cd webapp
npm ci
npm run check-types
npm run lint
npm test
cd ..

# Bundle for all platforms -> dist/ch.utaku.group-ring-<version>.tar.gz
make dist

# Regenerate the Calls client mock after changing calls.Client
make mock
```

`make test` runs the full suite as well, but it first builds a linux/amd64 bundle with developer
settings, following the starter template.

### What the unit tests cover

* `server/ring`: permissions (membership, post permission, role restriction, channel types,
  archived channels), recipient resolution and filtering (caller, bots, deactivated users,
  non-members, unknown users, duplicates, channel paging), limits (max recipients, confirmation
  threshold, sliding-window rate limit), user state (online, DND in each mode, offline), fallback
  DMs and acknowledgement, responses (accept, decline, dismiss, idempotency), cancel, call ID
  updates, and the Calls integration (unavailable, test mode, group calls not allowed, native
  deferral in GMs, call post links). Calls is replaced by a gomock mock
  (`server/calls/mocks`), and the KV store by an in-memory fake with compare-and-set semantics.
* `server/command`: parser and replies; `server/calls`: availability, settings and call
  tracking; `server/i18n`: catalog and fallback; configuration clamping.
* `webapp`: `/ring` interception and fallback, confirmation flow, Calls join message,
  and the incoming-ring lifecycle (ack, timeout, stop, decline, accept, joined elsewhere,
  call ended).

## Manual test plan

### Setup

* A Mattermost 12 server with Calls running and Group Ring installed, Site URL set.
  For channel scenarios, Calls must allow calls outside DMs (see the note at the top).
* Users: **caller**, **alice**, **bob**, **carol**, **dave**, **erin**, all in team `acme` and
  channel `~ring-test`. Log each of caller, alice, bob and carol in to a separate browser profile
  (or Desktop app). Log dave in too. erin stays logged out of every client.
* Keep the server log open (`mmctl logs` or the System Console server logs) to check the audit
  lines: `Ring started` at INFO with `caller_id`, `channel_id` and `recipient_count`, and no
  message content.

### Scenarios

| # | Scenario | Steps | Expected result |
| --- | --- | --- | --- |
| 1 | 1 caller + 3 recipients | In `~ring-test`, caller runs `/ring @alice @bob @carol`. | Caller's client opens the call in `~ring-test`. alice, bob and carol see the dialog with ringtone within about 1 s. Caller gets "Ringing 3 people in ~ring-test: @alice, @bob, @carol." alice clicks **Accept**: her client opens the channel and joins the call, and her other tabs stop ringing. Log shows `Ring started ... recipient_count=3`. |
| 2 | Full channel | Set **Ask for confirmation above** to 3. Caller clicks the header button, chooses **Everyone in this channel** and clicks **Ring channel**. | A "Ring 5 people?" confirmation appears. Nobody is rung yet. After **Ring 5 people**, every member except the caller and bots is rung. From mobile, `/ring @channel` replies with the `--confirm` hint instead. |
| 3 | DND user | bob sets **Do Not Disturb**. Caller runs `/ring @bob @alice`. Repeat with the DND setting on `dm_only`, then `skip`. | `silent`: bob's dialog appears without sound and says so, and alice hears the ringtone. `dm_only`: bob gets only the bot DM. `skip`: caller's reply lists "@bob (Do Not Disturb)" under "Not rung". |
| 4 | Offline user | erin is logged out everywhere (status offline). Caller runs `/ring @erin @alice`. | No dialog for erin. Immediately after, erin has a bot DM "**caller** is calling you in **ring-test**. Join the call", which links to the call post once the call started, or to the channel. If erin has the mobile app, she gets a push notification. |
| 5 | No webapp session (status not offline) | dave closes all browser tabs but stays online on mobile only (or set status to Online manually and close the tabs). Caller runs `/ring @dave`. | About 6 s later, dave gets the bot DM, because no webapp acknowledged the ring. A user with an open webapp never gets this DM. |
| 6 | Decline | Caller runs `/ring @carol`. carol clicks **Decline**. | carol's dialog closes on all her sessions. Caller sees the ephemeral "@carol declined your call." No DM is sent to carol. |
| 7 | Timeout | Set **Ring timeout** to 10. Caller runs `/ring @alice`, and alice does nothing. | The ringtone stops and the dialog closes after 10 s. No decline notice for the caller. |
| 8 | Dismiss | Caller rings alice. alice presses Esc or clicks ×. | The dialog closes silently, and the caller is not notified. |
| 9 | Cancel | Caller runs `/ring @channel --confirm`, then `/ring --cancel`. | Every pending dialog closes. Reply: "Stopped ringing N people." |
| 10 | Join elsewhere and call end | Caller rings alice and bob. alice joins the call from the channel's Calls button instead of the dialog. Caller then ends the call while bob is still ringing. | alice's dialog closes when she joins. bob's dialog closes when the call ends. |
| 11 | Rate limit | Caller runs `/ring @alice` four times within a minute. | The fourth reply is "You are ringing too often. Try again in N seconds." |
| 12 | Non-members and bots | Caller runs `/ring @alice @someone-outside-the-channel @a-bot`. | Only alice is rung. The reply lists the others as "not a member of this channel" and "bot". |
| 13 | Permissions | Set **Who can ring** to **Channel admins and above**. A regular member runs `/ring @alice`. In a read-only channel, a member without post permission tries to ring. | Both are refused with a clear message. Nobody is rung. |
| 14 | Already in the call | alice is in the call in `~ring-test`. Caller runs `/ring @alice @bob`. | alice's client does not show a dialog. bob is rung. |
| 15 | Native ringing in GMs | Turn on Calls **Enable call ringing**. In a GM with alice and bob (where Calls allows calls), caller runs `/ring @channel` with no call running. | The reply says Calls rings everyone. Only Calls' native ringing is heard, with no Group Ring dialog. Running `/ring @bob` again while the call is running shows the Group Ring dialog to bob. |
| 16 | Calls refuses calls outside DMs | On a Team Edition server without `GroupCallsAllowed`, open a public channel. | The channel menu and post menu show no Group Ring actions. The header dialog shows the explanation and **Ring** is disabled. `/ring @alice` replies that Calls only allows calls in direct messages. In a DM, ringing works. |
| 17 | Disabled plugin setting | Set **Enable Group Ring** to false. | The menu actions disappear, and `/ring` replies that Group Ring is disabled. |
