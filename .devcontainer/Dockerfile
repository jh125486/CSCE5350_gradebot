# Use the official Go devcontainer image as the base
FROM mcr.microsoft.com/devcontainers/go:1.24

# Install additional tools
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    jq \
    unzip \
    python3-pip \
    && rm -rf /var/lib/apt/lists/*

# Install AWS CLI
RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip" -o "awscliv2.zip" \
    && unzip awscliv2.zip \
    && ./aws/install \
    && rm -rf aws awscliv2.zip

# Install awslocal
RUN pip3 install awscli-local --break-system-packages

# Set the default shell
ENV SHELL=/bin/bash

# Create workspace directory
RUN mkdir -p /workspaces
WORKDIR /workspaces

# Switch to vscode user
USER vscode