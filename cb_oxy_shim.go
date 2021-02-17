package main

import (
	"net/http"

	"github.com/vulcand/oxy/cbreaker"
)

type OxyCircuitBreakerShim struct {
	impl *cbreaker.CircuitBreaker
}

func NewOxyCircuitBreakerWithOptions(expression string) func() (*OxyCircuitBreakerShim, error) {
	return func() (*OxyCircuitBreakerShim, error) {
		return NewOxyCircuitBreaker(expression)
	}
}

func NewOxyCircuitBreaker(expression string) (*OxyCircuitBreakerShim, error) {
	cb := &OxyCircuitBreakerShim{}

	implCB, err := cbreaker.NewRT(expression, cbreaker.OnTripped(cb))
	if err != nil {
		return nil, err
	}

	cb.impl = implCB

	return cb, nil
}

func (cb *OxyCircuitBreakerShim) FailedBecauseCircuitBreakerIsOpen(err error) bool {
	// this is the error that is set by the cb when it is in open state
	return err == cbreaker.SentinelErr
}

func (cb *OxyCircuitBreakerShim) RoundTripWithDelegate(r *http.Request, delegate http.RoundTripper) (*http.Response, error) {
	return cb.impl.RoundTripWithDelegate(r, delegate)
}

// implements cbreaker.SideEffect interface
// it is called when the CB enters the Open state
func (cb *OxyCircuitBreakerShim) Exec() error {
	// TODO: add counter, for counting how many time the CB was open
	return nil
}
