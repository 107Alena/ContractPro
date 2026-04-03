package circuitbreaker

import "errors"

// ErrCircuitOpen is returned when the circuit breaker is open and the
// request is rejected without reaching the underlying storage.
var ErrCircuitOpen = errors.New("circuit breaker is open: object storage unavailable")
