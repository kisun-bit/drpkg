#!/bin/bash
set -e
for f in $(find . -type f -exec file {} \; | grep ELF | cut -d: -f1); do
    echo "Patching RPATH for $f"
    patchelf --set-rpath '$ORIGIN' "$f"
done

