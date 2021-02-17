package main

import (
	"errors"
	"fmt"
	"github.com/vulcand/oxy/roundrobin"
	"net/url"
)

type OxyRoundRobingShim struct {
	target *roundrobin.RoundRobin
}

var _ LoadBalancer = &OxyRoundRobingShim{}

// NewOxyRoundRobinLoadBalancer is a shim for oxy's load balancer.
// Oxy implementation provides a dynamic weighted round robin load balancer.
//
// This method returns an object that conforms to the interface we want.
func NewOxyRoundRobinLoadBalancer() (*OxyRoundRobingShim, error) {
	oxyLB, err := roundrobin.New(nil)
	if err != nil {
		return nil, err
	}

	lb := &OxyRoundRobingShim{
		target: oxyLB,
	}

	return lb, nil
}

// NextServer simply returns the next healthy server for the current request
func (lb *OxyRoundRobingShim) NextServer() (string, error) {
	url, err := lb.target.NextServer()
	if err != nil {
		return "", err
	}
	if url == nil {
		return "", errors.New("service unavailable")
	}
	return url.Host, nil
}

// Servers returns all registered and healthy servers known by this LB
func (lb *OxyRoundRobingShim) Servers() []string {
	servers := lb.target.Servers()
	ret := make([]string, 0, len(servers))

	for _, server := range servers {
		ret = append(ret, server.Host)
	}

	return ret
}

// This method is used by the HealthMonitor to notify that the list of targets has changed
// TODO: I am going to rework this bit, leaving it for demonstration
func (lb *OxyRoundRobingShim) Rebalance(healthyServers []string, unhealthyServers []string) {
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
