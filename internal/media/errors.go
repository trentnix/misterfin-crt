package media

import "errors"

// ErrUnauthorized identifies explicit credential rejection. Transport failures
// must not match it, because temporary failures must retain saved sign-in.
var ErrUnauthorized = errors.New("server rejected authentication")
