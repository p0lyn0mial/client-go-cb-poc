package main

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestHealthProbes(t *testing.T) {
	target := &healthMonitor{
		targetsToMonitor:         []string{"master-0", "master-1", "master-2"},
		unhealthyProbesThreshold: 2,
		healthyProbesThreshold:   3,

		consecutiveSuccessfulProbes: map[string]int{},
		consecutiveFailedProbes:     map[string][]error{},
	}

	createHealthyProbe := func(server string) targetErrTuple {
		return targetErrTuple{server, nil}
	}

	createUnHealthyProbe := func(server string) targetErrTuple {
		return targetErrTuple{server, errors.New("random error")}
	}

	scenarios := []struct {
		name                     string
		currentHealthProbes      []targetErrTuple
		expectedHealthyServers   []string
		expectedUnhealthyServers []string
	}{
		{
			name:                "round 1: all servers passed probe",
			currentHealthProbes: []targetErrTuple{createHealthyProbe("master-0"), createHealthyProbe("master-1"), createHealthyProbe("master-2")},
		},

		{
			name:                "round 2: all servers passed probe",
			currentHealthProbes: []targetErrTuple{createHealthyProbe("master-0"), createHealthyProbe("master-1"), createHealthyProbe("master-2")},
		},

		{
			name:                   "round 3: all servers became healthy",
			currentHealthProbes:    []targetErrTuple{createHealthyProbe("master-0"), createHealthyProbe("master-1"), createHealthyProbe("master-2")},
			expectedHealthyServers: []string{"master-0", "master-1", "master-2"},
		},

		{
			name:                   "round 4: all servers passed probe, nothing has changed",
			currentHealthProbes:    []targetErrTuple{createHealthyProbe("master-0"), createHealthyProbe("master-1"), createHealthyProbe("master-2")},
			expectedHealthyServers: []string{"master-0", "master-1", "master-2"},
		},

		{
			name:                   "round 5: master-1 failed probe",
			currentHealthProbes:    []targetErrTuple{createHealthyProbe("master-0"), createUnHealthyProbe("master-1"), createHealthyProbe("master-2")},
			expectedHealthyServers: []string{"master-0", "master-1", "master-2"},
		},

		{
			name:                     "round 6: master-1 became unhealthy",
			currentHealthProbes:      []targetErrTuple{createHealthyProbe("master-0"), createUnHealthyProbe("master-1"), createHealthyProbe("master-2")},
			expectedHealthyServers:   []string{"master-0", "master-2"},
			expectedUnhealthyServers: []string{"master-1"},
		},

		{
			name:                     "round 7: master-1 passed probe",
			currentHealthProbes:      []targetErrTuple{createHealthyProbe("master-0"), createHealthyProbe("master-1"), createHealthyProbe("master-2")},
			expectedHealthyServers:   []string{"master-0", "master-2"},
			expectedUnhealthyServers: []string{"master-1"},
		},

		{
			name:                     "round 8: master-1 passed probe",
			currentHealthProbes:      []targetErrTuple{createHealthyProbe("master-0"), createHealthyProbe("master-1"), createHealthyProbe("master-2")},
			expectedHealthyServers:   []string{"master-0", "master-2"},
			expectedUnhealthyServers: []string{"master-1"},
		},

		{
			name:                     "round 9: master-1 became healthy",
			currentHealthProbes:      []targetErrTuple{createHealthyProbe("master-0"), createHealthyProbe("master-1"), createHealthyProbe("master-2")},
			expectedHealthyServers:   []string{"master-0", "master-1", "master-2"},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// act
			target.updateHealthChecksFor(scenario.currentHealthProbes)

			// validate
			if !cmp.Equal(target.healthyTargets, scenario.expectedHealthyServers, cmpopts.EquateEmpty()) {
				t.Errorf("unexpected list of healthy servers = %v, expected = %v", target.healthyTargets, scenario.expectedHealthyServers)
			}
			if !cmp.Equal(target.unhealthyTargets, scenario.expectedUnhealthyServers, cmpopts.EquateEmpty()) {
				t.Errorf("unexpected list of unhealthy servers = %v, expected = %v", target.unhealthyTargets, scenario.expectedUnhealthyServers)
			}
		})
	}
}
