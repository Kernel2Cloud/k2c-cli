#!/bin/sh
# Builds k2c for every supported platform into dist/: one archive each
# (tar.gz, zip for Windows) with the binary and the README, and SHA256SUMS.
# The release workflow runs it on a tag; run it locally the same way.
set -eu
version=${1:?usage: scripts/dist.sh <version, e.g. v0.1.0>}
command -v zip >/dev/null || { echo "dist.sh needs zip" >&2; exit 1; }

rm -rf dist
mkdir -p dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os=${target%/*}
  arch=${target#*/}
  # No version in the name: .../releases/latest/download/<name> always works.
  name="k2c_${os}_${arch}"
  exe=k2c
  [ "$os" = windows ] && exe=k2c.exe
  mkdir -p "dist/$name"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
    go build -trimpath -ldflags "-s -w -X main.version=$version" -o "dist/$name/$exe" ./cmd/k2c
  cp README.md "dist/$name/"
  if [ "$os" = windows ]; then
    (cd dist && zip -qr "$name.zip" "$name")
  else
    tar -C dist -czf "dist/$name.tar.gz" "$name"
  fi
  rm -rf "dist/${name:?}"
done
(cd dist && sha256sum k2c_* > SHA256SUMS)
ls -l dist
