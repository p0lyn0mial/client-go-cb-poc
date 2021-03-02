package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/config/helpers"
	"github.com/openshift/library-go/pkg/healthmonitor"
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
	config.Timeout = 30 * time.Second
	config.QPS = 1
	config.Burst = 1


	targetProvider := healthmonitor.StaticTargetProvider{"localhost:6443"}
	fmt.Println(fmt.Sprintf("creating and starting the health monitor for %v", targetProvider))
	hm, err := healthmonitor.New(targetProvider, createConfigForHealthMonitor(config), 2,5, 1 * time.Second, 2*time.Second)
	if err != nil {
		panic(err)
	}
	go hm.Run(context.TODO().Done())

	ct := newPreferredHostTransportWrapper(func () string {
		healthyTargets, _:= hm.Targets()
		if len(healthyTargets) == 1 {
			return healthyTargets[0]
		}
		return ""
	})
	config.Wrap(ct)

	fmt.Println("creating the k8s client set for the config\n")
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for {
		fmt.Println("about to LIST secrets in the default namespace")
		ret, err := clientset.CoreV1().Secrets("test-01").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			fmt.Println(fmt.Sprintf("error while listing secrets, err = %v", err))
		}
		fmt.Println(fmt.Sprintf("found %d secrets in the default namespace", len(ret.Items)))
	}
}

func newPreferredHostTransportWrapper(preferredHostFn func() string) func(http.RoundTripper) http.RoundTripper {
	return func(rt http.RoundTripper) http.RoundTripper { return &preferredHostTransport{baseRT: rt, preferredHostFn: preferredHostFn} }
}

type preferredHostTransport struct {
	baseRT http.RoundTripper
	preferredHostFn func() string
}

func (t *preferredHostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	fmt.Println(fmt.Sprintf("preferredHostTransport: RoundTrip: received a request for Method = %q to Host = %q, URL = %q", r.Method, r.Host, r.URL))

	preferredHost := t.preferredHostFn()

	if len(preferredHost) == 0 {
		return t.baseRT.RoundTrip(r)
	}

	r.Host = preferredHost
	r.URL.Host = preferredHost
	fmt.Println(fmt.Sprintf("preferredHostTransport: RoundTrip: The new Host is = %q", r.Host))
	return t.baseRT.RoundTrip(r)
}

func createConfigForHealthMonitor(restConfig *rest.Config) *rest.Config {
	restConfigCopy := *restConfig
	restConfigCopy.UserAgent = "HealthMonitor" // find a way that would help identify the client

	// doesn't matter since we are not using client-go only the TLS config
	restConfigCopy.QPS = -1
	restConfigCopy.Burst = -1

	return &restConfigCopy
}