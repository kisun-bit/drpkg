#!/bin/bash
#
# drfirstboot cross compile script
#
# Supported targets:
#   windows amd64
#   windows 386
#   linux amd64
#   linux arm64
#
# Usage:
#   ./build.sh <os> <arch>
#
# Examples:
#   ./build.sh windows amd64
#   ./build.sh linux arm64
#

set -e

set -e


if [ $# -ne 2 ]; then
    echo "Usage:"
    echo "  $0 <windows|linux> <amd64|386|arm64>"
    exit 1
fi


OS=$1
ARCH=$2


if [ "$OS" = "windows" ]; then

    if [ "$ARCH" != "amd64" ] && [ "$ARCH" != "386" ]; then
        echo "Windows only supports amd64 and 386"
        exit 1
    fi

elif [ "$OS" = "linux" ]; then

    if [ "$ARCH" != "amd64" ] && [ "$ARCH" != "arm64" ]; then
        echo "Linux only supports amd64 and arm64"
        exit 1
    fi

else
    echo "Unsupported OS: $OS"
    exit 1
fi


export GOOS=$OS
export GOARCH=$ARCH


EXT=""

if [ "$OS" = "windows" ]; then
    EXT=".exe"
fi


OUTPUT="../../ps/recovery/x2xlib/library/extra/firstboot/drfirstboot/$OS/$ARCH/drfirstboot${EXT}"


echo "Building drfirstboot..."
echo "GOOS=$GOOS"
echo "GOARCH=$GOARCH"
echo "OUTPUT=$OUTPUT"


go build \
    -trimpath \
    -o "$OUTPUT"


echo "Build success: $OUTPUT"