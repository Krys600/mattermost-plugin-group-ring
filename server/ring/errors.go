package ring

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrorCode identifies a user-facing failure. Each code has a translation in the i18n catalog
// under "ring.error.<code>".
type ErrorCode string

const (
	ErrDisabled          ErrorCode = "disabled"
	ErrChannelNotFound   ErrorCode = "channel_not_found"
	ErrChannelType       ErrorCode = "channel_type_not_allowed"
	ErrNotChannelMember  ErrorCode = "not_channel_member"
	ErrNoPostPermission  ErrorCode = "no_post_permission"
	ErrRoleNotAllowed    ErrorCode = "role_not_allowed"
	ErrCallsUnavailable  ErrorCode = "calls_unavailable"
	ErrCallsTestMode     ErrorCode = "calls_test_mode"
	ErrGroupCallsBlocked ErrorCode = "group_calls_not_allowed"
	ErrNoRecipients      ErrorCode = "no_recipients"
	ErrTooManyRecipients ErrorCode = "too_many_recipients"
	ErrRateLimited       ErrorCode = "rate_limited"
	ErrRingNotFound      ErrorCode = "ring_not_found"
	ErrNotRecipient      ErrorCode = "not_recipient"
	ErrNotCaller         ErrorCode = "not_caller"
	ErrInvalidRequest    ErrorCode = "invalid_request"
	ErrMixedTargets      ErrorCode = "mixed_targets"
	ErrInternal          ErrorCode = "internal"
)

// Error is a typed, user-facing error. Params feed the translated message.
type Error struct {
	Code   ErrorCode
	Params map[string]any
	// Skipped carries the skipped users for ErrNoRecipients, so callers can explain why.
	Skipped []Skipped
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.cause)
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	return e.cause
}

// HTTPStatus maps the error to an HTTP status code.
func (e *Error) HTTPStatus() int {
	switch e.Code {
	case ErrChannelNotFound, ErrRingNotFound:
		return http.StatusNotFound
	case ErrNotChannelMember, ErrNoPostPermission, ErrRoleNotAllowed, ErrNotRecipient, ErrNotCaller, ErrChannelType, ErrDisabled, ErrCallsTestMode, ErrGroupCallsBlocked:
		return http.StatusForbidden
	case ErrRateLimited:
		return http.StatusTooManyRequests
	case ErrCallsUnavailable:
		return http.StatusServiceUnavailable
	case ErrInternal:
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

func newError(code ErrorCode, params map[string]any) *Error {
	return &Error{Code: code, Params: params}
}

func internalError(cause error) *Error {
	return &Error{Code: ErrInternal, cause: cause}
}

// AsError extracts a *Error from err, wrapping unknown errors as internal errors.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return internalError(err)
}
