package main

import (
	"context"
	"flag"
	"fmt"
	"k8s.io/client-go/rest"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/config/helpers"
)

func main() {
	fmt.Println("starting the app")

	var kubeConfig string
	flag.StringVar(&kubeConfig, "kubeconfig", "", "")
	flag.Parse()

	config, err := helpers.GetKubeConfigOrInClusterConfig(kubeConfig, configv1.ClientConnectionOverrides{})
	if err != nil {
		panic(err.Error())
	}
	config.Timeout = 3 * time.Second
	config.QPS = 1
	config.Burst = 1

	config.TLSClientConfig.ServerName = "kubernetes.default.svc"

	targetProvider := StaticTargetProvider{"10.0.139.201:6443", "10.0.154.83:6443", "10.0.165.154:6443"}
	fmt.Println(fmt.Sprintf("creating and starting the health monitor for %v", targetProvider))
	// TODO: copy the config and set a separate userAgent
	hm, err := NewHealthMonitor(targetProvider, createConfigForHealthMonitor(config))
	if err != nil {
		panic(err)
	}
	go hm.StartMonitoring(context.TODO().Done())

	loadBalancer, err := NewOxyRoundRobinLoadBalancer(hm)
	if err != nil {
		panic(err)
	}

	config.Wrap(newCustomTransportWrapper(loadBalancer))

	fmt.Println("creating the k8s client set for the config")
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for {
		fmt.Println("about to LIST secrets in the test-01 namespace")
		ret, err := clientset.CoreV1().Secrets("test-01").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			fmt.Println(fmt.Sprintf("error while listing secrets, err = %v", err))
		}
		fmt.Println(fmt.Sprintf("found %d secrets in the default namespace", len(ret.Items)))
		fmt.Println("")
		time.Sleep(35 * time.Second)
	}
}

func createConfigForHealthMonitor(restConfig *rest.Config) *rest.Config {
	restConfigCopy := *restConfig
	restConfigCopy.UserAgent = "HealthMonitor" // find a way that would help identify the client
	restConfigCopy.TLSClientConfig.ServerName = "kubernetes.default.svc"

	// doesn't matter since we are not using client-go only the TLS config
	restConfigCopy.QPS = 1
	restConfigCopy.Burst = 1

	return &restConfigCopy
}

func newCustomTransportWrapper(lb LoadBalancer) func(http.RoundTripper) http.RoundTripper {
	return func(rt http.RoundTripper) http.RoundTripper { return &customTransport{baseRT: rt, lb: lb} }
}

type customTransport struct {
	baseRT http.RoundTripper
	lb     LoadBalancer
}

func (t *customTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	fmt.Println(fmt.Sprintf("customTransport: RoundTrip: received a request for Method = %q to Host = %q, URL = %q", r.Method, r.Host, r.URL))

	// TODO: if there are no healthy targets we could use the service IP - why not ?
	server, err := t.lb.NextServer()
	if err != nil {
		return nil, err
	}

	// I don't like modifying the original request
	r.Host = server
	r.URL.Host = server

	fmt.Println(fmt.Sprintf("customTransport: RoundTrip: The new Host is = %q", r.Host))

	// TODO: needs proper implementation
	rt := getCircuitBreakerForEndpoint(server, t.baseRT)
	return rt.RoundTrip(r)
}

// getCircuitBreakerForEnpoint could map an endpoint (URL) to a circuit breaker aware endpoint
// the mapper could have a cache (LRU) to store the mapping
func getCircuitBreakerForEndpoint(ep string, delegate http.RoundTripper) http.RoundTripper {
	return &cb{delegate: delegate}
}

type cb struct {
	delegate http.RoundTripper
}

// the CB aware endpoint could:
//  - be safe for concurrent use just like http.Client is
//  - implement RoundTrip interface
//  - analyse err
//  - analyse rsp (for example HTTP 500)
//  - analyse latency
//  - update its internal state
//  - notify the load balancer
func (c *cb) RoundTrip(r *http.Request) (*http.Response, error) {
	fmt.Println(fmt.Sprintf("circuitBreaker: RoundTrip: received a request for Method = %q to Host = %q, URL = %q", r.Method, r.Host, r.URL))
	defer fmt.Println(fmt.Sprintf("circuitBreaker: RoundTrip ended for Host = %q", r.Host))
	return c.delegate.RoundTrip(r)
}
