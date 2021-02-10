package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
)

type healthMonitor struct {
	client *http.Client

	// probeResponseTimeout specifies a time limit for requests made by the HTTP client for the health check
	probeResponseTimeout time.Duration

	// probeInterval specifies a time interval at which health checks are send
	probeInterval time.Duration

	// unhealthyProbesThreshold specifies consecutive failed health checks after which a target is considered unhealthy
	unhealthyProbesThreshold int

	// healthyProbesThreshold  specifies consecutive successful health checks after which a target is considered healthy
	healthyProbesThreshold int

	healthyTargets   []string
	unhealthyTargets []string
	targetsToMonitor []string

	consecutiveSuccessfulProbes map[string]int
	consecutiveFailedProbes     map[string][]error
}

// TODO: move serverToMonitor to a provider that can return a dynamic list
func NewHealthMonitor(serversToMonitor []string, restConfig *rest.Config) (*healthMonitor, error) {
	probeResponseTimeout := 5 * time.Second

	client, err := createHealthCheckHTTPClient(probeResponseTimeout, restConfig)
	if err != nil {
		return nil, err
	}

	return &healthMonitor{
		targetsToMonitor:         serversToMonitor,
		client:                   client,
		probeResponseTimeout:     probeResponseTimeout,
		probeInterval:            10 * time.Second,
		unhealthyProbesThreshold: 2,
		healthyProbesThreshold:   5,

		consecutiveSuccessfulProbes: map[string]int{},
		consecutiveFailedProbes:     map[string][]error{},
	}, nil
}

func (sm *healthMonitor) StartMonitoring(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()

	// TODO: add logging

	go wait.Until(sm.healthCheckRegisteredTargets, sm.probeInterval, stopCh)
}

type targetErrTuple struct {
	target string
	err    error
}

func (sm *healthMonitor) healthCheckRegisteredTargets() {
	// TODO: check if the list of servers to monitors has changed
	var wg sync.WaitGroup
	resTargetErrTupleCh := make(chan targetErrTuple, len(sm.targetsToMonitor))

	for i := 0; i < len(sm.targetsToMonitor); i++ {
		wg.Add(1)
		go func(server string) {
			defer wg.Done()
			if err := sm.healthCheckSingleTarget(server); err != nil {
				resTargetErrTupleCh <- targetErrTuple{server, err}
			}
		}(sm.targetsToMonitor[i])
	}
	wg.Wait()

	currentHealthCheckProbes := make([]targetErrTuple, 0, len(sm.targetsToMonitor))
	for svrErrTuple := range resTargetErrTupleCh {
		currentHealthCheckProbes = append(currentHealthCheckProbes, svrErrTuple)
	}

	sm.updateHealthChecksFor(currentHealthCheckProbes)
}

func (sm *healthMonitor) updateHealthChecksFor(currentHealthCheckProbes []targetErrTuple) {
	newUnhealthyTargets := []string{}
	newHealthyTargets := []string{}

	for _, svrErrTuple := range currentHealthCheckProbes {
		if svrErrTuple.err != nil {
			delete(sm.consecutiveSuccessfulProbes, svrErrTuple.target)

			unhealthyProbesSlice := sm.consecutiveFailedProbes[svrErrTuple.target]
			if len(unhealthyProbesSlice) < sm.unhealthyProbesThreshold {
				unhealthyProbesSlice = append(unhealthyProbesSlice, svrErrTuple.err)
				sm.consecutiveFailedProbes[svrErrTuple.target] = unhealthyProbesSlice
				if len(unhealthyProbesSlice) == sm.unhealthyProbesThreshold {
					newUnhealthyTargets = append(newUnhealthyTargets, svrErrTuple.target)
				}
			}
			continue
		}

		delete(sm.consecutiveFailedProbes, svrErrTuple.target)

		healthyProbesCounter := sm.consecutiveSuccessfulProbes[svrErrTuple.target]
		if healthyProbesCounter < sm.healthyProbesThreshold {
			healthyProbesCounter++
			sm.consecutiveSuccessfulProbes[svrErrTuple.target] = healthyProbesCounter
			if healthyProbesCounter == sm.healthyProbesThreshold {
				newHealthyTargets = append(newHealthyTargets, svrErrTuple.target)
			}
		}
	}

	newUnhealthyTargetsSet := sets.NewString(newUnhealthyTargets...)
	newHealthyTargetsSet := sets.NewString(newHealthyTargets...)

	// detect unhealthy targets
	previouslyUnhealthyTargetsSet := sets.NewString(sm.unhealthyTargets...)
	currentlyUnhealthyTargetsSet := previouslyUnhealthyTargetsSet.Union(newUnhealthyTargetsSet)
	currentlyUnhealthyTargetsSet = currentlyUnhealthyTargetsSet.Delete(newHealthyTargetsSet.List()...)
	if !currentlyUnhealthyTargetsSet.Equal(previouslyUnhealthyTargetsSet) {
		// TODO: notify about new unhealthy targets
		// TODO: add metrics
		sm.unhealthyTargets = currentlyUnhealthyTargetsSet.List()
	}

	// detect healthy targets
	previouslyHealthyTargetsSet := sets.NewString(sm.healthyTargets...)
	currentlyHealthyTargetsSet := previouslyHealthyTargetsSet.Union(newHealthyTargetsSet)
	currentlyHealthyTargetsSet = currentlyHealthyTargetsSet.Delete(newUnhealthyTargetsSet.List()...)
	if !currentlyHealthyTargetsSet.Equal(previouslyHealthyTargetsSet) {
		// TODO: notify about new healthy servers
		// TODO: add metrics
		sm.healthyTargets = currentlyHealthyTargetsSet.List()
	}
}

func (sm *healthMonitor) healthCheckSingleTarget(target string) error {
	// TODO: make the protocol, port and the path configurable
	url := fmt.Sprintf("https://%s/%s", target, "/readyz")
	newReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := sm.client.Do(newReq)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status from %v: %v, expected HTTP 200", url, resp.StatusCode)
	}

	return err
}

func createHealthCheckHTTPClient(responseTimeout time.Duration, restConfig *rest.Config) (*http.Client, error) {
	transportConfig, err := restConfig.TransportConfig()
	if err != nil {
		return nil, err
	}

	tlsConfig, err := transport.TLSConfigFor(transportConfig)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: utilnet.SetTransportDefaults(&http.Transport{
			TLSClientConfig: tlsConfig,
		}),
		Timeout: responseTimeout,
	}

	return client, nil
}
