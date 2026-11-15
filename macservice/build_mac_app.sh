#!/usr/bin/env bash
set -e

# ==============================================================================
# macOS App Bundle & Release Packaging Script for SnapHaven Server
# ==============================================================================
# Optional env vars for Mac App Store builds:
#   MAS_BUILD=1                        Compile with -tags mas (disables self-updater, uses SMAppService)
#   BUILD_NUMBER=42                    CFBundleVersion (defaults to GITHUB_RUN_NUMBER or 1)
#   MAS_PROVISION_PROFILE=/path/to/... Embed Contents/embedded.provisionprofile
#   MAC_APP_SIGN_IDENTITY="3rd Party Mac Developer Application: ..."
#   MAC_INSTALLER_SIGN_IDENTITY="3rd Party Mac Developer Installer: ..."
# ==============================================================================

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
SERVER_DIR="$ROOT_DIR/server"
APP_NAME="SnapHaven Server.app"
BUILD_DIR="$SCRIPT_DIR/build"

RAW_VERSION="${GITHUB_REF_NAME:-${VERSION:-$(git -C "$ROOT_DIR" describe --tags --abbrev=0 2>/dev/null || echo "v1.1.13")}}"
CLEAN_VERSION=$(echo "$RAW_VERSION" | sed 's/^v//')
BUILD_NUMBER="${BUILD_NUMBER:-${GITHUB_RUN_NUMBER:-1}}"
BUILDDATE=$(date -u +"%Y-%m-%d %H:%M:%S UTC")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none")

GO_BUILD_TAGS=""
if [ "${MAS_BUILD:-0}" = "1" ]; then
    GO_BUILD_TAGS="-tags mas"
    echo "🍎 Building Mac App Store variant (-tags mas)..."
fi

echo "📦 1. Compiling macOS binaries for version $RAW_VERSION (build $BUILD_NUMBER)..."
cd "$SERVER_DIR"

# Compile Apple Silicon (arm64) and Intel (amd64) with native Cocoa CGO enabled
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build $GO_BUILD_TAGS -ldflags "-X 'main.Version=$RAW_VERSION' -X 'main.Commit=$COMMIT' -X 'main.BuildTime=$BUILDDATE'" -o "$SCRIPT_DIR/snaphaven-mac-arm64" .
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build $GO_BUILD_TAGS -ldflags "-X 'main.Version=$RAW_VERSION' -X 'main.Commit=$COMMIT' -X 'main.BuildTime=$BUILDDATE'" -o "$SCRIPT_DIR/snaphaven-mac-amd64" .

echo "🍏 2. Assembling macOS Universal App Bundle..."
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR/$APP_NAME/Contents/MacOS"
mkdir -p "$BUILD_DIR/$APP_NAME/Contents/Resources"

sed -e "s/1.0.0/$CLEAN_VERSION/g" -e "s/__BUILD_NUMBER__/$BUILD_NUMBER/g" "$SCRIPT_DIR/Info.plist" > "$BUILD_DIR/$APP_NAME/Contents/Info.plist"
lipo -create -output "$BUILD_DIR/$APP_NAME/Contents/MacOS/snaphaven" "$SCRIPT_DIR/snaphaven-mac-arm64" "$SCRIPT_DIR/snaphaven-mac-amd64"
chmod +x "$BUILD_DIR/$APP_NAME/Contents/MacOS/snaphaven"
rm -f "$SCRIPT_DIR/snaphaven-mac-arm64" "$SCRIPT_DIR/snaphaven-mac-amd64"

echo "🎨 3. Generating icon.icns..."
ICON_SRC=""
if [ -f "$ROOT_DIR/../snaphaven.app/store_icon_1080.png" ]; then
    ICON_SRC="$ROOT_DIR/../snaphaven.app/store_icon_1080.png"
elif [ -f "$SERVER_DIR/icon.png" ]; then
    ICON_SRC="$SERVER_DIR/icon.png"
fi

if [ -n "$ICON_SRC" ] && command -v sips >/dev/null 2>&1 && command -v iconutil >/dev/null 2>&1; then
    ICONSET_DIR="$BUILD_DIR/icon.iconset"
    mkdir -p "$ICONSET_DIR"
    sips -z 16 16     "$ICON_SRC" --out "$ICONSET_DIR/icon_16x16.png" >/dev/null
    sips -z 32 32     "$ICON_SRC" --out "$ICONSET_DIR/icon_16x16@2x.png" >/dev/null
    sips -z 32 32     "$ICON_SRC" --out "$ICONSET_DIR/icon_32x32.png" >/dev/null
    sips -z 64 64     "$ICON_SRC" --out "$ICONSET_DIR/icon_32x32@2x.png" >/dev/null
    sips -z 128 128   "$ICON_SRC" --out "$ICONSET_DIR/icon_128x128.png" >/dev/null
    sips -z 256 256   "$ICON_SRC" --out "$ICONSET_DIR/icon_128x128@2x.png" >/dev/null
    sips -z 256 256   "$ICON_SRC" --out "$ICONSET_DIR/icon_256x256.png" >/dev/null
    sips -z 512 512   "$ICON_SRC" --out "$ICONSET_DIR/icon_256x256@2x.png" >/dev/null
    sips -z 512 512   "$ICON_SRC" --out "$ICONSET_DIR/icon_512x512.png" >/dev/null
    sips -z 1024 1024 "$ICON_SRC" --out "$ICONSET_DIR/icon_512x512@2x.png" >/dev/null
    iconutil -c icns "$ICONSET_DIR" -o "$BUILD_DIR/$APP_NAME/Contents/Resources/icon.icns"
    rm -rf "$ICONSET_DIR"
else
    echo "⚠️ Skipping icon.icns generation (missing source PNG or sips/iconutil)."
fi

if [ -n "${MAS_PROVISION_PROFILE:-}" ] && [ -f "$MAS_PROVISION_PROFILE" ]; then
    echo "📄 Embedding MAS provisioning profile..."
    cp "$MAS_PROVISION_PROFILE" "$BUILD_DIR/$APP_NAME/Contents/embedded.provisionprofile"
fi

if [ -n "${MAC_APP_SIGN_IDENTITY:-}" ]; then
    echo "🔏 4. Code-signing App Bundle with App Sandbox entitlements..."
    codesign --force --options runtime --timestamp \
        --entitlements "$SCRIPT_DIR/entitlements.mas.plist" \
        --sign "$MAC_APP_SIGN_IDENTITY" \
        "$BUILD_DIR/$APP_NAME"
    codesign --verify --deep --strict --verbose=2 "$BUILD_DIR/$APP_NAME"
fi

if [ -n "${MAC_INSTALLER_SIGN_IDENTITY:-}" ]; then
    echo "📦 5. Building signed Mac App Store .pkg..."
    productbuild --component "$BUILD_DIR/$APP_NAME" /Applications \
        --sign "$MAC_INSTALLER_SIGN_IDENTITY" \
        "$SCRIPT_DIR/SnapHavenServer-MAS.pkg"
    echo "✅ Mac App Store package ready: $SCRIPT_DIR/SnapHavenServer-MAS.pkg"
else
    echo "🚚 4. Packaging Release Archive..."
    cd "$BUILD_DIR"
    tar -czvf "$SCRIPT_DIR/SnapHavenServer-macOS.tar.gz" "$APP_NAME"
    echo "✅ macOS packaging complete: $SCRIPT_DIR/SnapHavenServer-macOS.tar.gz"
fi
