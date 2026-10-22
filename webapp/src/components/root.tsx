import React from 'react';

import ConfirmDialog from './confirm_dialog';
import IncomingCallModal from './incoming_call_modal';
import RingModal from './ring_modal';

// Root is registered as a root component and hosts all of the plugin's dialogs.
export default function Root() {
    return (
        <>
            <RingModal/>
            <ConfirmDialog/>
            <IncomingCallModal/>
        </>
    );
}
