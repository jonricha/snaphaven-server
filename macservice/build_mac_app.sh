#!/usr/bin/env bash
set -e

# ==============================================================================
# macOS App Bundle & Release Packaging Script for SnapHaven Server
# ==============================================================================

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
SERVER_DIR="$ROOT_DIR/server"
APP_NAME="SnapHaven Server.app"
BUILD_DIR="$SCRIPT_DIR/build"

VERSION="${VERSION:-v1.0.0}"
BUILDDATE=$(date -u +"%Y-%m-%d %H:%M:%S UTC")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none")

echo "📦 1. Compiling macOS binaries..."
cd "$SERVER_DIR"

# Compile Apple Silicon (arm64) and Intel (amd64)
GOOS=darwin GOARCH=arm64 go build -ldflags "-X 'main.Version=$VERSION' -X 'main.Commit=$COMMIT' -X 'main.BuildTime=$BUILDDATE'" -o "$SCRIPT_DIR/snaphaven-mac-arm64" .
GOOS=darwin GOARCH=amd64 go build -ldflags "-X 'main.Version=$VERSION' -X 'main.Commit=$COMMIT' -X 'main.BuildTime=$BUILDDATE'" -o "$SCRIPT_DIR/snaphaven-mac-amd64" .

echo "🍏 2. Assembling macOS App Bundle..."
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR/$APP_NAME/Contents/MacOS"
mkdir -p "$BUILD_DIR/$APP_NAME/Contents/Resources"

cp "$SCRIPT_DIR/Info.plist" "$BUILD_DIR/$APP_NAME/Contents/Info.plist"
cp "$SCRIPT_DIR/snaphaven-mac-arm64" "$BUILD_DIR/$APP_NAME/Contents/MacOS/snaphaven"
chmod +x "$BUILD_DIR/$APP_NAME/Contents/MacOS/snaphaven"

echo "🚚 3. Packaging Release Archive..."
cd "$BUILD_DIR"
tar -czvf "$SCRIPT_DIR/SnapHavenServer-macOS.tar.gz" "$APP_NAME"

echo "✅ macOS packaging complete: $SCRIPT_DIR/SnapHavenServer-macOS.tar.gz"
