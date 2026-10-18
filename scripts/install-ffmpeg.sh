#!/bin/sh
# Install pinned static ffmpeg and ffprobe binaries, for pkg/vpdq's tests.
#
# vPDQ hashes depend on the ffmpeg build that decodes the video, so CI pins
# one, by the checksums of both the downloads and the binaries in them.
#
#   INSTALL_PREFIX  Directory to install into. Default /usr/local/bin; use a
#                   writable one such as $HOME/.local/bin when unprivileged.
#   FFMPEG_ARCH     Override the detected architecture (amd64 or arm64).
#
# Idempotent: exits 0 without downloading if the pinned binaries are present.
set -eu

INSTALL_PREFIX="${INSTALL_PREFIX:-/usr/local/bin}"

if [ -z "${FFMPEG_ARCH:-}" ]; then
    case "$(uname -m)" in
        x86_64 | amd64) FFMPEG_ARCH="amd64" ;;
        aarch64 | arm64) FFMPEG_ARCH="arm64" ;;
        *)
            echo "unsupported architecture $(uname -m); no pinned ffmpeg build" >&2
            exit 1
            ;;
    esac
fi

# Each architecture is an independent upstream build with its own build ID.
FFMPEG_VERSION="9.0.2"
case "${FFMPEG_ARCH}" in
    amd64)
        FFMPEG_BUILD_ID="1789931100"
        FFMPEG_SHA256="fa8ecf4abbd290d98f7d188b8649cc6b391ae209a98452be955a15aab1909d7f"
        FFPROBE_SHA256="3f428c49070be3d24ec338602b76d412e401ffcb8a5641ef0e729181a232fc32"
        FFMPEG_BIN_SHA256="2624cfce1acdb259fe72b6d020dac937a1e7e66cf3578d8c4d43704e69a42cba"
        FFPROBE_BIN_SHA256="fa300ee31cd55e7a8d052f16d017fde3b68d842dff39e0c29575ced5278947d6"
        ;;
    arm64)
        FFMPEG_BUILD_ID="1789931697"
        FFMPEG_SHA256="93a76ae90db5474eecdf951a729857c64f3de23567228d6a7d5e6e8e3cd1021b"
        FFPROBE_SHA256="bcbe80fb741c180083327afaf5434812e006b33cacde2016b9aeaf6936128330"
        FFMPEG_BIN_SHA256="d8802725e54217a3fcb22dfb14d95cb648c0383685802bb155091575cf44a3ff"
        FFPROBE_BIN_SHA256="814bee3ef4bd807ec3c0baa0dd83acf6f7d399d7df4fe14218f5492211183e76"
        ;;
    *)
        echo "unsupported FFMPEG_ARCH=${FFMPEG_ARCH} (expected amd64 or arm64)" >&2
        exit 1
        ;;
esac

# verify FILE SHA256 [quiet]
verify() {
    if [ ! -f "$1" ]; then
        [ -n "${3:-}" ] || printf '%s: not found\n' "$1" >&2
        return 1
    fi
    sum=$(sha256sum <"$1") || {
        printf '%s: sha256sum failed\n' "$1" >&2
        return 1
    }
    [ "${sum%% *}" = "$2" ] && return 0
    [ -n "${3:-}" ] || printf '%s: sha256 %s does not match the pinned %s\n' "$1" "${sum%% *}" "$2" >&2
    return 1
}
if verify "${INSTALL_PREFIX}/ffmpeg" "${FFMPEG_BIN_SHA256}" quiet &&
    verify "${INSTALL_PREFIX}/ffprobe" "${FFPROBE_BIN_SHA256}" quiet; then
    echo "ffmpeg/ffprobe ${FFMPEG_VERSION} already installed at ${INSTALL_PREFIX}"
    exit 0
fi

# Create the prefix unprivileged when its parent allows it, so a missing
# $HOME/.local/bin does not force sudo.
mkdir -p "${INSTALL_PREFIX}" 2>/dev/null || true

SUDO=""
if [ ! -w "${INSTALL_PREFIX}" ] && [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
    else
        echo "${INSTALL_PREFIX} not writable and sudo unavailable" >&2
        exit 1
    fi
fi

command -v unzip >/dev/null 2>&1 || {
    echo "unzip not found" >&2
    exit 1
}

BASE_URL="https://ffmpeg.martin-riedl.de/download/linux/${FFMPEG_ARCH}/${FFMPEG_BUILD_ID}_${FFMPEG_VERSION}"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

curl -fsSL "${BASE_URL}/ffmpeg.zip" -o "${WORK}/ffmpeg.zip"
curl -fsSL "${BASE_URL}/ffprobe.zip" -o "${WORK}/ffprobe.zip"
verify "${WORK}/ffmpeg.zip" "${FFMPEG_SHA256}"
verify "${WORK}/ffprobe.zip" "${FFPROBE_SHA256}"

unzip -q "${WORK}/ffmpeg.zip" -d "${WORK}/bin"
unzip -q "${WORK}/ffprobe.zip" -d "${WORK}/bin"
verify "${WORK}/bin/ffmpeg" "${FFMPEG_BIN_SHA256}"
verify "${WORK}/bin/ffprobe" "${FFPROBE_BIN_SHA256}"
${SUDO} mkdir -p "${INSTALL_PREFIX}"
${SUDO} install -m 755 "${WORK}/bin/ffmpeg" "${WORK}/bin/ffprobe" "${INSTALL_PREFIX}/"

echo "installed ffmpeg/ffprobe ${FFMPEG_VERSION} (${FFMPEG_ARCH}) to ${INSTALL_PREFIX}"
