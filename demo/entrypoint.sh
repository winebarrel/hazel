#!/bin/sh
set -e

[ -f /demo/manual.txt ] && cat /demo/manual.txt

exec "$@"
