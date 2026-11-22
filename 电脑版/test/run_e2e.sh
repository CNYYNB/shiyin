#!/bin/bash
# Runs the Playwright e2e suites against a Linux build of the app.
# Usage: test/run_e2e.sh [e2e.py|e2e_v2.py|e2e_v21.py ...]   (default: all)
set -uo pipefail
cd "$(dirname "$0")/.."
export PATH=/workspace/tools/go/bin:$PATH GOPATH=/workspace/tools/gopath GOMODCACHE=/workspace/tools/gopath/pkg/mod GOCACHE=/workspace/tools/gocache
go build -o /tmp/shiyin-linux . || exit 1
SUITES=("$@"); [ ${#SUITES[@]} -eq 0 ] && SUITES=(e2e.py e2e_v2.py e2e_v21.py e2e_v24.py e2e_v30.py)
rc=0
export SHIYIN_NP_DIR=/tmp/shiyin-np; rm -rf $SHIYIN_NP_DIR
for s in "${SUITES[@]}"; do
  pkill -f fake_phone.py; pkill -f shiyin-linux; pkill -f fake_update.py; sleep 0.3
  rm -rf test/data test/pc; mkdir -p test/data test/pc; cp test/lib/* test/pcinit/* test/pc/
  [ "$s" != e2e_v21.py ] && python3 test/fake_phone.py test/phone > /tmp/phone.log 2>&1 &  # v21: the fake phone only dials out
  EXTRA=""
  if [ "$s" = e2e_v2.py ]; then
    mkdir -p /tmp/shiyin-extra; rm -rf /tmp/shiyin-extra/*; cp test/extra/* /tmp/shiyin-extra/
    python3 test/fake_update.py > /tmp/upd.log 2>&1 &
    export SHIYIN_UPDATE_URL=http://127.0.0.1:18599 SHIYIN_ITUNES_URL=http://127.0.0.1:18599/itunes SHIYIN_LRCLIB_URL=http://127.0.0.1:18599/lrclib SHIYIN_NO_CN_LYRICS=1 SHIYIN_TEST_INSTALL_CMD=$PWD/test/fake_install.sh; rm -f /tmp/shiyin-installed; rm -rf /tmp/ShiyinUpdate
  fi
  SHIYIN_DATA=$PWD/test/data SHIYIN_MUSIC=$PWD/test/pc SHIYIN_PORT=18537 SHIYIN_HEADLESS=1 /tmp/shiyin-linux > /tmp/app.log 2>&1 &
  sleep 1.5
  python3 test/$s || rc=1
  unset SHIYIN_UPDATE_URL SHIYIN_ITUNES_URL SHIYIN_LRCLIB_URL SHIYIN_NO_CN_LYRICS SHIYIN_TEST_INSTALL_CMD
done
pkill -f fake_phone.py; pkill -f shiyin-linux; pkill -f fake_update.py
exit $rc
