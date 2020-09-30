# efried: test

SHELL := /usr/bin/env bash

# Include shared Makefiles
include project.mk
include standard.mk
include functions.mk

default: gobuild

# Extend Makefile after here

# Build the container image
.PHONY: container-build
container-build:
	$(MAKE) build

# Push the container image
.PHONY: container-push
container-push:
	$(MAKE) push
