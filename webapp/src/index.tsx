import manifest from 'manifest';
import React from 'react';
import {FormattedMessage} from 'react-intl';
import type {Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import type {PluginRegistry} from 'types/mattermost-webapp';

import {CALLS_EVENT_CALL_END, CALLS_EVENT_CALL_START, CALLS_EVENT_USER_JOINED} from './calls_bridge';
import {fetchCallsGroupCallsAllowed, fetchConfig} from './client';
import {RingIcon} from './components/icons';
import Root from './components/root';
import {getTranslations} from './i18n';
import {
    handleCallEnded,
    handleCallStarted,
    handleIncomingRing,
    handleRingStop,
    handleRingUpdated,
    handleUserJoined,
    resetIncoming,
} from './incoming';
import {onCallStarted, slashCommandHook, submitRing} from './outgoing';
import {currentTeamId, currentUserId, getChannel, getPost, setReduxStore} from './redux_state';
import type {ChannelTypeCode, IncomingRingPayload, WebSocketMessage} from './types';
import {uiStore} from './ui_store';

const pluginEvent = (name: string) => `custom_${manifest.id}_${name}`;

async function loadConfig() {
    const [config, callsGroupCallsAllowed] = await Promise.all([fetchConfig(), fetchCallsGroupCallsAllowed()]);
    uiStore.setState({config, callsGroupCallsAllowed});
}

// ringingAllowedIn reports whether the UI entry points should be shown for a channel. The server
// enforces the same rules; this only hides actions that would be rejected.
function ringingAllowedIn(channelId: string): boolean {
    const config = uiStore.getState().config;
    if (!config) {
        return true;
    }
    if (!config.enabled) {
        return false;
    }
    const channel = getChannel(channelId);
    if (!channel) {
        return true;
    }
    if (channel.type !== 'D' && uiStore.getState().callsGroupCallsAllowed === false) {
        return false;
    }
    return config.channel_types[channel.type as ChannelTypeCode] !== false;
}

function teamIdFor(channelId: string): string {
    return getChannel(channelId)?.team_id || currentTeamId();
}

function openRingModal(channelId: string, mode: 'people' | 'channel', preselectedUserIds: string[] = []) {
    uiStore.setState({ringModal: {channelId, teamId: teamIdFor(channelId), mode, preselectedUserIds}});
}

function ringWholeChannel(channelId: string) {
    // Sent as a command so that errors are reported in the channel like any /ring reply.
    submitRing({channel_id: channelId, team_id: teamIdFor(channelId), command: '/ring @channel'}).catch(() => undefined);
}

export default class Plugin {
    public async initialize(registry: PluginRegistry, store: Store<GlobalState>) {
        setReduxStore(store);
        registry.registerTranslations(getTranslations);
        registry.registerRootComponent(Root);

        registry.registerChannelHeaderButtonAction(
            <RingIcon/>,
            (channel) => openRingModal(channel.id, 'people'),
            (
                <FormattedMessage
                    id='group_ring.header.button'
                    defaultMessage='Ring people'
                />
            ),
            (
                <FormattedMessage
                    id='group_ring.header.tooltip'
                    defaultMessage='Ring people to join a call'
                />
            ),
        );

        const shouldRender = (state: GlobalState) => ringingAllowedIn(state.entities.channels.currentChannelId);
        registry.registerChannelHeaderMenuAction(
            (
                <FormattedMessage
                    id='group_ring.menu.ring_channel'
                    defaultMessage='Ring channel'
                />
            ),
            (channelId) => ringWholeChannel(channelId),
            shouldRender,
        );
        registry.registerChannelHeaderMenuAction(
            (
                <FormattedMessage
                    id='group_ring.menu.ring_people'
                    defaultMessage='Ring people...'
                />
            ),
            (channelId) => openRingModal(channelId, 'people'),
            shouldRender,
        );

        registry.registerPostDropdownMenuAction(
            (
                <FormattedMessage
                    id='group_ring.menu.ring_people'
                    defaultMessage='Ring people...'
                />
            ),
            (postId) => {
                const post = getPost(postId);
                if (!post) {
                    return;
                }
                const preselected = post.user_id && post.user_id !== currentUserId() ? [post.user_id] : [];
                openRingModal(post.channel_id, 'people', preselected);
            },
            (postId) => {
                const post = getPost(postId);
                return Boolean(post && !post.type?.startsWith('system_') && ringingAllowedIn(post.channel_id));
            },
        );

        registry.registerSlashCommandWillBePostedHook(slashCommandHook);

        registry.registerWebSocketEventHandler(pluginEvent('incoming_ring'), (msg) => handleIncomingRing(msg as unknown as WebSocketMessage<IncomingRingPayload>));
        registry.registerWebSocketEventHandler(pluginEvent('ring_updated'), (msg) => handleRingUpdated(msg as unknown as WebSocketMessage<{ring_id: string; call_id: string}>));
        registry.registerWebSocketEventHandler(pluginEvent('ring_stop'), (msg) => handleRingStop(msg as unknown as WebSocketMessage<{ring_id: string; reason: string}>));
        registry.registerWebSocketEventHandler(pluginEvent('config_changed'), () => {
            loadConfig();
        });

        // Calls plugin events: report the call ID of our rings and stop rings that are no longer needed.
        registry.registerWebSocketEventHandler(CALLS_EVENT_CALL_START, (msg) => {
            const ev = msg as unknown as WebSocketMessage<{id?: string; channelID?: string}>;
            handleCallStarted(ev);
            onCallStarted(ev.data?.channelID || ev.broadcast?.channel_id || '', ev.data?.id || '');
        });
        registry.registerWebSocketEventHandler(CALLS_EVENT_CALL_END, (msg) => handleCallEnded(msg as unknown as WebSocketMessage<{channelID?: string}>));
        registry.registerWebSocketEventHandler(CALLS_EVENT_USER_JOINED, (msg) => handleUserJoined(msg as unknown as WebSocketMessage<{user_id?: string; channelID?: string}>));
        registry.registerReconnectHandler(() => {
            loadConfig();
        });

        await loadConfig();
    }

    public uninitialize() {
        resetIncoming();
        uiStore.reset();
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
