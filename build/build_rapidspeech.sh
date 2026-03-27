#!/bin/bash
# Build RapidSpeech static library for CGO embedding integration.
# Output: RapidSpeech.cpp/build/librapidspeech_static.a
#
# Prerequisites: CMake 3.14+, C/C++ compiler (gcc/clang)

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RS_DIR="$SCRIPT_DIR/../RapidSpeech.cpp"
BUILD_DIR="$RS_DIR/build"

echo "[build_rapidspeech] Building static library..."
echo "[build_rapidspeech] Source: $RS_DIR"
echo "[build_rapidspeech] Build:  $BUILD_DIR"

mkdir -p "$BUILD_DIR"

cmake -S "$RS_DIR" -B "$BUILD_DIR" \
    -DCMAKE_BUILD_TYPE=Release \
    -DBUILD_SHARED_LIBS=OFF \
    -DRS_BUILD_TESTS=OFF \
    -DRS_BUILD_SERVER=OFF \
    -DRS_ENABLE_PYTHON=OFF

cmake --build "$BUILD_DIR" --target rapidspeech_static --config Release -j"$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)"

echo "[build_rapidspeech] Done. Static library built successfully."
