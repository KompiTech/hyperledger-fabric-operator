FROM alpine:3.8

RUN apk upgrade --update --no-cache

USER nobody

ADD build/_output/bin/hl-fabric-operator /usr/local/bin/hl-fabric-operator
