import {useSyncExternalStore} from 'react';

import type {ClientConfig, IncomingRing} from './types';

export type RingModalState = {
    channelId: string;
    teamId: string;
    mode: 'people' | 'channel';
    preselectedUserIds: string[];
};

export type ConfirmState = {
    count: number;
    onConfirm: () => void;
};

export type UIState = {
    config: ClientConfig | null;

    // The Calls plugin's "GroupCallsAllowed" setting; null when unknown.
    callsGroupCallsAllowed: boolean | null;

    // Incoming rings, oldest first. The first one is shown.
    incoming: IncomingRing[];
    ringModal: RingModalState | null;
    confirm: ConfirmState | null;
};

const initialState: UIState = {
    config: null,
    callsGroupCallsAllowed: null,
    incoming: [],
    ringModal: null,
    confirm: null,
};

// UIStore is a minimal external store for the plugin's own UI state, kept outside the
// Mattermost Redux store on purpose: the state is transient and private to this plugin.
export class UIStore {
    private state: UIState = initialState;
    private listeners = new Set<() => void>();

    getState = (): UIState => this.state;

    setState = (update: Partial<UIState> | ((state: UIState) => Partial<UIState>)) => {
        const partial = typeof update === 'function' ? update(this.state) : update;
        this.state = {...this.state, ...partial};
        this.listeners.forEach((listener) => listener());
    };

    subscribe = (listener: () => void) => {
        this.listeners.add(listener);
        return () => {
            this.listeners.delete(listener);
        };
    };

    reset = () => {
        this.state = initialState;
        this.listeners.forEach((listener) => listener());
    };
}

export const uiStore = new UIStore();

export function useUIState<T>(selector: (state: UIState) => T): T {
    return useSyncExternalStore(uiStore.subscribe, () => selector(uiStore.getState()));
}
