import {connectedCallChannelId, joinCall} from './calls_bridge';
import {isCallActive, reportCallId, startRing} from './client';
import type {StartRingBody} from './client';
import {navigateToChannel} from './navigation';
import type {Outcome, RingResult} from './types';
import {uiStore} from './ui_store';

// Rings started by this window, by channel, waiting for the Calls "call_start" event so the call
// ID can be reported to the server.
const pendingCallReports = new Map<string, {ringId: string; timer: ReturnType<typeof setTimeout>}>();

const RING_COMMAND = /^\/ring(\s|$)/i;

export function isRingCommand(message: string): boolean {
    return RING_COMMAND.test(message.trim());
}

// submitRing sends a ring request and, when it succeeds, starts or joins the call. A request that
// needs confirmation opens the confirmation dialog and is re-sent once confirmed.
export async function submitRing(body: StartRingBody): Promise<Outcome> {
    // The hint is only meaningful for the current channel; "~channel" rings target another one.
    const targetsOtherChannel = Boolean(body.command && body.command.includes('~'));
    const callActive = targetsOtherChannel ? false : await isCallActive(body.channel_id);

    const groupCallsAllowed = uiStore.getState().callsGroupCallsAllowed;
    const outcome = await startRing({
        ...body,
        call_active: callActive,
        group_calls_allowed: groupCallsAllowed === null ? undefined : groupCallsAllowed,
    });
    switch (outcome.kind) {
    case 'needs_confirmation':
        uiStore.setState({
            confirm: {
                count: outcome.result?.recipient_count || 0,
                onConfirm: () => {
                    submitRing({...body, confirmed: true}).catch(() => undefined);
                },
            },
        });
        break;
    case 'ringing':
    case 'deferred_to_native':
        if (outcome.result) {
            afterRingStarted(outcome.result);
        }
        break;
    }
    return outcome;
}

// afterRingStarted brings the caller to the channel and starts or joins the call there.
export function afterRingStarted(result: RingResult) {
    if (result.ring_id) {
        const previous = pendingCallReports.get(result.channel_id);
        if (previous) {
            clearTimeout(previous.timer);
        }
        const timer = setTimeout(() => pendingCallReports.delete(result.channel_id), Math.max(result.timeout_seconds, 30) * 1000);
        pendingCallReports.set(result.channel_id, {ringId: result.ring_id, timer});
    }

    navigateToChannel(result.channel_id, result.channel_name, result.team_id);
    if (connectedCallChannelId() !== result.channel_id) {
        joinCall(result.channel_id, result.team_id);
    }
}

// onCallStarted reports the Calls call ID for a ring this window started.
export function onCallStarted(channelId: string, callId: string) {
    const pending = pendingCallReports.get(channelId);
    if (!pending || !callId) {
        return;
    }
    clearTimeout(pending.timer);
    pendingCallReports.delete(channelId);
    reportCallId(pending.ringId, callId);
}

// slashCommandHook intercepts /ring so that the webapp can start the call in this window after the
// server accepted the ring. If the plugin API cannot be reached, the command is passed on to the
// server-side slash command instead.
export async function slashCommandHook(message: string, args: {channel_id: string; team_id?: string}) {
    if (!isRingCommand(message)) {
        return {message, args};
    }
    try {
        await submitRing({channel_id: args.channel_id, team_id: args.team_id, command: message.trim()});
        return {};
    } catch {
        return {message, args};
    }
}
