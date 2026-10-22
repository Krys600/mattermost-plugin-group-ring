import type {Store} from 'redux';

import type {Channel} from '@mattermost/types/channels';
import type {Post} from '@mattermost/types/posts';
import type {GlobalState} from '@mattermost/types/store';
import type {Team} from '@mattermost/types/teams';
import type {UserProfile} from '@mattermost/types/users';

// Read-only accessors for the Mattermost Redux state. They read the documented state shape
// directly instead of bundling mattermost-redux selectors.

let reduxStore: Store<GlobalState> | null = null;

export function setReduxStore(store: Store<GlobalState>) {
    reduxStore = store;
}

function state(): GlobalState | null {
    return reduxStore ? reduxStore.getState() : null;
}

export function currentUserId(): string {
    return state()?.entities.users.currentUserId || '';
}

export function currentUserLocale(): string {
    const s = state();
    if (!s) {
        return 'en';
    }
    return s.entities.users.profiles[s.entities.users.currentUserId]?.locale || 'en';
}

export function currentChannelId(): string {
    return state()?.entities.channels.currentChannelId || '';
}

export function currentTeamId(): string {
    return state()?.entities.teams.currentTeamId || '';
}

export function getChannel(channelId: string): Channel | undefined {
    return state()?.entities.channels.channels[channelId];
}

export function getTeam(teamId: string): Team | undefined {
    return state()?.entities.teams.teams[teamId];
}

export function getPost(postId: string): Post | undefined {
    return state()?.entities.posts.posts[postId];
}

export function getUser(userId: string): UserProfile | undefined {
    return state()?.entities.users.profiles[userId];
}
