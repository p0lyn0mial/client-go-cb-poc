package main

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"net/http"
	"testing"
)

type scenarioRT struct {
	code int
	err  error
}

func (srt *scenarioRT) RoundTrip(_ *http.Request) (*http.Response, error) {
	rsp := &http.Response{}
	rsp.StatusCode = srt.code
	return rsp, srt.err
}

func roundTripperFor(httpStatusCode int, err error) http.RoundTripper {
	return &scenarioRT{code: httpStatusCode, err: err}
}

func TestOxyCircuitBreakerFallback(t *testing.T) {
	target, err := NewOxyCircuitBreaker("ResponseCodeRatio(500, 600, 0, 600) > 0.1")
	if err != nil {
		t.Fatal(err)
	}

	scenarios := []struct {
		name             string
		rt               http.RoundTripper
		expectedErr      error
		expectedResponse *http.Response
	}{
		{
			name:             "round 1:",
			rt:               roundTripperFor(http.StatusInternalServerError, nil),
			expectedResponse: &http.Response{StatusCode: http.StatusInternalServerError},
		},

		{
			name:             "round 2:",
			rt:               roundTripperFor(http.StatusInternalServerError, nil),
			expectedResponse: &http.Response{StatusCode: http.StatusInternalServerError},
		},

		{
			name:             "round 3:",
			rt:               roundTripperFor(http.StatusInternalServerError, nil),
			expectedResponse: &http.Response{StatusCode: http.StatusInternalServerError},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// act
			actualRsp, actualErr := target.RoundTripWithDelegate(nil, scenario.rt)

			// validate
			if !cmp.Equal(actualRsp, scenario.expectedResponse, cmpopts.EquateEmpty()) {
				t.Errorf("unexpected HTTP response returned = %v, expected = %v", actualRsp, scenario.expectedResponse)
			}

			if !cmp.Equal(actualErr, scenario.expectedErr, cmpopts.EquateEmpty()) {
				t.Errorf("unexpected error returned = %v, expected = %v", actualErr, scenario.expectedErr)
			}
		})
	}
}
