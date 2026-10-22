import React, {useEffect, useMemo, useState} from 'react';
import {FormattedMessage, useIntl} from 'react-intl';

import * as styles from './styles';

import {fetchChannelMemberCount, NetworkError, searchChannelUsers, userImageUrl} from '../client';
import {submitRing} from '../outgoing';
import {currentUserId, getChannel, getUser} from '../redux_state';
import type {ChannelUser} from '../types';
import {uiStore, useUIState} from '../ui_store';
import type {RingModalState} from '../ui_store';

const SEARCH_DEBOUNCE_MS = 250;

function displayName(user: ChannelUser): string {
    const fullName = [user.first_name, user.last_name].filter(Boolean).join(' ');
    return fullName ? `@${user.username} - ${fullName}` : `@${user.username}`;
}

// RingModal lets the caller ring the whole channel or pick people from the channel members.
export default function RingModal() {
    const modal = useUIState((state) => state.ringModal);
    const groupCallsAllowed = useUIState((state) => state.callsGroupCallsAllowed);
    if (!modal) {
        return null;
    }

    // Remount for every opening so the form starts fresh.
    return (
        <RingModalContent
            key={`${modal.channelId}-${modal.mode}-${modal.preselectedUserIds.join(',')}`}
            modal={modal}
            groupCallsBlocked={groupCallsAllowed === false && getChannel(modal.channelId)?.type !== 'D'}
        />
    );
}

