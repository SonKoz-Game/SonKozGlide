#!/usr/bin/env bash
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
sdk="${ANDROID_HOME:-$LOCALAPPDATA/Android/Sdk}"
ndk="$(ls -d "$sdk"/ndk/* | sort | tail -1)"
export ANDROID_HOME="$sdk"
export ANDROID_NDK_HOME="$ndk"
export PATH="$PATH:$(go env GOPATH)/bin"
cd "$here/core"
gomobile bind -target=android/arm64,android/arm,android/amd64 -androidapi 24 \
  -o "$here/app/libs/glide.aar" ./glide
echo "wrote $here/app/libs/glide.aar"
