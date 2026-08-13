#!/usr/bin/env bash
# Build a DSM 7.2 x86_64 .spk for black/official Synology (manual install).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SPK_SRC="${ROOT}/synology/spk"
DIST="${ROOT}/dist"
VERSION="${PILOTSERVER_SPK_VERSION:-1.0.19-1}"
PKG_NAME="pilotserver"
ARCH="x64"
# Avoid macOS AppleDouble (._*) files breaking DSM wizard parsing.
export COPYFILE_DISABLE=1
export COPY_EXTENDED_ATTRIBUTES_DISABLE=1
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/pilotserver-spk.XXXXXX")"
cleanup() { rm -rf "${STAGE}"; }
trap cleanup EXIT

echo "==> cross-compile linux/amd64"
mkdir -p "${STAGE}/package/bin"
(
	cd "${ROOT}"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -ldflags="-s -w" -o "${STAGE}/package/bin/pilotserver" ./cmd/pilotserver
)

# Minimal DSM UI placeholder (opens admin via adminurl)
mkdir -p "${STAGE}/package/ui"
cat >"${STAGE}/package/ui/config" <<EOF
{
  ".url": {
    "com.pilotserver": {
      "title": "PilotServer",
      "desc": "OpenPilot self-hosted server",
      "icon": "images/pilotserver-{0}.png",
      "type": "url",
      "protocol": "http",
      "port": "18780",
      "url": "/admin/",
      "allUsers": true
    }
  }
}
EOF
mkdir -p "${STAGE}/package/ui/images"
# 1x1 PNG placeholder icons (DSM expects some sizes; empty files may warn)
python3 - "${STAGE}/package/ui/images" <<'PY'
import struct, zlib, sys, os
out = sys.argv[1]
os.makedirs(out, exist_ok=True)

def png(w, h, rgba=(0x2F, 0x6F, 0xED, 255)):
    def chunk(tag, data):
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xffffffff)
    raw = b"".join(b"\x00" + bytes(rgba) * w for _ in range(h))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 6, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")

for size in (16, 24, 32, 48, 64, 72, 256):
    open(os.path.join(out, f"pilotserver-{size}.png"), "wb").write(png(size, size))
PY

echo "==> assemble package.tgz"
tar -C "${STAGE}/package" -czf "${STAGE}/package.tgz" .

echo "==> assemble spk root"
mkdir -p "${STAGE}/spk/scripts" "${STAGE}/spk/conf" "${STAGE}/spk/WIZARD_UIFILES"
cp "${STAGE}/package.tgz" "${STAGE}/spk/"
sed "s/REPLACE_VERSION/${VERSION}/" "${SPK_SRC}/INFO.in" >"${STAGE}/spk/INFO"
cp "${SPK_SRC}/conf/privilege" "${STAGE}/spk/conf/"
# Do NOT ship conf/resource port-config: DSM7 expects a .sc under package target;
# a wrong protocol-file path/format aborts install after the wizard.
# Copy wizard/scripts without AppleDouble junk
find "${SPK_SRC}/WIZARD_UIFILES" -type f ! -name '._*' -exec cp {} "${STAGE}/spk/WIZARD_UIFILES/" \;
find "${SPK_SRC}/scripts" -type f ! -name '._*' -exec cp {} "${STAGE}/spk/scripts/" \;
# DSM scripts must be LF
find "${STAGE}/spk/scripts" -type f -exec sed -i '' $'s/\r$//' {} +
find "${STAGE}/spk/WIZARD_UIFILES" -type f -exec sed -i '' $'s/\r$//' {} +
chmod 755 "${STAGE}/spk/scripts/"*
# INFO must not contain comments
grep -v '^[[:space:]]*#' "${STAGE}/spk/INFO" > "${STAGE}/spk/INFO.clean" || true
mv "${STAGE}/spk/INFO.clean" "${STAGE}/spk/INFO"

# checksums (DSM expects PACKAGE_SHA256 or older checksum sometimes)
(
	cd "${STAGE}/spk"
	sha256sum package.tgz | awk '{print $1}' > PACKAGE_SHA256
)

OUT="${DIST}/${PKG_NAME}-${VERSION}-${ARCH}.spk"
mkdir -p "${DIST}"
tar -C "${STAGE}/spk" --exclude='._*' --exclude='.DS_Store' -cf "${OUT}" \
	INFO PACKAGE_SHA256 package.tgz scripts conf WIZARD_UIFILES

echo "==> built ${OUT}"
tar -tf "${OUT}" | head -40
ls -lh "${OUT}"
