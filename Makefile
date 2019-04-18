SHELL := /usr/bin/env bash

# Include shared Makefiles
include project.mk
include standard.mk

default: gobuild

# Extend Makefile after here

# Build the docker image
.PHONY: docker-build
docker-build:
	build

# Push the docker image
.PHONY: docker-push
docker-push:
	push
