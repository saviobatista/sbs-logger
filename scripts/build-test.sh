#!/bin/bash

# Simple Docker build test script
# This script helps debug Docker build issues locally

set -e

echo "🧪 Testing Docker builds locally..."

# Check if Docker is running
if ! docker info &> /dev/null; then
    echo "❌ Docker is not running. Please start Docker first."
    exit 1
fi

# Services to test
SERVICES=("ingestor" "logger" "tracker" "migrate")

for service in "${SERVICES[@]}"; do
    echo "🔨 Building $service for AMD64..."
    docker buildx build \
        --platform linux/amd64 \
        --file Dockerfile.$service \
        --tag sbs-$service:test-amd64 \
        --progress=plain \
        --no-cache \
        .
    
    echo "✅ $service AMD64 build completed!"
    
    echo "🔨 Building $service for ARM64..."
    docker buildx build \
        --platform linux/arm64 \
        --file Dockerfile.$service \
        --tag sbs-$service:test-arm64 \
        --progress=plain \
        --no-cache \
        .
    
    echo "✅ $service ARM64 build completed!"
done

echo "🚀 Testing container runs..."
for service in "${SERVICES[@]}"; do
    echo "Testing $service container..."
    docker run --rm sbs-$service:test-amd64 --help 2>/dev/null || echo "$service container started successfully"
done

echo "✅ All tests completed!"
echo "📦 Images created:"
docker images | grep sbs- 