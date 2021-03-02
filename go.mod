module github.com/p0lyn0mial/client-go-cb-poc

go 1.15

require (
	github.com/openshift/api v0.0.0-20201214114959-164a2fb63b5f
	github.com/openshift/library-go v0.0.0-20210301154249-aa29957b8a9c
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e // indirect
	k8s.io/apimachinery v0.20.0
	k8s.io/client-go v0.20.0
)

replace github.com/openshift/library-go v0.0.0-20210301154249-aa29957b8a9c => /Users/lszaszki/go/src/github.com/openshift/library-go
