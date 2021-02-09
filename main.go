package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
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
	config.Wrap(newCustomTransport)

	fmt.Println("creating the k8s client set for the config\n")
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
	}
}

func newCustomTransport(rt http.RoundTripper) http.RoundTripper {
	return &customTransport{baseRT: rt}
}

type customTransport struct {
	baseRT http.RoundTripper
}

func (t *customTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	fmt.Println(fmt.Sprintf("customTransport: RoundTrip: received a request for Method = %q to Host = %q, URL = %q", r.Method, r.Host, r.URL))

	// TODO: needs proper implementation
	kubeAPIServers := resolveKubeAPIServers()

	// TODO: needs proper implementation
	host := pickKubernetesServer(nil, kubeAPIServers)


	// I don't like modifying the original request
	r.Host = host
	r.URL.Host = host

	fmt.Println(fmt.Sprintf("customTransport: RoundTrip: The new Host is = %q", r.Host))

	// TODO: needs proper implementation
	rt := getCircuitBreakerForEndpoint(host, t.baseRT)
	return rt.RoundTrip(r)
}

// resolveKubeAPIServers knows how to get a list of IPs for Kubernetes API
//
// There are at least a few possible implementations
//
// 1: we get a static list from an operator via flags or config
//
// 2: we use a service IP to monitor endpoints for the default kubernetes service
//
// 3: we use a configmap that is automatically updated by kubelet
//
// regardless of the implementation the service resolver should monitor /readyz and notify dependants (for example a load balancer)
func resolveKubeAPIServers() []string {
	return []string{
		"10.0.139.143:6443",
		"10.0.153.114:6443",
		"10.0.162.144:6443",
	}
}

// pickKubernetesServer is a load balancer that picks a server
// for the current request, it could:
//
// - exclude already seen endpoints (servers) for the current request (support for retries)
// - take into account weight of endpoints
// - support an ep removal
//     the service resolver could notify it when an ep fails /readyz
//     the circuit breaker could notify it when a certain failure threshold has been reached
//     the circuit breaker could notify when the weight of an ep decreased
func pickKubernetesServer(seen []string, servers []string) string {
	return servers[rand.Intn(len(servers))]
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