#!/bin/bash
# regenerate gradle.properties with the current (short-lived) proxy credentials
cd "$(dirname "$0")"
python3 - <<'P'
import os, urllib.parse
u = urllib.parse.urlparse(os.environ["HTTPS_PROXY"])
user = urllib.parse.unquote(u.username or ""); pw = urllib.parse.unquote(u.password or "")
lines = [
 "org.gradle.jvmargs=-Xmx3g -Dfile.encoding=UTF-8 -Djdk.http.auth.tunneling.disabledSchemes= -Djdk.http.auth.proxying.disabledSchemes=",
 "org.gradle.daemon=true", "org.gradle.parallel=false", "org.gradle.caching=true",
 "android.useAndroidX=true", "kotlin.code.style=official", "android.nonTransitiveRClass=true",
 "android.aapt2FromMavenOverride=/home/sandbox/android-sdk/build-tools/34.0.0/aapt2",
]
for s in ("http","https"):
    lines += [f"systemProp.{s}.proxyHost={u.hostname}", f"systemProp.{s}.proxyPort={u.port}",
              f"systemProp.{s}.proxyUser={user}", f"systemProp.{s}.proxyPassword={pw}"]
lines.append("systemProp.http.nonProxyHosts=localhost|127.0.0.1")
open("gradle.properties","w").write("\n".join(lines)+"\n")
P
echo "sdk.dir=/home/sandbox/android-sdk" > local.properties
