#!/bin/bash

# Wait for LocalStack to be ready
echo "Waiting for LocalStack to be ready..."
while ! curl -s http://localhost:4566/_localstack/health | grep -q '"s3": "available"'; do
  echo "Waiting for S3 service..."
  sleep 1
done

echo "LocalStack is ready. Creating S3 bucket..."

# Create the test bucket
awslocal s3 mb s3://test-gradebot-bucket

# Create additional buckets for testing
awslocal s3 mb s3://gradebot-prod-bucket
awslocal s3 mb s3://gradebot-staging-bucket

# Set bucket policies for public read (optional, for testing)
awslocal s3api put-bucket-policy --bucket test-gradebot-bucket --policy '{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": "*",
      "Action": "s3:GetObject",
      "Resource": "arn:aws:s3:::test-gradebot-bucket/*"
    }
  ]
}'

echo "S3 buckets created successfully!"
echo "LocalStack initialization complete."
echo ""
echo "Available buckets:"
awslocal s3 ls