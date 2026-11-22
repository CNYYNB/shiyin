#!/bin/sh
# Stands in for the NSIS installer in tests: records how it was called.
sha256sum "$1" > /tmp/shiyin-installed
