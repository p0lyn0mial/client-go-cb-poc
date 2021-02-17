package main

import (
	"fmt"
	"sync"
)

type ServersHealthMonitorProviderNotifier interface {
	ServersHealthMonitorProvider
	Notifier
}

type OxyCircuitBreakerFactory struct {
	lb      *OxyRoundRobingShim
	newCBFn func() (*OxyCircuitBreakerShim, error)
	hm      ServersHealthMonitorProviderNotifier

	// TODO: use cow technique instead
	store     map[string]*OxyCircuitBreakerShim
	storeLock sync.RWMutex
}

// knows how to use the health monitor for getting the list of healthy/unhealthy servers.
func NewOxyCircuitBreakerFactory(hm ServersHealthMonitorProviderNotifier, lb *OxyRoundRobingShim, newCBFn func() (*OxyCircuitBreakerShim, error)) *OxyCircuitBreakerFactory {
	f := &OxyCircuitBreakerFactory{
		lb:        lb,
		newCBFn:   newCBFn,
		hm:        hm,
		store:     map[string]*OxyCircuitBreakerShim{},
		storeLock: sync.RWMutex{},
	}

	hm.AddListener(f)

	return f
}

func (f *OxyCircuitBreakerFactory) Get(server string) CircuitBreaker {
	f.storeLock.RLock()
	defer f.storeLock.RUnlock()
	cb, exists := f.store[server]
	if !exists {
		// should not happen ?!
		// cb are created in Enqueue method, before they are even added to the LB
		panic(fmt.Sprintf("no cb found for %v", server))
	}
	return cb
}

func (f *OxyCircuitBreakerFactory) Enqueue() {
	f.storeLock.Lock()
	defer f.storeLock.Unlock()

	// register new CB in the global cache
	healthyServers, unhealthyServers := f.hm.Targets()

	for _, healthyServer := range healthyServers {
		if _, exists := f.store[healthyServer]; !exists {
			cb, err := f.newCBFn()
			if err != nil {
				panic(err)
			}
			f.store[healthyServer] = cb
		}
	}

	// notify the lb about new healthy/unhealthy servers
	f.lb.Rebalance(healthyServers, unhealthyServers)
}
