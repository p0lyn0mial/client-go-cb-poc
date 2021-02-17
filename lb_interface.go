package main

// LoadBalancer defines an interface that is used by the client-go's custom transport
// for getting a target server for each request
type LoadBalancer interface {
	// NextServer() knows how to return a target server for the current request
	NextServer() (string, error)

	// Servers returns all registered and healthy servers known by a LB
	Servers() []string
}

// ServersHealthMonitorProvider defines an interface that is used by the load balancer to discover healthy and unhealthy servers
type ServersHealthMonitorProvider interface {
	// Targets when called returns a list of healthy and unhealthy servers
	Targets() (healthy []string, unhealthy []string)
}
