import {currentChannelId, currentTeamId, getChannel, getTeam} from './redux_state';

declare global {
    interface Window {
        WebappUtils?: {
            browserHistory?: {push: (path: string) => void};
        };
    }
}

// navigateToChannel opens a channel unless it is already the current one. DM and GM channels have
// no team, so the current team is used; the webapp resolves DM and GM names under /channels/.
export function navigateToChannel(channelId: string, channelName: string, teamId?: string) {
    if (channelId === currentChannelId()) {
        return;
    }
    const channel = getChannel(channelId);
    const name = channel?.name || channelName;
    const team = getTeam(channel?.team_id || teamId || '') || getTeam(currentTeamId());
    if (!name || !team) {
        return;
    }
    window.WebappUtils?.browserHistory?.push(`/${team.name}/channels/${name}`);
}
