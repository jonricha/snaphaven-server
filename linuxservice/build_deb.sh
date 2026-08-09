#!/usr/bin/env bash
set -e

# ==============================================================================
# Linux Debian (.deb) & Tarball Packaging Script for SnapHaven Server
# ==============================================================================

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
SERVER_DIR="$ROOT_DIR/server"
BUILD_DIR="$SCRIPT_DIR/build"
DEB_DIR="$BUILD_DIR/snaphaven-server_1.0.0_amd64"

VERSION="${VERSION:-v1.0.0}"
BUILDDATE=$(date -u +"%Y-%m-%d %H:%M:%S UTC")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none")

echo "📦 1. Compiling Linux server executable..."
cd "$SERVER_DIR"
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-X 'main.Version=$VERSION' -X 'main.Commit=$COMMIT' -X 'main.BuildTime=$BUILDDATE'" -o "$SCRIPT_DIR/snaphaven-server" .

echo "🐧 2. Assembling Debian Package Directory..."
rm -rf "$BUILD_DIR"
mkdir -p "$DEB_DIR/DEBIAN"
mkdir -p "$DEB_DIR/usr/local/bin"
mkdir -p "$DEB_DIR/etc/systemd/user"

cp "$SCRIPT_DIR/control" "$DEB_DIR/DEBIAN/control"
cp "$SCRIPT_DIR/snaphaven-server" "$DEB_DIR/usr/local/bin/snaphaven-server"
cp "$SCRIPT_DIR/snaphaven-server.service" "$DEB_DIR/etc/systemd/user/snaphaven-server.service"

chmod +x "$DEB_DIR/usr/local/bin/snaphaven-server"

echo "🚚 3. Building .deb package..."
dpkg-deb --build "$DEB_DIR" "$SCRIPT_DIR/snaphaven-server_1.0.0_amd64.deb"

cd "$SCRIPT_DIR"
tar -czvf SnapHavenServer-Linux-x86_64.tar.gz snaphaven-server

echo "✅ Linux packaging complete: $SCRIPT_DIR/snaphaven-server_1.0.0_amd64.deb"
