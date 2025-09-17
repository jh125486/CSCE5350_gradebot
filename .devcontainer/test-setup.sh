#!/bin/bash

# Test script for devcontainer LocalStack setup
set -e

echo "🧪 Testing LocalStack setup..."

# Wait for LocalStack to be ready
echo "⏳ Waiting for LocalStack..."
max_attempts=30
attempt=1
while ! curl -s http://localstack:4566/_localstack/health | grep -q '"s3": "running"'; do
  if [ $attempt -ge $max_attempts ]; then
    echo "❌ LocalStack failed to start after $max_attempts attempts"
    exit 1
  fi
  echo "  Attempt $attempt/$max_attempts..."
  sleep 2
  ((attempt++))
done

echo "✅ LocalStack is ready!"

# Test bucket creation
echo "📦 Testing S3 bucket operations..."
export AWS_ENDPOINT_URL=http://localstack:4566
awslocal s3 ls

# Test our storage package
echo "🔧 Testing storage package..."
cd /workspaces/CSCE5350_gradebot
go test ./pkg/storage -v

echo "🎉 All tests passed!"
echo ""
echo "🚀 Your devcontainer is ready for development!"
echo "   - LocalStack S3: http://localhost:4566"
echo "   - Gradebot server: http://localhost:8080 (when running)"
echo "   - Run 'go test ./...' to test everything"