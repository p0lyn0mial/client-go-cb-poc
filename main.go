package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	config.Timeout = 30 * time.Second
	config.QPS = 1
	config.Burst = 1

	fmt.Println("creating the k8s client set for the config\n")
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for {
		fmt.Println("about to LIST secrets in the default namespace")
		ret, err := clientset.CoreV1().Secrets("default").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			fmt.Println(fmt.Sprintf("error while listing secrets, err = %v", err))
		}
		fmt.Println(fmt.Sprintf("found %d secrets in the default namespace", len(ret.Items)))
	}
}
