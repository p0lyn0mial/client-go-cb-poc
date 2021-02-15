package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"k8s.io/client-go/rest"
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

	targetProvider := StaticTargetProvider{"10.0.139.201:6443", "10.0.154.83:6443", "10.0.165.154:6443"}
	fmt.Println(fmt.Sprintf("creating and starting the health monitor for %v", targetProvider))
	// TODO: copy the config and set a separate userAgent
	hm, err := NewHealthMonitor(targetProvider, createConfigForHealthMonitor(config))
	if err != nil {
		panic(err)
	}
	go hm.StartMonitoring(context.TODO().Done())

	config.Wrap(newCustomTransportWrapper(func() []string {
		return hm.HealthyTargets()
	}))

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

func newCustomTransportWrapper(resolveKubeAPIServersFn func() []string) func(http.RoundTripper) http.RoundTripper {
	return func(rt http.RoundTripper) http.RoundTripper {
		return &customTransport{
			baseRT:                  rt,
			resolveKubeAPIServersFn: resolveKubeAPIServersFn,
		}
	}

}

type customTransport struct {
	baseRT                  http.RoundTripper
	resolveKubeAPIServersFn func() []string
}

func (t *customTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	fmt.Println(fmt.Sprintf("customTransport: RoundTrip: received a request for Method = %q to Host = %q, URL = %q", r.Method, r.Host, r.URL))

	// TODO: this should be replaced by calling the LB only
	//       The LB should be notified about healthy/unhealthy targets and pick the best for the current request
	//
	// TODO: we need a way of waiting for the targets to become healthy
	//
	// TODO: if there are no healthy targets we could use the service IP - why not ?
	//
	kubeAPIServers := t.resolveKubeAPIServersFn()
	if len(kubeAPIServers) == 0 {
		return nil, errors.New("service unavailable")
	}

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

// pickKubernetesServer is a load balancer that picks a target
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
