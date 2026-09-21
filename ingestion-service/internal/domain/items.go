package domain

import "errors"

// ErrInvalidItemFilter is returned for an unknown todo status or reminder
// window on the items list endpoints.
var ErrInvalidItemFilter = errors.New("invalid item filter")
