import React, {useEffect, useRef} from 'react';
import {FormattedMessage, useIntl} from 'react-intl';

import * as styles from './styles';

import {userImageUrl} from '../client';
import {acceptRing, declineRing, dismissRing} from '../incoming';
import {Ringtone} from '../ringtone';
import {useUIState} from '../ui_store';

// IncomingCallModal shows the oldest pending ring with Accept, Decline and Dismiss. It plays the
// ringtone unless the ring is silent (Do Not Disturb).
export default function IncomingCallModal() {
    const intl = useIntl();
    const incoming = useUIState((state) => state.incoming);
    const ring = incoming.length > 0 ? incoming[0] : null;
    const ringtone = useRef<Ringtone | null>(null);

    const shouldPlay = Boolean(ring && !ring.silent);
    useEffect(() => {
        if (!ringtone.current) {
            ringtone.current = new Ringtone();
        }
        if (shouldPlay) {
            ringtone.current.start();
        } else {
            ringtone.current.stop();
        }
    }, [shouldPlay]);

    useEffect(() => () => ringtone.current?.stop(), []);

    useEffect(() => {
        if (!ring) {
            return undefined;
        }
        const onKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                dismissRing(ring);
            }
        };
        document.addEventListener('keydown', onKeyDown);
        return () => document.removeEventListener('keydown', onKeyDown);
    }, [ring]);

    if (!ring) {
        return null;
    }

    const callerName = ring.caller_display_name || ring.caller_username;
    let location: React.ReactNode;
    switch (ring.channel_type) {
    case 'D':
        location = (
            <FormattedMessage
                id='group_ring.incoming.direct'
                defaultMessage='Direct message'
            />
        );
        break;
    case 'G':
        location = (
            <FormattedMessage
                id='group_ring.incoming.group'
                defaultMessage='Group message'
            />
        );
        break;
    default:
        location = (
            <FormattedMessage
                id='group_ring.incoming.channel'
                defaultMessage='in {channel}'
                values={{channel: ring.channel_display_name || ring.channel_name}}
            />
        );
    }

    return (
        <div
            style={styles.backdrop}
            role='presentation'
        >
            <div
                style={{...styles.dialog, textAlign: 'center'}}
                role='alertdialog'
                aria-modal='true'
                aria-labelledby='group-ring-incoming-title'
                data-testid='group-ring-incoming'
            >
                <button
                    type='button'
                    style={styles.closeButton}
                    onClick={() => dismissRing(ring)}
                    aria-label={intl.formatMessage({id: 'group_ring.incoming.dismiss', defaultMessage: 'Dismiss'})}
                    title={intl.formatMessage({id: 'group_ring.incoming.dismiss', defaultMessage: 'Dismiss'})}
                >
                    {'×'}
                </button>
                <img
                    src={userImageUrl(ring.caller_id)}
                    alt=''
                    style={styles.avatar}
                />
                <h2
                    id='group-ring-incoming-title'
                    style={{...styles.title, marginTop: 12}}
                >
                    <FormattedMessage
                        id='group_ring.incoming.title'
                        defaultMessage='{name} is calling you'
                        values={{name: callerName}}
                    />
                </h2>
                <div style={styles.subtitle}>{location}</div>
                {ring.silent && (
                    <div style={styles.subtitle}>
                        <FormattedMessage
                            id='group_ring.incoming.silent'
                            defaultMessage='Ringing without sound because you are in Do Not Disturb.'
                        />
                    </div>
                )}
                {incoming.length > 1 && (
                    <div style={styles.subtitle}>
                        <FormattedMessage
                            id='group_ring.incoming.more'
                            defaultMessage='{count, plural, one {# more incoming call} other {# more incoming calls}}'
                            values={{count: incoming.length - 1}}
                        />
                    </div>
                )}
                <div style={{...styles.footer, justifyContent: 'center'}}>
                    <button
                        type='button'
                        style={styles.declineButton}
                        onClick={() => declineRing(ring)}
                    >
                        <FormattedMessage
                            id='group_ring.incoming.decline'
                            defaultMessage='Decline'
                        />
                    </button>
                    <button
                        type='button'
                        style={styles.acceptButton}
                        onClick={() => acceptRing(ring)}
                        autoFocus={true}
                    >
                        <FormattedMessage
                            id='group_ring.incoming.accept'
                            defaultMessage='Accept'
                        />
                    </button>
                </div>
            </div>
        </div>
    );
}
