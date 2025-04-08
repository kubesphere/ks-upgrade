
REPO ?= kubesphere
TAG ?= latest


build-local: ; $(info $(M)...Begin to build ks-upgrade binary.)  @ ## Build ks-upgrade.
	CGO_ENABLED=0 go build -mod=vendor -ldflags \
	"-X 'main.goVersion=$(shell go version|sed 's/go version //g')' \
	-X 'main.gitHash=$(shell git describe --dirty --always --tags)' \
	-X 'main.buildTime=$(shell TZ=UTC-8 date +%Y-%m-%d" "%H:%M:%S)'" \
	-o bin/ks-upgrade cmd/ks-upgrade.go

build-image: ; $(info $(M)...Begin to build ks-upgrade image.)  @ ## Build ks-upgrade image.
	docker build -f Dockerfile -t ${REPO}/ks-upgrade:${TAG}  .
	docker push ${REPO}/ks-upgrade:${TAG}

build-cross-image: ; $(info $(M)...Begin to build ks-upgrade image.)  @ ## Build ks-upgrade image.
	docker buildx build -f Dockerfile -t ${REPO}/ks-upgrade:${TAG} --push --platform linux/amd64,linux/arm64 .