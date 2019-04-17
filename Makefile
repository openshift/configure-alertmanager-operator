SHELL := /bin/bash
include version.mk
include project.mk

IMAGE_REPOSITORY=openshift-sre
OPERATOR_IMAGE_URI=${IMAGE_REGISTRY}/${IMAGE_REPOSITORY}/${IMAGE_NAME}:v${VERSION_FULL}

VERSION_MAJOR=0
VERSION_MINOR=1

BINFILE=build/_output/bin/configure-alertmanager-operator
MAINPACKAGE=./cmd/manager
GOENV=GOOS=linux GOARCH=amd64 CGO_ENABLED=0
GOFLAGS=-gcflags="all=-trimpath=${GOPATH}" -asmflags="all=-trimpath=${GOPATH}"

.PHONY: all
all: check dockerbuild

.PHONY: isclean
isclean:
	@(test "${ALLOW_DIRTY_CHECKOUT}" != "false" || test 0 -eq $$(git status --porcelain | wc -l)) || (echo "Local git checkout is not clean, commit changes and try again." && exit 1)

.PHONY: check
check: ## Lint code
	gofmt -s -l $(shell go list -f '{{ .Dir }}' ./... ) | grep ".*\.go"; if [ "$$?" = "0" ]; then gofmt -s -d $(shell go list -f '{{ .Dir }}' ./... ); exit 1; fi
	go vet ./cmd/... ./pkg/...

.PHONY: dockerbuild
dockerbuild:
	docker build -f build/Dockerfile . -t ${OPERATOR_IMAGE_URI}

# This part is done by the docker build
.PHONY: gobuild
gobuild: ## Build binary
	${GOENV} go build ${GOFLAGS} -a -o ${BINFILE} ${MAINPACKAGE}

.PHONY: env
.SILENT: env
env: isclean
	echo OPERATOR_NAME=${OPERATOR_NAME}
	echo OPERATOR_NAMESPACE=${OPERATOR_NAMESPACE}
	echo OPERATOR_VERSION=${VERSION_FULL}
	echo OPERATOR_IMAGE_URI=${OPERATOR_IMAGE_URI}