function RingModalContent({modal, groupCallsBlocked}: {modal: RingModalState; groupCallsBlocked: boolean}) {
    const intl = useIntl();
    const [mode, setMode] = useState<'people' | 'channel'>(modal.mode);
    const [term, setTerm] = useState('');
    const [results, setResults] = useState<ChannelUser[]>([]);
    const [selected, setSelected] = useState<ChannelUser[]>(() => modal.preselectedUserIds.
        map((id) => getUser(id)).
        filter((u): u is NonNullable<typeof u> => Boolean(u)).
        map((u) => ({id: u.id, username: u.username, first_name: u.first_name, last_name: u.last_name})));
    const [memberCount, setMemberCount] = useState<number | null>(null);
    const [error, setError] = useState('');
    const [submitting, setSubmitting] = useState(false);

    const channel = getChannel(modal.channelId);
    const me = currentUserId();
    const close = () => uiStore.setState({ringModal: null});

    useEffect(() => {
        fetchChannelMemberCount(modal.channelId).then(setMemberCount);
    }, [modal.channelId]);

    useEffect(() => {
        if (mode !== 'people') {
            return undefined;
        }
        let cancelled = false;
        const timer = setTimeout(async () => {
            const users = await searchChannelUsers(modal.channelId, term.trim());
            if (!cancelled) {
                setResults(users);
            }
        }, SEARCH_DEBOUNCE_MS);
        return () => {
            cancelled = true;
            clearTimeout(timer);
        };
    }, [modal.channelId, term, mode]);

    useEffect(() => {
        const onKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                close();
            }
        };
        document.addEventListener('keydown', onKeyDown);
        return () => document.removeEventListener('keydown', onKeyDown);
    }, []);

    const selectedIds = useMemo(() => new Set(selected.map((u) => u.id)), [selected]);
    const candidates = results.filter((u) => u.id !== me && !u.is_bot && !u.delete_at && !selectedIds.has(u.id));
    const canSubmit = !submitting && !groupCallsBlocked && (mode === 'channel' || selected.length > 0);

    const submit = async () => {
        if (!canSubmit) {
            return;
        }
        setSubmitting(true);
        setError('');
        try {
            const outcome = await submitRing({
                channel_id: modal.channelId,
                team_id: modal.teamId,
                whole_channel: mode === 'channel',
                user_ids: mode === 'people' ? selected.map((u) => u.id) : undefined,
            });
            if (outcome.kind === 'error') {
                setError(outcome.text);
                setSubmitting(false);
                return;
            }
            close();
        } catch (err) {
            setError(err instanceof NetworkError ? intl.formatMessage({id: 'group_ring.modal.network_error', defaultMessage: 'Could not reach the server. Please try again.'}) : String(err));
            setSubmitting(false);
        }
    };

    const channelLabel = channel?.display_name || channel?.name || '';
    const othersCount = memberCount === null ? null : Math.max(memberCount - 1, 0);

    return (
        <div
            style={styles.backdrop}
            role='presentation'
            onClick={(e) => {
                if (e.target === e.currentTarget) {
                    close();
                }
            }}
        >
            <div
                style={styles.dialog}
                role='dialog'
                aria-modal='true'
                aria-labelledby='group-ring-modal-title'
                data-testid='group-ring-modal'
            >
                <button
                    type='button'
                    style={styles.closeButton}
                    onClick={close}
                    aria-label={intl.formatMessage({id: 'group_ring.common.close', defaultMessage: 'Close'})}
                >
                    {'×'}
                </button>
                <h2
                    id='group-ring-modal-title'
                    style={styles.title}
                >
                    <FormattedMessage
                        id='group_ring.modal.title'
                        defaultMessage='Ring people'
                    />
                </h2>
                <div style={styles.subtitle}>
                    <FormattedMessage
                        id='group_ring.modal.subtitle'
                        defaultMessage='Starts a call in {channel} (or joins the one that is running) and rings the people you choose.'
                        values={{channel: channelLabel}}
                    />
                </div>

                <div style={{marginTop: 16}}>
                    <label style={styles.radioLabel}>
                        <input
                            type='radio'
                            name='group-ring-mode'
                            checked={mode === 'people'}
                            onChange={() => setMode('people')}
                        />
                        <FormattedMessage
                            id='group_ring.modal.mode_people'
                            defaultMessage='Choose people'
                        />
                    </label>
                    <label style={styles.radioLabel}>
                        <input
                            type='radio'
                            name='group-ring-mode'
                            checked={mode === 'channel'}
                            onChange={() => setMode('channel')}
                        />
                        {othersCount === null ? (
                            <FormattedMessage
                                id='group_ring.modal.mode_channel'
                                defaultMessage='Everyone in this channel'
                            />
                        ) : (
                            <FormattedMessage
                                id='group_ring.modal.mode_channel_count'
                                defaultMessage='Everyone in this channel ({count, plural, one {# other member} other {# other members}})'
                                values={{count: othersCount}}
                            />
                        )}
                    </label>
                </div>

                {mode === 'people' && (
                    <div style={{marginTop: 12}}>
                        {selected.length > 0 && (
                            <div style={{marginBottom: 6}}>
                                {selected.map((u) => (
                                    <span
                                        key={u.id}
                                        style={styles.chip}
                                    >
                                        <img
                                            src={userImageUrl(u.id)}
                                            alt=''
                                            style={styles.smallAvatar}
                                        />
                                        {`@${u.username}`}
                                        <button
                                            type='button'
                                            style={styles.chipRemove}
                                            onClick={() => setSelected(selected.filter((s) => s.id !== u.id))}
                                            aria-label={intl.formatMessage({id: 'group_ring.modal.remove', defaultMessage: 'Remove {name}'}, {name: u.username})}
                                        >
                                            {'×'}
                                        </button>
                                    </span>
                                ))}
                            </div>
                        )}
                        <input
                            type='text'
                            style={styles.input}
                            value={term}
                            onChange={(e) => setTerm(e.target.value)}
                            placeholder={intl.formatMessage({id: 'group_ring.modal.search', defaultMessage: 'Search channel members'})}
                            aria-label={intl.formatMessage({id: 'group_ring.modal.search', defaultMessage: 'Search channel members'})}
                            autoFocus={true}
                        />
                        <div
                            style={{marginTop: 8, maxHeight: 240, overflowY: 'auto'}}
                            role='listbox'
                        >
                            {candidates.map((u) => (
                                <button
                                    key={u.id}
                                    type='button'
                                    role='option'
                                    aria-selected={false}
                                    style={styles.resultRow}
                                    onClick={() => {
                                        setSelected([...selected, u]);
                                        setTerm('');
                                    }}
                                >
                                    <img
                                        src={userImageUrl(u.id)}
                                        alt=''
                                        style={styles.smallAvatar}
                                    />
                                    {displayName(u)}
                                </button>
                            ))}
                            {candidates.length === 0 && term.trim() !== '' && (
                                <div style={styles.subtitle}>
                                    <FormattedMessage
                                        id='group_ring.modal.no_results'
                                        defaultMessage='No matching channel members.'
                                    />
                                </div>
                            )}
                        </div>
                    </div>
                )}

                {groupCallsBlocked && (
                    <div style={styles.error}>
                        <FormattedMessage
                            id='group_ring.modal.group_calls_blocked'
                            defaultMessage='The Calls plugin on this server only allows calls in direct messages, so nobody can be rung here.'
                        />
                    </div>
                )}
                {error && <div style={styles.error}>{error}</div>}

                <div style={styles.footer}>
                    <button
                        type='button'
                        style={styles.secondaryButton}
                        onClick={close}
                    >
                        <FormattedMessage
                            id='group_ring.common.cancel'
                            defaultMessage='Cancel'
                        />
                    </button>
                    <button
                        type='button'
                        style={canSubmit ? styles.primaryButton : {...styles.primaryButton, ...styles.disabled}}
                        onClick={submit}
                        disabled={!canSubmit}
                    >
                        {mode === 'people' ? (
                            <FormattedMessage
                                id='group_ring.modal.ring_people'
                                defaultMessage='{count, plural, =0 {Ring} one {Ring # person} other {Ring # people}}'
                                values={{count: selected.length}}
                            />
                        ) : (
                            <FormattedMessage
                                id='group_ring.modal.ring_channel'
                                defaultMessage='Ring channel'
                            />
                        )}
                    </button>
                </div>
            </div>
        </div>
    );
}
