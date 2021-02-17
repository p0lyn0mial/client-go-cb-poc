package main

import "net/http"

// CircuitBreaker defines an interface that is used by the client-go's custom transport for calling a remote server
type CircuitBreaker interface {
	//
	FailedBecauseCircuitBreakerIsOpen(err error) bool

	//
	RoundTripWithDelegate(r *http.Request, delegate http.RoundTripper) (*http.Response, error)
}
