#!/bin/bash
# Install git hooks

set -e

REPO_ROOT=$(git rev-parse --show-toplevel)
HOOKS_DIR="$REPO_ROOT/.git/hooks"
GITHOOKS_DIR="$REPO_ROOT/.githooks"

echo "📦 Installing git hooks..."

# Install pre-push hook
if [ -f "$GITHOOKS_DIR/pre-push" ]; then
    cp "$GITHOOKS_DIR/pre-push" "$HOOKS_DIR/pre-push"
    chmod +x "$HOOKS_DIR/pre-push"
    echo "✅ Installed pre-push hook"
else
    echo "❌ Pre-push hook not found at $GITHOOKS_DIR/pre-push"
    exit 1
fi

echo ""
echo "🎉 Git hooks installed successfully!"
echo ""
echo "The pre-push hook will now run tests and linting before each push."
echo "To bypass the hook (not recommended), use: git push --no-verify"
echo ""
