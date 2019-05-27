#syntax=docker/dockerfile:experimental
FROM golang:1.12.3-alpine3.9 as build_base
ARG GOPROXY
RUN apk add --no-cache --update alpine-sdk make git openssl gcc openssh
RUN mkdir /src
RUN mkdir /root/.ssh/
RUN touch /root/.ssh/known_hosts
RUN git config --global url."git@github.com:KompiTech".insteadOf https://github.com/KompiTech
RUN ssh-keyscan github.com >> /root/.ssh/known_hosts
COPY go.mod /src
COPY go.sum /src
WORKDIR /src
RUN --mount=type=ssh go mod download

FROM build_base as build
ARG GOPROXY
COPY . /src
WORKDIR /src
RUN make build-linux

FROM alpine:3.9
RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*
WORKDIR /
COPY --from=build /src/build/_output/hl-fabric-operator.linux /
ENTRYPOINT ["/hl-fabric-operator.linux"]
