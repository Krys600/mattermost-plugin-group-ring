import React from 'react';

type Props = {
    size?: number;
};

// RingIcon is a phone handset with sound waves, drawn in the current text color.
export function RingIcon({size = 18}: Props) {
    return (
        <svg
            width={size}
            height={size}
            viewBox='0 0 24 24'
            fill='currentColor'
            aria-hidden='true'
        >
            <path d='M6.62 10.79a15.05 15.05 0 0 0 6.59 6.59l2.2-2.2a1 1 0 0 1 1.02-.24c1.12.37 2.33.57 3.57.57a1 1 0 0 1 1 1V20a1 1 0 0 1-1 1C10.61 21 3 13.39 3 4a1 1 0 0 1 1-1h3.5a1 1 0 0 1 1 1c0 1.25.2 2.45.57 3.57a1 1 0 0 1-.25 1.02l-2.2 2.2z'/>
            <path d='M15 3.5a6.5 6.5 0 0 1 5.5 5.5h-1.53A5 5 0 0 0 15 5.03V3.5zm0 3.03A3.5 3.5 0 0 1 17.47 9H15.9A2 2 0 0 0 15 8.1V6.53z'/>
        </svg>
    );
}
