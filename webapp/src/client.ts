import manifest from 'manifest';

import type {ChannelUser, ClientConfig, Outcome, RingAction} from './types';

const CALLS_PLUGIN_ID = 'com.mattermost.calls';

declare global {
    interface Window {
        basename?: string;
    }
}

function basePath(): string {
    return window.basename || '';
}

export function pluginApiUrl(path: string): string {
    return `${basePath()}/plugins/${manifest.id}/api/v1${path}`;
}

export function userImageUrl(userId: string): string {
    return `${basePath()}/api/v4/users/${encodeURIComponent(userId)}/image`;
}

function csrfToken(): string {
    const match = document.cookie.match(/(?:^|;\s*)MMCSRF=([^;]+)/);
    return match ? decodeURIComponent(match[1]) : '';
}

export class NetworkError extends Error {}

type Response<T> = {
    status: number;
    data: T;
};

// request performs a same-origin request with the session cookie. Non-GET requests carry the CSRF
// token, which the server requires for cookie-authenticated plugin requests.
async function request<T>(url: string, method = 'GET', body?: unknown): Promise<Response<T>> {
    const headers: Record<string, string> = {'X-Requested-With': 'XMLHttpRequest'};
    if (method !== 'GET') {
        headers['Content-Type'] = 'application/json';
        const token = csrfToken();
        if (token) {
            headers['X-CSRF-Token'] = token;
        }
    }

    let res: globalThis.Response;
    try {
        res = await fetch(url, {
            method,
            headers,
            credentials: 'include',
            body: body === undefined ? undefined : JSON.stringify(body),
        });
    } catch (err) {
        throw new NetworkError(String(err));
    }

    let data: unknown = null;
    try {
        data = await res.json();
    } catch {
        if (!res.ok) {
            throw new NetworkError(`HTTP ${res.status}`);
        }
    }
    return {status: res.status, data: data as T};
}

export async function fetchConfig(): Promise<ClientConfig | null> {
    try {
        const res = await request<ClientConfig>(pluginApiUrl('/config'));
        return res.status === 200 ? res.data : null;
    } catch {
        return null;
    }
}

export type StartRingBody = {
    channel_id: string;
    team_id?: string;
    command?: string;
    user_ids?: string[];
    whole_channel?: boolean;
    confirmed?: boolean;
    call_active?: boolean;
    group_calls_allowed?: boolean;
};

// startRing returns the outcome for both successful and rejected rings. It throws NetworkError
// when the plugin cannot be reached.
export async function startRing(body: StartRingBody): Promise<Outcome> {
    const res = await request<Outcome>(pluginApiUrl('/rings'), 'POST', body);
    if (!res.data || typeof res.data.kind !== 'string') {
        throw new NetworkError(`unexpected response (HTTP ${res.status})`);
    }
    return res.data;
}

// respond reports a recipient action. Failures are ignored: the ring expires on its own.
export async function respond(ringId: string, action: RingAction): Promise<void> {
    try {
        await request(pluginApiUrl(`/rings/${ringId}/respond`), 'POST', {action});
    } catch {
        // Best effort.
    }
}

export async function cancelRing(ringId: string): Promise<void> {
    try {
        await request(pluginApiUrl(`/rings/${ringId}/cancel`), 'POST', {});
    } catch {
        // Best effort.
    }
}

export async function reportCallId(ringId: string, callId: string): Promise<void> {
    try {
        await request(pluginApiUrl(`/rings/${ringId}/call`), 'POST', {call_id: callId});
    } catch {
        // Best effort.
    }
}

export async function searchChannelUsers(channelId: string, term: string): Promise<ChannelUser[]> {
    const params = new URLSearchParams({in_channel: channelId, name: term, limit: '25'});
    try {
        const res = await request<{users?: ChannelUser[]}>(`${basePath()}/api/v4/users/autocomplete?${params.toString()}`);
        return res.status === 200 ? res.data?.users || [] : [];
    } catch {
        return [];
    }
}

export async function fetchChannelMemberCount(channelId: string): Promise<number | null> {
    try {
        const res = await request<{member_count?: number}>(`${basePath()}/api/v4/channels/${channelId}/stats`);
        return res.status === 200 && typeof res.data?.member_count === 'number' ? res.data.member_count : null;
    } catch {
        return null;
    }
}

// fetchCallsGroupCallsAllowed reads the Calls plugin's "GroupCallsAllowed" client setting. Calls v1
// only allows calls outside direct messages with a license or when the server sets
// MM_CALLS_GROUP_CALLS_ALLOWED=true. Returns null when the setting cannot be read.
export async function fetchCallsGroupCallsAllowed(): Promise<boolean | null> {
    try {
        const res = await request<{GroupCallsAllowed?: boolean}>(`${basePath()}/plugins/${CALLS_PLUGIN_ID}/config`);
        return res.status === 200 && typeof res.data?.GroupCallsAllowed === 'boolean' ? res.data.GroupCallsAllowed : null;
    } catch {
        return null;
    }
}

// isCallActive asks the Calls plugin whether a call is running in the channel. It uses the
// caller's session, so it works where the server-side inter-plugin API cannot.
export async function isCallActive(channelId: string): Promise<boolean> {
    try {
        const res = await request<{active?: boolean}>(`${basePath()}/plugins/${CALLS_PLUGIN_ID}/calls/${channelId}/active`);
        return res.status === 200 && Boolean(res.data?.active);
    } catch {
        return false;
    }
}
