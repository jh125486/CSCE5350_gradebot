# Use the official Go devcontainer image as the base
FROM mcr.microsoft.com/devcontainers/go:1.24

# Download AWS CLI archive based on architecture
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "x86_64" ]; then \
        AWSCLI_URL="https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip"; \
    elif [ "$ARCH" = "aarch64" ]; then \
        AWSCLI_URL="https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip"; \
    else \
        echo "Unsupported architecture: $ARCH" && exit 1; \
    fi && \
    wget "$AWSCLI_URL" -O /tmp/awscliv2.zip

# Install additional tools and dependencies
RUN apt-get update \
    && apt-get install -y \
        curl \
        jq \
        postgresql-client \
        python3-pip \
        unzip \
        wget \
    && rm -rf /var/lib/apt/lists/* \
    && unzip /tmp/awscliv2.zip -d /tmp \
    && /tmp/aws/install \
    && rm -rf /tmp/aws /tmp/awscliv2.zip \
    && pip3 install awscli-local --break-system-packages \
    && mkdir -p /workspaces

# Set the default shell
ENV SHELL=/bin/bash

WORKDIR /workspaces

# Switch to vscode user
USER vscode