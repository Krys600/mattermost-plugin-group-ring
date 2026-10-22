import React from 'react';
import {FormattedMessage} from 'react-intl';

import * as styles from './styles';

import {uiStore, useUIState} from '../ui_store';

// ConfirmDialog asks the caller to confirm a ring above the configured threshold.
export default function ConfirmDialog() {
    const confirm = useUIState((state) => state.confirm);
    if (!confirm) {
        return null;
    }

    const close = () => uiStore.setState({confirm: null});
    const proceed = () => {
        close();
        confirm.onConfirm();
    };

    return (
        <div
            style={styles.backdrop}
            role='presentation'
        >
            <div
                style={styles.dialog}
                role='dialog'
                aria-modal='true'
                aria-labelledby='group-ring-confirm-title'
                data-testid='group-ring-confirm'
            >
                <h2
                    id='group-ring-confirm-title'
                    style={styles.title}
                >
                    <FormattedMessage
                        id='group_ring.confirm.title'
                        defaultMessage='Ring {count} people?'
                        values={{count: confirm.count}}
                    />
                </h2>
                <p style={{...styles.subtitle, marginTop: 12}}>
                    <FormattedMessage
                        id='group_ring.confirm.body'
                        defaultMessage='Everyone selected will get an incoming call notification.'
                    />
                </p>
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
                        style={styles.primaryButton}
                        onClick={proceed}
                        autoFocus={true}
                    >
                        <FormattedMessage
                            id='group_ring.confirm.ring'
                            defaultMessage='Ring {count} people'
                            values={{count: confirm.count}}
                        />
                    </button>
                </div>
            </div>
        </div>
    );
}
