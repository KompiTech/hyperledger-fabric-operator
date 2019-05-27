.DEFAULT_GOAL := default

VERSION?=v0.0.0-prototype0
COMMIT=$(shell git rev-parse HEAD)
COMMIT_SHORT=$(shell git rev-parse --short HEAD)
BRANCH=$(shell git rev-parse --abbrev-ref HEAD)
IMG_REPO?=
IMG_TAG?=${BRANCH}-${COMMIT_SHORT}
IMG_TAG_LATEST?=latest
COVER_PROFILE:="./build/coverage.out"
CGO?=1
GOPROXY?=

PKG_NAME?=hl-fabric-operator
IMAGE?=hl-fabric-operator
CMD_PATH?=cmd/manager/main.go
BUILD_DIR?=build/_output

VERSION_IMPORT=main
LDFLAGS=-ldflags '-X ${VERSION_IMPORT}.version=${VERSION} -X ${VERSION_IMPORT}.commit=${COMMIT} -X ${VERSION_IMPORT}.branch=${BRANCH}'

.PHONY: default all clean run
.PHONY: test unit-test code-check-test cover
.PHONY: build build-all build-linux build-darwin build-windows
.PHONY: image compose compose-stop

# test: code-check-test unit-test

# code-check-test:
# 	go vet -shadow=true -shadowstrict=true ./...

# unit-test: compose-stop gen-test-certs
# 	KHLP_CERT=${TEST_CERT} \
# 	KHLP_CERT_KEY=${TEST_CERT_KEY} \
# 	go test -v -cover -coverprofile=${COVER_PROFILE} ./...

# cover: unit-test
# 	go tool cover -html=${COVER_PROFILE}

build:
	go build -o ${BUILD_DIR}/${PKG_NAME} ${LDFLAGS} ${CMD_PATH}

build-all: build-linux build-darwin build-windows

build-linux:
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=${CGO} go build -o ${BUILD_DIR}/${PKG_NAME}.linux ${LDFLAGS} ${CMD_PATH}
build-darwin:
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=${CGO} go build -o ${BUILD_DIR}/${PKG_NAME}.darwin ${LDFLAGS} ${CMD_PATH}
build-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=${CGO} go build -o ${BUILD_DIR}/${PKG_NAME}.windows ${LDFLAGS} ${CMD_PATH}

clean:
	rm -rf ./${BUILD_DIR}/

image: clean
	DOCKER_BUILDKIT=1 docker build --ssh default --build-arg GOPROXY="$${GOPROXY}" -t ${IMG_REPO}${IMAGE}:${IMG_TAG} -t ${IMG_REPO}${IMAGE}:${IMG_TAG_LATEST} --progress=plain .

image-publish: image publish

publish:
	docker push ${IMG_REPO}${IMAGE}:${IMG_TAG}
	docker push ${IMG_REPO}${IMAGE}:${IMG_TAG_LATEST}


# compose: image compose-stop
# 	KHLP_CERT=${TEST_CERT} \
# 	KHLP_CERT_KEY=${TEST_CERT_KEY} \
# 	KHLP_SESSIONS_SECRET=${TEST_SESSIONS_SECRET} \
# 	KHLP_CSRF_SECRET=${TEST_CSRF_SECRET} \
# 	KHLP_OAUTH2_CONFIGS='$(shell cat var-oauth2.env)' \
# 	KHLP_JWT_KEYS='$(shell cat var-jwt_keys.env)' \
# 	BRANDNAME=${BRANDNAME} \
# 	docker-compose up -d -V

# compose-stop:
# 	-BRANDNAME=${BRANDNAME} docker-compose stop

# run: gen-test-certs
# 	KHLP_CERT=${TEST_CERT} \
# 	KHLP_CERT_KEY=${TEST_CERT_KEY} \
# 	KHLP_SESSIONS_SECRET=${TEST_SESSIONS_SECRET} \
# 	KHLP_CSRF_SECRET=${TEST_CSRF_SECRET} \
# 	KHLP_OAUTH2_CONFIGS='$(shell cat var-oauth2.env)' \
# 	KHLP_JWT_KEYS='$(shell cat var-jwt_keys.env)' \
# 	go run ${LDFLAGS} .

all: clean test build-all image

default: build #test
