#!/bin/sh
# Polls a TCP port until it accepts a connection or the timeout expires.
# Usage: wait-for-ssh.sh <host> <port> [timeout_seconds]
set -e

host="$1"
port="$2"
timeout="${3:-60}"

if [ -z "$host" ] || [ -z "$port" ]; then
    echo "usage: $0 <host> <port> [timeout_seconds]" >&2
    exit 2
fi

i=0
while [ "$i" -lt "$timeout" ]; do
    if nc -z "$host" "$port" 2>/dev/null; then
        exit 0
    fi
    i=$((i + 1))
    sleep 1
done

echo "timeout waiting for $host:$port after ${timeout}s" >&2
exit 1
