// Integration with the Calls plugin webapp (com.mattermost.calls).
//
// The Calls webapp listens for same-origin window messages of type "calls-run-slash-command" and
// runs them through its own /call command handler. "/call join" joins the call in the channel, or
// starts one when none is running. This is how this plugin starts and joins calls without
// reimplementing WebRTC or depending on Calls internals.

export const CALLS_PLUGIN_ID = 'com.mattermost.calls';

// Calls WebSocket events this plugin listens to (the server prefixes plugin events with "custom_<id>_").
export const CALLS_EVENT_CALL_START = `custom_${CALLS_PLUGIN_ID}_call_start`;
export const CALLS_EVENT_CALL_END = `custom_${CALLS_PLUGIN_ID}_call_end`;
export const CALLS_EVENT_USER_JOINED = `custom_${CALLS_PLUGIN_ID}_user_joined`;

declare global {
    interface Window {

        // Set by the Calls webapp while this window is connected to a call.
        callsClient?: {channelID?: string};
    }
}

// connectedCallChannelId returns the channel of the call this window is connected to, if any.
// In the Desktop app the call runs in a separate window, so this is only a best-effort check.
export function connectedCallChannelId(): string | undefined {
    return window.callsClient?.channelID || undefined;
}

export function joinCall(channelId: string, teamId?: string) {
    window.postMessage({
        type: 'calls-run-slash-command',
        message: '/call join',
        args: {
            channel_id: channelId,
            team_id: teamId || '',
            root_id: '',
        },
    }, window.origin);
}
