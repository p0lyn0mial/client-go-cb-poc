package main

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/vulcand/oxy/roundrobin"
)

type oxyRoundRobingShim struct {
	target *roundrobin.RoundRobin

	hmp ServersHealthMonitorProvider
}

var _ LoadBalancer = &oxyRoundRobingShim{}

// NewOxyRoundRobinLoadBalancer is a shim for oxy's load balancer.
// Oxy implementation provides a dynamic weighted round robin load balancer.
//
// This method returns an object that conforms to the interface we want.
// And knows how to use the health monitor for getting the list of healthy/unhealthy servers.
func NewOxyRoundRobinLoadBalancer(healthMonitorProvider ServersHealthMonitorProvider) (LoadBalancer, error) {
	oxyLB, err := roundrobin.New(nil)
	if err != nil {
		return nil, err
	}

	lb := &oxyRoundRobingShim{
		target: oxyLB,
		hmp: healthMonitorProvider,
	}
	lb.hmp.AddListener(lb)

	return lb, nil
}

// NextServer simply returns the next healthy server for the current request
func (lb *oxyRoundRobingShim) NextServer() (string, error) {
	url, err := lb.target.NextServer()
	if err != nil {
		return "", err
	}
	if url == nil {
		return "", errors.New("service unavailable")
	}
	return url.Host, nil
}

// This method is used by the HealthMonitor to notify that the list of targets has changed
// TODO: I am going to rework this bit, leaving it for demonstration
func (lb *oxyRoundRobingShim) Enqueue() {
	healthyServers, unhealthyServers := lb.hmp.Targets()

	// for now assume HTTPS scheme
	toURLFn := func(server string) (*url.URL, error) {
		return url.Parse(fmt.Sprintf("https://%v", server))
	}

	// TODO: rework so that it won't panic
	for _, unhealthyServer := range unhealthyServers {
		u, e := toURLFn(unhealthyServer)
		if e != nil {
			panic(e)
		}
		if err := lb.target.RemoveServer(u); err != nil {
			panic(err)
		}
	}

	for _, healthyServers := range healthyServers {
		u, e := toURLFn(healthyServers)
		if e != nil {
			panic(e)
		}
		if err := lb.target.UpsertServer(u); err != nil {
			panic(err)
		}
	}
}
