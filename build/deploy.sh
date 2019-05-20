#!/bin/bash

set -e

cd $(dirname $0)/..

if [[ -z $IMAGE_REPOSITORY ]]; then
  IMAGE_REPOSITORY=app-sre
fi

# Build the image

make IMAGE_REPOSITORY=$IMAGE_REPOSITORY build skopeo-push
