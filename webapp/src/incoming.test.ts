import * as client from './client';
import {
    acceptRing,
    declineRing,
    handleCallEnded,
    handleIncomingRing,
    handleRingStop,
    handleUserJoined,
    resetIncoming,
} from './incoming';
import {setReduxStore} from './redux_state';
import type {IncomingRingPayload, WebSocketMessage} from './types';
import {uiStore} from './ui_store';

jest.mock('./client', () => {
    const actual = jest.requireActual('./client');
    return {...actual, respond: jest.fn()};
});

const respond = client.respond as jest.MockedFunction<typeof client.respond>;

const CHANNEL_ID = 'channel1channel1channel1ch';

function ringEvent(ringId: string, overrides: Partial<IncomingRingPayload> = {}): WebSocketMessage<IncomingRingPayload> {
    return {
        event: 'custom_ch.utaku.group-ring_incoming_ring',
        broadcast: {user_id: 'me'},
        data: {
            ring_id: ringId,
            caller_id: 'caller',
            caller_username: 'caller',
            caller_display_name: 'Caller',
            channel_id: CHANNEL_ID,
            channel_name: 'town-square',
            channel_display_name: 'Town Square',
            channel_type: 'O',
            team_id: 'team',
            call_id: '',
            call_post_id: '',
            silent: false,
            expires_at: 0,
            timeout_seconds: 30,
            ...overrides,
        },
    };
}

const ringIds = () => uiStore.getState().incoming.map((r) => r.ring_id);

beforeEach(() => {
    jest.useFakeTimers();
    resetIncoming();
    delete window.callsClient;
    setReduxStore({
        getState: () => ({
            entities: {
                users: {currentUserId: 'me', profiles: {}},
                channels: {currentChannelId: 'other', channels: {}},
                teams: {currentTeamId: 'team', teams: {team: {id: 'team', name: 'acme'}}},
                posts: {posts: {}},
            },
        }),
    } as never);
});

afterEach(() => {
    jest.useRealTimers();
});

test('shows a ring and acknowledges it', () => {
    handleIncomingRing(ringEvent('r1'));
    handleIncomingRing(ringEvent('r1'));
    expect(ringIds()).toEqual(['r1']);
    expect(respond).toHaveBeenCalledTimes(1);
    expect(respond).toHaveBeenCalledWith('r1', 'ack');
});

test('stops ringing after the timeout and reports a missed call', () => {
    handleIncomingRing(ringEvent('r1', {timeout_seconds: 10}));
    jest.advanceTimersByTime(9999);
    expect(ringIds()).toEqual(['r1']);
    jest.advanceTimersByTime(1);
    expect(ringIds()).toEqual([]);
    expect(respond).toHaveBeenLastCalledWith('r1', 'missed');
});

test('stops when another session answered or the caller cancelled', () => {
    handleIncomingRing(ringEvent('r1'));
    respond.mockClear();
    handleRingStop({event: 'ring_stop', broadcast: {}, data: {ring_id: 'r1', reason: 'cancelled'}});
    expect(ringIds()).toEqual([]);
    expect(respond).not.toHaveBeenCalled();
    jest.advanceTimersByTime(60000);
    expect(respond).not.toHaveBeenCalled();
});

test('decline reports the decline', () => {
    handleIncomingRing(ringEvent('r1'));
    declineRing(uiStore.getState().incoming[0]);
    expect(ringIds()).toEqual([]);
    expect(respond).toHaveBeenLastCalledWith('r1', 'decline');
});

test('accept joins the call and answers every ring for the channel', () => {
    const postMessage = jest.spyOn(window, 'postMessage').mockImplementation(() => undefined);
    handleIncomingRing(ringEvent('r1'));
    handleIncomingRing(ringEvent('r2'));
    handleIncomingRing(ringEvent('r3', {channel_id: 'elsewhere'}));

    acceptRing(uiStore.getState().incoming[0]);
    expect(ringIds()).toEqual(['r3']);
    expect(respond).toHaveBeenCalledWith('r1', 'accept');
    expect(respond).toHaveBeenCalledWith('r2', 'accept');
    expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({type: 'calls-run-slash-command', message: '/call join'}), window.origin);
    postMessage.mockRestore();
});

test('joining the call from another device stops the ring', () => {
    handleIncomingRing(ringEvent('r1'));
    handleUserJoined({event: 'user_joined', broadcast: {channel_id: CHANNEL_ID}, data: {user_id: 'someone-else'}});
    expect(ringIds()).toEqual(['r1']);
    handleUserJoined({event: 'user_joined', broadcast: {channel_id: CHANNEL_ID}, data: {user_id: 'me'}});
    expect(ringIds()).toEqual([]);
    expect(respond).toHaveBeenLastCalledWith('r1', 'accept');
});

test('the end of the call stops the ring', () => {
    handleIncomingRing(ringEvent('r1'));
    handleCallEnded({event: 'call_end', broadcast: {channel_id: CHANNEL_ID}, data: {}});
    expect(ringIds()).toEqual([]);
    expect(respond).toHaveBeenLastCalledWith('r1', 'missed');
});

test('a ring into the call the user is already in is not shown', () => {
    window.callsClient = {channelID: CHANNEL_ID};
    handleIncomingRing(ringEvent('r1'));
    expect(ringIds()).toEqual([]);
    expect(respond).toHaveBeenCalledWith('r1', 'ack');
    expect(respond).toHaveBeenCalledWith('r1', 'accept');
});
