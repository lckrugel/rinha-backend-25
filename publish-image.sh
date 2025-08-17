#!/bin/bash

IMAGE_NAME="lckrugel98/rinha-backend-2025-go"

if [ -z "$1" ]; then
  echo "Usage: $0 <version>"
  exit 1
fi

VERSION="$1"

# Build the image
docker build -t $IMAGE_NAME:$VERSION .

# Set version tag to latest tag
docker tag $IMAGE_NAME:$VERSION $IMAGE_NAME:latest

# Push the version tag
docker push $IMAGE_NAME:$VERSION

# Push the latest tag
docker push $IMAGE_NAME:latest

echo "Image $IMAGE_NAME:$VERSION and $IMAGE_NAME:latest pushed to Docker Hub."
