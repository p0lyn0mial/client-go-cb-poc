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
	"k8s.io/klog/v2"
)

type healthMonitor struct {
	// targetProvider provides a list of targets to monitor
	// it also can schedule refreshing the list by simply calling Enqueue method
	targetProvider TargetProvider

	// client is an HTTP client that is used to probe health checks for targets
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

	refreshTargetsLock sync.Mutex
	refreshTargets     bool
}

func NewHealthMonitor(targetProvider TargetProvider, restConfig *rest.Config) (*healthMonitor, error) {
	probeResponseTimeout := 5 * time.Second

	client, err := createHealthCheckHTTPClient(probeResponseTimeout, restConfig)
	if err != nil {
		return nil, err
	}

	return &healthMonitor{
		client:                   client,
		targetProvider:           targetProvider,
		targetsToMonitor:         targetProvider.CurrentTargetsList(),
		probeResponseTimeout:     probeResponseTimeout,
		probeInterval:            1 * time.Second,
		unhealthyProbesThreshold: 2,
		healthyProbesThreshold:   5,

		consecutiveSuccessfulProbes: map[string]int{},
		consecutiveFailedProbes:     map[string][]error{},
	}, nil
}

// StartMonitoring starts monitoring the provided targets until stop channel is closed
func (sm *healthMonitor) StartMonitoring(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()

	klog.Infof("Starting the health monitor with Interval = %v, Timeout = %v, HealthyThreshold = %v, UnhealthyThreshold = %v ", sm.probeInterval, sm.probeResponseTimeout, sm.healthyProbesThreshold, sm.unhealthyProbesThreshold)
	defer klog.Info("Shutting down the health monitor")

	wait.Until(sm.healthCheckRegisteredTargets, sm.probeInterval, stopCh)
}

// Enqueue schedules refreshing the target list on the next probeInterval
// This method is used by the TargetProvider to notify that the list has changed
func (sm *healthMonitor) Enqueue() {
	sm.refreshTargetsLock.Lock()
	defer sm.refreshTargetsLock.Unlock()
	sm.refreshTargets = true
}

type targetErrTuple struct {
	target string
	err    error
}

// refreshTargetsLocked updates the internal targets list to monitor if it was requested (via the Enqueue method)
func (sm *healthMonitor) refreshTargetsLocked() {
	sm.refreshTargetsLock.Lock()
	defer sm.refreshTargetsLock.Unlock()
	if !sm.refreshTargets {
		return
	}

	sm.refreshTargets = false
	freshTargets := sm.targetProvider.CurrentTargetsList()
	freshTargetSet := sets.NewString(freshTargets...)

	currentTargetsSet := sets.NewString(sm.targetsToMonitor...)
	newTargetsToMonitorSet := freshTargetSet.Difference(currentTargetsSet)
	if newTargetsToMonitorSet.Len() > 0 {
		klog.Infof("health monitor observed new targets = %v", newTargetsToMonitorSet.List())
	}

	removedTargetsToMonitorSet := currentTargetsSet.Difference(freshTargetSet)
	if removedTargetsToMonitorSet.Len() > 0 {
		klog.Infof("health monitor will stop checking the following targets targets = %v", removedTargetsToMonitorSet.List())
		for targetToRemove, _ := range removedTargetsToMonitorSet {
			delete(sm.consecutiveSuccessfulProbes, targetToRemove)
			delete(sm.consecutiveFailedProbes, targetToRemove)
		}

		healthyTargetsSet := sets.NewString(sm.healthyTargets...)
		healthyTargetsSet.Delete(removedTargetsToMonitorSet.List()...)
		sm.healthyTargets = healthyTargetsSet.List()

		unhealthyTargetsSet := sets.NewString(sm.unhealthyTargets...)
		unhealthyTargetsSet.Delete(removedTargetsToMonitorSet.List()...)
		sm.unhealthyTargets = unhealthyTargetsSet.List()
	}

	sm.targetsToMonitor = freshTargets
}

func (sm *healthMonitor) healthCheckRegisteredTargets() {
	sm.refreshTargetsLocked()
	var wg sync.WaitGroup
	resTargetErrTupleCh := make(chan targetErrTuple, len(sm.targetsToMonitor))

	for i := 0; i < len(sm.targetsToMonitor); i++ {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			err := sm.healthCheckSingleTarget(target)
			resTargetErrTupleCh <- targetErrTuple{target, err}
		}(sm.targetsToMonitor[i])
	}
	wg.Wait()
	close(resTargetErrTupleCh)

	currentHealthCheckProbes := make([]targetErrTuple, 0, len(sm.targetsToMonitor))
	for svrErrTuple := range resTargetErrTupleCh {
		currentHealthCheckProbes = append(currentHealthCheckProbes, svrErrTuple)
	}

	sm.updateHealthChecksFor(currentHealthCheckProbes)
}

// TODO: add metrics
// updateHealthChecksFor examines the health of targets based on the provided probes and the current configuration.
// It also notifies interested parties about changes in the health condition.
// Interested parties can be registered by calling AddListener method.
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
	currentlyUnhealthyTargetsSet.Delete(newHealthyTargetsSet.List()...)
	if !currentlyUnhealthyTargetsSet.Equal(previouslyUnhealthyTargetsSet) {
		// TODO: notify about new unhealthy targets
		sm.unhealthyTargets = currentlyUnhealthyTargetsSet.List()
		klog.Infof("observed the following unhealthy targets %v", sm.unhealthyTargets)
	}

	// detect healthy targets
	previouslyHealthyTargetsSet := sets.NewString(sm.healthyTargets...)
	currentlyHealthyTargetsSet := previouslyHealthyTargetsSet.Union(newHealthyTargetsSet)
	currentlyHealthyTargetsSet.Delete(newUnhealthyTargetsSet.List()...)
	if !currentlyHealthyTargetsSet.Equal(previouslyHealthyTargetsSet) {
		// TODO: notify about new healthy servers
		sm.healthyTargets = currentlyHealthyTargetsSet.List()
		klog.Infof("observed the following healthy targets %v", sm.healthyTargets)
	}
}

func (sm *healthMonitor) healthCheckSingleTarget(target string) error {
	// TODO: make the protocol, port and the path configurable
	url := fmt.Sprintf("https://%s/%s", target, "readyz")
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
