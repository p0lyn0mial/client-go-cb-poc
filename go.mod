module github.com/p0lyn0mial/client-go-cb-poc

go 1.15

require (
	github.com/google/go-cmp v0.5.2
	github.com/openshift/api v0.0.0-20201214114959-164a2fb63b5f
	github.com/openshift/library-go v0.0.0-20210127081712-a4f002827e42
	github.com/vulcand/oxy v1.1.0
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e // indirect
	k8s.io/apimachinery v0.20.0
	k8s.io/client-go v0.20.0
	k8s.io/klog/v2 v2.4.0
)

replace (
	github.com/vulcand/oxy  => /Users/lszaszki/go/src/github.com/p0lyn0mial/oxy
)
