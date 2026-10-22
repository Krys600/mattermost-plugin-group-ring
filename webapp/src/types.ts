// Shapes shared with the server API (see server/api_rings.go and server/ring/types.go).

export type ChannelTypeCode = 'O' | 'P' | 'D' | 'G';

export type ClientConfig = {
    enabled: boolean;
    ring_timeout_seconds: number;
    max_recipients: number;
    confirm_threshold: number;
    channel_types: Record<ChannelTypeCode, boolean>;
    allowed_callers: string;
    calls_available: boolean;
};

export type SkippedUser = {
    user_id?: string;
    username: string;
    reason: string;
};

export type RingResult = {
    ring_id?: string;
    channel_id: string;
    channel_name: string;
    team_id?: string;
    call_active: boolean;
    recipient_count: number;
    rung: string[];
    silent: string[];
    dm_only: string[];
    skipped: SkippedUser[] | null;
    needs_confirmation: boolean;
    deferred_to_native: boolean;
    timeout_seconds: number;
};

export type OutcomeKind = 'help' | 'ringing' | 'deferred_to_native' | 'needs_confirmation' | 'cancelled' | 'error';

export type Outcome = {
    kind: OutcomeKind;
    text: string;
    result?: RingResult;
    error_code?: string;
};

export type RingAction = 'ack' | 'accept' | 'decline' | 'dismiss' | 'missed';

// Payload of the "incoming_ring" WebSocket event.
export type IncomingRingPayload = {
    ring_id: string;
    caller_id: string;
    caller_username: string;
    caller_display_name: string;
    channel_id: string;
    channel_name: string;
    channel_display_name: string;
    channel_type: ChannelTypeCode;
    team_id: string;
    call_id: string;
    call_post_id: string;
    silent: boolean;
    expires_at: number;
    timeout_seconds: number;
};

export type IncomingRing = IncomingRingPayload & {

    // Local deadline, computed from the timeout so that clock skew does not matter.
    deadline: number;
};

export type WebSocketMessage<T> = {
    event: string;
    data: T;
    broadcast: {
        channel_id?: string;
        user_id?: string;
        team_id?: string;
    };
};

export type ChannelUser = {
    id: string;
    username: string;
    first_name?: string;
    last_name?: string;
    nickname?: string;
    is_bot?: boolean;
    delete_at?: number;
};
