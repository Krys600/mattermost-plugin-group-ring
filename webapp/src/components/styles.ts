import type React from 'react';

// Styles use the theme CSS variables defined by the Mattermost webapp, so the UI follows the
// user's theme.

export const backdrop: React.CSSProperties = {
    position: 'fixed',
    inset: 0,
    background: 'rgba(0, 0, 0, 0.5)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 1060,
};

export const dialog: React.CSSProperties = {
    position: 'relative',
    width: 'min(480px, calc(100vw - 32px))',
    maxHeight: 'calc(100vh - 64px)',
    overflowY: 'auto',
    background: 'var(--center-channel-bg)',
    color: 'var(--center-channel-color)',
    borderRadius: 12,
    boxShadow: '0 20px 32px rgba(0, 0, 0, 0.12)',
    padding: 24,
};

export const title: React.CSSProperties = {
    fontSize: 20,
    fontWeight: 600,
    lineHeight: '28px',
    margin: 0,
};

export const subtitle: React.CSSProperties = {
    fontSize: 14,
    opacity: 0.72,
    marginTop: 4,
};

export const closeButton: React.CSSProperties = {
    position: 'absolute',
    top: 12,
    right: 12,
    width: 32,
    height: 32,
    border: 'none',
    borderRadius: 4,
    background: 'transparent',
    color: 'var(--center-channel-color)',
    opacity: 0.64,
    fontSize: 20,
    cursor: 'pointer',
};

export const footer: React.CSSProperties = {
    display: 'flex',
    justifyContent: 'flex-end',
    gap: 8,
    marginTop: 24,
};

const buttonBase: React.CSSProperties = {
    border: 'none',
    borderRadius: 4,
    padding: '10px 20px',
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
};

export const primaryButton: React.CSSProperties = {
    ...buttonBase,
    background: 'var(--button-bg)',
    color: 'var(--button-color)',
};

export const secondaryButton: React.CSSProperties = {
    ...buttonBase,
    background: 'rgba(var(--button-bg-rgb), 0.08)',
    color: 'var(--button-bg)',
};

export const acceptButton: React.CSSProperties = {
    ...buttonBase,
    background: 'var(--online-indicator)',
    color: '#fff',
};

export const declineButton: React.CSSProperties = {
    ...buttonBase,
    background: 'var(--dnd-indicator)',
    color: '#fff',
};

export const disabled: React.CSSProperties = {
    opacity: 0.48,
    cursor: 'not-allowed',
};

export const error: React.CSSProperties = {
    color: 'var(--error-text)',
    fontSize: 14,
    marginTop: 12,
    whiteSpace: 'pre-wrap',
};

export const input: React.CSSProperties = {
    width: '100%',
    boxSizing: 'border-box',
    padding: '8px 12px',
    fontSize: 14,
    borderRadius: 4,
    border: '1px solid rgba(var(--center-channel-color-rgb), 0.16)',
    background: 'var(--center-channel-bg)',
    color: 'var(--center-channel-color)',
};

export const avatar: React.CSSProperties = {
    width: 72,
    height: 72,
    borderRadius: '50%',
    objectFit: 'cover',
};

export const smallAvatar: React.CSSProperties = {
    width: 24,
    height: 24,
    borderRadius: '50%',
    objectFit: 'cover',
};

export const chip: React.CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    padding: '2px 4px 2px 2px',
    margin: '0 6px 6px 0',
    borderRadius: 12,
    background: 'rgba(var(--center-channel-color-rgb), 0.08)',
    fontSize: 13,
};

export const chipRemove: React.CSSProperties = {
    border: 'none',
    background: 'transparent',
    color: 'var(--center-channel-color)',
    opacity: 0.56,
    cursor: 'pointer',
    padding: '0 4px',
    fontSize: 14,
};

export const resultRow: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    width: '100%',
    padding: '6px 8px',
    border: 'none',
    background: 'transparent',
    color: 'var(--center-channel-color)',
    textAlign: 'left',
    cursor: 'pointer',
    borderRadius: 4,
    fontSize: 14,
};

export const radioLabel: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    fontSize: 14,
    fontWeight: 'normal',
    margin: '6px 0',
    cursor: 'pointer',
};
