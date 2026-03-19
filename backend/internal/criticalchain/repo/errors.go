package repo

import "errors"

var (
	errNotFound   = errors.New("criticalchain: not found")
	errInvalidID  = errors.New("criticalchain: invalid id")
)
