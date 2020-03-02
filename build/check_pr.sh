#!/bin/bash -x

set -e

cd $(dirname $0)/..

# it's okay to omit the IMAGE_REPOSITORY since this is just a PR test
make container-build && make clean
