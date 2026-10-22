import {connectedCallChannelId, joinCall} from './calls_bridge';
import {respond} from './client';
import {translate} from './i18n';
import {navigateToChannel} from './navigation';
import {currentUserId, currentUserLocale} from './redux_state';
import type {IncomingRing, IncomingRingPayload, RingAction, WebSocketMessage} from './types';
import {uiStore} from './ui_store';

const expiryTimers = new Map<string, ReturnType<typeof setTimeout>>();

function removeRing(ringId: string) {
    const timer = expiryTimers.get(ringId);
    if (timer) {
        clearTimeout(timer);
        expiryTimers.delete(ringId);
    }
    uiStore.setState((state) => ({incoming: state.incoming.filter((r) => r.ring_id !== ringId)}));
}

function finish(ringId: string, action: RingAction) {
    removeRing(ringId);
    respond(ringId, action);
}

function ringsInChannel(channelId: string): IncomingRing[] {
    return uiStore.getState().incoming.filter((r) => r.channel_id === channelId);
}

function showDesktopNotification(ring: IncomingRing) {
    if (!document.hidden || typeof Notification === 'undefined' || Notification.permission !== 'granted') {
        return;
    }
    try {
        const locale = currentUserLocale();
        const notification = new Notification(
            translate(locale, 'group_ring.incoming.notification_title', 'Incoming call'),
            {
                body: translate(locale, 'group_ring.incoming.title', '{name} is calling you', {name: ring.caller_display_name || ring.caller_username}),
                tag: `group-ring-${ring.ring_id}`,
                silent: ring.silent,
            },
        );
        notification.onclick = () => {
            window.focus();
            notification.close();
        };
    } catch {
        // Notifications are optional.
    }
}

export function handleIncomingRing(msg: WebSocketMessage<IncomingRingPayload>) {
    const data = msg.data;
    if (!data?.ring_id || uiStore.getState().incoming.some((r) => r.ring_id === data.ring_id)) {
        return;
    }

    // Tell the server a webapp session got the ring, so it does not send the fallback message.
    respond(data.ring_id, 'ack');

    if (connectedCallChannelId() === data.channel_id) {
        // Already in this call.
        respond(data.ring_id, 'accept');
        return;
    }

    const timeoutMs = Math.max(1, data.timeout_seconds || 30) * 1000;
    const ring: IncomingRing = {...data, deadline: Date.now() + timeoutMs};
    uiStore.setState((state) => ({incoming: [...state.incoming, ring]}));
    expiryTimers.set(ring.ring_id, setTimeout(() => finish(ring.ring_id, 'missed'), timeoutMs));
    showDesktopNotification(ring);
}

export function handleRingUpdated(msg: WebSocketMessage<{ring_id: string; call_id: string; call_post_id?: string}>) {
    const {ring_id: ringId, call_id: callId, call_post_id: callPostId} = msg.data || {};
    uiStore.setState((state) => ({
        incoming: state.incoming.map((r) => (r.ring_id === ringId ? {...r, call_id: callId, call_post_id: callPostId || r.call_post_id} : r)),
    }));
}

// handleRingStop stops a ring that was answered on another session or cancelled by the caller.
export function handleRingStop(msg: WebSocketMessage<{ring_id: string; reason: string}>) {
    if (msg.data?.ring_id) {
        removeRing(msg.data.ring_id);
    }
}

function eventChannelId(msg: WebSocketMessage<{channelID?: string}>): string {
    return msg.data?.channelID || msg.broadcast?.channel_id || '';
}

// handleCallStarted fills in the call ID of rings into a call that has just started.
export function handleCallStarted(msg: WebSocketMessage<{id?: string; channelID?: string}>) {
    const channelId = eventChannelId(msg);
    const callId = msg.data?.id || '';
    if (!channelId || !callId) {
        return;
    }
    uiStore.setState((state) => ({
        incoming: state.incoming.map((r) => (r.channel_id === channelId && !r.call_id ? {...r, call_id: callId} : r)),
    }));
}

// handleCallEnded stops rings into a call that has ended.
export function handleCallEnded(msg: WebSocketMessage<{channelID?: string}>) {
    for (const ring of ringsInChannel(eventChannelId(msg))) {
        finish(ring.ring_id, 'missed');
    }
}

// handleUserJoined stops rings once the current user joined the call, from any device.
export function handleUserJoined(msg: WebSocketMessage<{user_id?: string; channelID?: string}>) {
    if (!msg.data?.user_id || msg.data.user_id !== currentUserId()) {
        return;
    }
    for (const ring of ringsInChannel(eventChannelId(msg))) {
        finish(ring.ring_id, 'accept');
    }
}

export function acceptRing(ring: IncomingRing) {
    for (const other of ringsInChannel(ring.channel_id)) {
        finish(other.ring_id, 'accept');
    }
    navigateToChannel(ring.channel_id, ring.channel_name, ring.team_id);
    if (connectedCallChannelId() !== ring.channel_id) {
        joinCall(ring.channel_id, ring.team_id);
    }
}

export function declineRing(ring: IncomingRing) {
    finish(ring.ring_id, 'decline');
}

export function dismissRing(ring: IncomingRing) {
    finish(ring.ring_id, 'dismiss');
}

// resetIncoming clears all rings and timers, for plugin deactivation and tests.
export function resetIncoming() {
    expiryTimers.forEach((timer) => clearTimeout(timer));
    expiryTimers.clear();
    uiStore.setState({incoming: []});
}
