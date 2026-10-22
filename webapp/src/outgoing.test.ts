import * as client from './client';
import {isRingCommand, slashCommandHook, submitRing} from './outgoing';
import {setReduxStore} from './redux_state';
import type {Outcome} from './types';
import {uiStore} from './ui_store';

jest.mock('./client', () => {
    const actual = jest.requireActual('./client');
    return {
        ...actual,
        startRing: jest.fn(),
        isCallActive: jest.fn(),
        reportCallId: jest.fn(),
    };
});

const startRing = client.startRing as jest.MockedFunction<typeof client.startRing>;
const isCallActive = client.isCallActive as jest.MockedFunction<typeof client.isCallActive>;

const CHANNEL_ID = 'channel1channel1channel1ch';
const TEAM_ID = 'team1team1team1team1team1t';

function ringingOutcome(): Outcome {
    return {
        kind: 'ringing',
        text: 'Ringing 2 people',
        result: {
            ring_id: 'ring1ring1ring1ring1ring1r',
            channel_id: CHANNEL_ID,
            channel_name: 'town-square',
            team_id: TEAM_ID,
            call_active: false,
            recipient_count: 2,
            rung: ['alice', 'bob'],
            silent: [],
            dm_only: [],
            skipped: [],
            needs_confirmation: false,
            deferred_to_native: false,
            timeout_seconds: 30,
        },
    };
}

// flush lets pending promise callbacks run while timers are faked.
const flush = () => new Promise((resolve) => jest.requireActual('timers').setImmediate(resolve));

afterEach(() => {
    jest.clearAllTimers();
    jest.useRealTimers();
});

beforeEach(() => {
    jest.useFakeTimers();
    uiStore.reset();
    isCallActive.mockResolvedValue(false);
    setReduxStore({
        getState: () => ({
            entities: {
                users: {currentUserId: 'me', profiles: {}},
                channels: {currentChannelId: CHANNEL_ID, channels: {}},
                teams: {currentTeamId: TEAM_ID, teams: {}},
                posts: {posts: {}},
            },
        }),
    } as never);
});

describe('isRingCommand', () => {
    test.each([
        ['/ring @alice', true],
        ['  /RING --help', true],
        ['/ring', true],
        ['/ringtone', false],
        ['/call start', false],
        ['ring @alice', false],
    ])('%s -> %s', (message, expected) => {
        expect(isRingCommand(message)).toBe(expected);
    });
});

describe('slashCommandHook', () => {
    const args = {channel_id: CHANNEL_ID, team_id: TEAM_ID};

    test('passes other commands through', async () => {
        await expect(slashCommandHook('/call start', args)).resolves.toEqual({message: '/call start', args});
        expect(startRing).not.toHaveBeenCalled();
    });

    test('consumes /ring, then starts or joins the call through the Calls webapp', async () => {
        startRing.mockResolvedValue(ringingOutcome());
        const postMessage = jest.spyOn(window, 'postMessage').mockImplementation(() => undefined);

        await expect(slashCommandHook('/ring @alice @bob', args)).resolves.toEqual({});

        expect(startRing).toHaveBeenCalledWith({channel_id: CHANNEL_ID, team_id: TEAM_ID, command: '/ring @alice @bob', call_active: false});
        expect(postMessage).toHaveBeenCalledWith({
            type: 'calls-run-slash-command',
            message: '/call join',
            args: {channel_id: CHANNEL_ID, team_id: TEAM_ID, root_id: ''},
        }, window.origin);
        postMessage.mockRestore();
    });

    test('falls back to the server-side command when the plugin API is unreachable', async () => {
        startRing.mockRejectedValue(new client.NetworkError('offline'));
        await expect(slashCommandHook('/ring @alice', args)).resolves.toEqual({message: '/ring @alice', args});
    });

    test('does not start a call when the ring is rejected', async () => {
        startRing.mockResolvedValue({kind: 'error', text: 'Rate limited', error_code: 'rate_limited'});
        const postMessage = jest.spyOn(window, 'postMessage').mockImplementation(() => undefined);
        await slashCommandHook('/ring @alice', args);
        expect(postMessage).not.toHaveBeenCalled();
        postMessage.mockRestore();
    });
});

describe('submitRing', () => {
    test('asks for confirmation and resends confirmed', async () => {
        const needsConfirmation = ringingOutcome();
        needsConfirmation.kind = 'needs_confirmation';
        needsConfirmation.result!.recipient_count = 42;
        startRing.mockResolvedValueOnce(needsConfirmation).mockResolvedValueOnce(ringingOutcome());
        const postMessage = jest.spyOn(window, 'postMessage').mockImplementation(() => undefined);

        await submitRing({channel_id: CHANNEL_ID, team_id: TEAM_ID, whole_channel: true});
        const confirm = uiStore.getState().confirm;
        expect(confirm?.count).toBe(42);
        expect(postMessage).not.toHaveBeenCalled();

        confirm!.onConfirm();
        await flush();
        expect(startRing).toHaveBeenLastCalledWith(expect.objectContaining({whole_channel: true, confirmed: true}));
        expect(postMessage).toHaveBeenCalledTimes(1);
        postMessage.mockRestore();
    });

    test('forwards the Calls group-calls setting when known', async () => {
        startRing.mockResolvedValue({kind: 'error', text: 'x'});
        uiStore.setState({callsGroupCallsAllowed: false});
        await submitRing({channel_id: CHANNEL_ID, command: '/ring @alice'});
        expect(startRing).toHaveBeenLastCalledWith(expect.objectContaining({group_calls_allowed: false}));

        uiStore.setState({callsGroupCallsAllowed: null});
        await submitRing({channel_id: CHANNEL_ID, command: '/ring @alice'});
        expect(startRing.mock.calls[startRing.mock.calls.length - 1][0].group_calls_allowed).toBeUndefined();
    });

    test('sends the active-call hint for the current channel only', async () => {
        isCallActive.mockResolvedValue(true);
        startRing.mockResolvedValue({kind: 'error', text: 'x'});
        await submitRing({channel_id: CHANNEL_ID, command: '/ring @alice'});
        expect(startRing).toHaveBeenLastCalledWith(expect.objectContaining({call_active: true}));

        await submitRing({channel_id: CHANNEL_ID, command: '/ring ~dev'});
        expect(startRing).toHaveBeenLastCalledWith(expect.objectContaining({call_active: false}));
    });
});
