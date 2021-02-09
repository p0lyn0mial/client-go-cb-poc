FROM registry.svc.ci.openshift.org/ocp/builder:rhel-8-golang-1.15-openshift-4.6 AS builder
WORKDIR /go/src/github.com/p0lyn0mial/client-go-cb-poc
COPY . .
RUN go build -o ./app .

FROM debian
COPY --from=builder /go/src/github.com/p0lyn0mial/client-go-cb-poc/app /usr/bin/
ENTRYPOINT /usr/bin/app
