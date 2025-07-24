#!/bin/bash

# Test Docker build locally using act
# This script helps debug Docker build issues

set -e

echo "🧪 Testing Docker build locally..."

# Check if act is installed
if ! command -v act &> /dev/null; then
    echo "❌ act is not installed. Please install it first:"
    echo "   brew install act (macOS)"
    echo "   or visit: https://github.com/nektos/act"
    exit 1
fi

# Create a temporary workflow file for testing
cat > .github/workflows/test-docker-build.yml << 'EOF'
name: Test Docker Build

on:
  workflow_dispatch:

jobs:
  test-docker-build:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Build logger image (AMD64 only for testing)
        uses: docker/build-push-action@v6
        with:
          context: .
          file: Dockerfile.logger
          platforms: linux/amd64
          push: false
          cache-from: type=gha
          cache-to: type=gha,mode=max
          build-args: |
            BUILDKIT_INLINE_CACHE=1
          provenance: false
EOF

echo "📋 Created test workflow file"

# Run the test workflow
echo "🚀 Running Docker build test..."
act workflow_dispatch -W .github/workflows/test-docker-build.yml

# Cleanup
rm -f .github/workflows/test-docker-build.yml

echo "✅ Test completed!" 