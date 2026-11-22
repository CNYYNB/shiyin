#!/bin/bash
# Prepare the Cloudflare Pages site for an app update.
#   usage: publish/prepare.sh "中文更新说明" [path/to/signed.apk]
# Copies the signed APK into publish/site/ as myplayer-<versionName>.apk and writes
# publish/site/version.json (versionCode/versionName from aapt badging, sha256 of the APK).
# Run it after: ANDROID_HOME=~/android-sdk ~/gradle-8.9/bin/gradle :app:assembleRelease
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$(pwd)

NOTES=${1:?usage: publish/prepare.sh "更新说明" [apk]}
APK=${2:-$ROOT/app/build/outputs/apk/release/app-release.apk}
SITE=$ROOT/publish/site
BT=${BUILD_TOOLS:-${ANDROID_HOME:-$HOME/android-sdk}/build-tools/34.0.0}
EXPECTED_CERT=48bca8fed19e1a08a95b459f9f07a1cd1e9e11f442a6b2406747f0a330901586

[ -f "$APK" ] || { echo "APK not found: $APK" >&2; exit 1; }

# badging: aapt2 first (the arm64 aapt v1 binary prints nothing for badging), then aapt
BADGING=$("$BT/aapt2" dump badging "$APK" 2>/dev/null || true)
[ -n "$BADGING" ] || BADGING=$("$BT/aapt" dump badging "$APK" 2>/dev/null || true)
PKG_LINE=$(printf '%s\n' "$BADGING" | grep -m1 "^package:" || true)
[ -n "$PKG_LINE" ] || { echo "aapt badging failed for $APK" >&2; exit 1; }
PKG=$(grep -oP "^package: name='\K[^']*" <<<"$PKG_LINE")
VCODE=$(grep -oP " versionCode='\K[0-9]+" <<<"$PKG_LINE")
VNAME=$(grep -oP " versionName='\K[^']*" <<<"$PKG_LINE")
[ "$PKG" = "com.louis.myplayer" ] || { echo "unexpected package: $PKG" >&2; exit 1; }

# signature must be the same key as 1.0, or the update can't install over it
CERT=$("$BT/apksigner" verify --print-certs "$APK" 2>/dev/null | sed -nE 's/^Signer #1 certificate SHA-256 digest: ([0-9a-f]+)$/\1/p')
[ "$CERT" = "$EXPECTED_CERT" ] || { echo "certificate mismatch: got '$CERT', expected $EXPECTED_CERT" >&2; exit 1; }

NAME="myplayer-$VNAME.apk"
mkdir -p "$SITE"
cp "$APK" "$SITE/$NAME"
SHA=$(sha256sum "$SITE/$NAME" | cut -d' ' -f1)

VCODE="$VCODE" VNAME="$VNAME" NAME="$NAME" SHA="$SHA" NOTES="$NOTES" OUT="$SITE/version.json" python3 - <<'PY'
import json, os
d = {"versionCode": int(os.environ["VCODE"]), "versionName": os.environ["VNAME"],
     "apk": os.environ["NAME"], "notes": os.environ["NOTES"], "sha256": os.environ["SHA"]}
open(os.environ["OUT"], "w", encoding="utf-8").write(json.dumps(d, ensure_ascii=False, indent=2) + "\n")
PY

# don't let Cloudflare cache version.json; serve the APK with the right type
cat > "$SITE/_headers" <<'H'
/version.json
  Cache-Control: no-cache
  Content-Type: application/json; charset=utf-8
/*.apk
  Content-Type: application/vnd.android.package-archive
H

echo "package     $PKG"
echo "version     $VNAME ($VCODE)"
echo "cert        $CERT (matches 1.0)"
echo "site        $SITE"
ls -l "$SITE"
cat "$SITE/version.json"
