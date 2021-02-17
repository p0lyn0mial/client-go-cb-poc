package main

import (
	"context"
	"flag"
	"fmt"
	"k8s.io/apimachinery/pkg/util/sets"
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
	config.QPS = 100
	config.Burst = 1000

	config.TLSClientConfig.ServerName = "kubernetes.default.svc"

	targetProvider := StaticTargetProvider{"10.0.139.201:6443", "10.0.154.83:6443", "10.0.165.154:6443"}
	fmt.Println(fmt.Sprintf("creating and starting the health monitor for %v", targetProvider))
	// TODO: copy the config and set a separate userAgent
	healthMonitor, err := NewHealthMonitor(targetProvider, createConfigForHealthMonitor(config))
	if err != nil {
		panic(err)
	}

	loadBalancer, err := NewOxyRoundRobinLoadBalancer()
	if err != nil {
		panic(err)
	}

	// I think that the following setting will trigger the circuit breaker:
	//   when 1% of the requests returned a 5XX status
	//   OR when the ratio of network errors reaches 1%,
	//     it looks that StatusGatewayTimeout OR StatusBadGateway HTTP status codes
	//     are considered network errors (https://github.com/p0lyn0mial/oxy/blob/c52e3e446ec6a8a2ee1fc7a9cc85a06e999b05ca/memmetrics/roundtrip.go#L212)
	//
	// TODO: set fallback duration and recovery duration
	circuitBreakerFn := NewOxyCircuitBreakerWithOptions("ResponseCodeRatio(500, 600, 0, 600) > 0.1 || NetworkErrorRatio() > 0.1")
	circuitBreakerFactory := NewOxyCircuitBreakerFactory(healthMonitor, loadBalancer, circuitBreakerFn)

	ct := newCustomTransportWrapper(loadBalancer, circuitBreakerFactory.Get)
	config.Wrap(ct)

	fmt.Println("creating the k8s client set for the config")
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	go healthMonitor.StartMonitoring(context.TODO().Done())
	// TODO wait for healthy servers to be observed

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

func createConfigForHealthMonitor(restConfig *rest.Config) *rest.Config {
	restConfigCopy := *restConfig
	restConfigCopy.UserAgent = "HealthMonitor" // find a way that would help identify the client
	restConfigCopy.TLSClientConfig.ServerName = "kubernetes.default.svc"

	// doesn't matter since we are not using client-go only the TLS config
	restConfigCopy.QPS = 1
	restConfigCopy.Burst = 1

	return &restConfigCopy
}

func newCustomTransportWrapper(lb LoadBalancer, cbGetter func(server string) CircuitBreaker) func(http.RoundTripper) http.RoundTripper {
	return func(rt http.RoundTripper) http.RoundTripper { return &customTransport{baseRT: rt, lb: lb, cbGetter: cbGetter} }
}

type customTransport struct {
	baseRT http.RoundTripper
	lb     LoadBalancer
	cbGetter func(server string) CircuitBreaker
}

func (t *customTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	fmt.Println(fmt.Sprintf("customTransport: RoundTrip: received a request for Method = %q to Host = %q, URL = %q", r.Method, r.Host, r.URL))

	// TODO: filter on specific path (for example only SAR requests)
	// TODO: if there are no healthy targets we could use the service IP - why not ?
	server, err := t.lb.NextServer()
	if err != nil {
		return nil, err
	}
	seenServersSet := sets.NewString(server)

	for {
		// I don't like modifying the original request
		r.Host = server
		r.URL.Host = server

		fmt.Println(fmt.Sprintf("customTransport: RoundTrip: The new Host is = %q", r.Host))

		crt := t.cbGetter(server)
		rsp, err := crt.RoundTripWithDelegate(r, t.baseRT)
		if rsp == nil && err != nil && crt.FailedBecauseCircuitBreakerIsOpen(err) {
			allServers := t.lb.Servers()
			potentialServersSet := sets.NewString(allServers...)
			potentialServersSet.Delete(seenServersSet.List()...)
			if potentialServersSet.Len() == 0 {
				return nil, fmt.Errorf("service unavailable, tried %d servers", seenServersSet.Len())
			}
			server = potentialServersSet.List()[0]
			seenServersSet.Insert(server)
			continue
		}

		if seenServersSet.Len() > 1 {
			// this is the place where we are going to measure success of this mechanism
			// we have hit more than one server
			// if the request succeeded (HTTP 200) we have won
			// TODO: add a metric
		}

		return rsp, err
	}
}
