#!/bin/sh
# Entrypoint for the ssl-manager-web container.
#
# The container image creates /app/data owned by the non-root "sslmanager"
# user, but when a host bind mount or named volume is attached at /app/data
# (e.g. `-v $(pwd)/ssl-manager-data:/app/data`), Docker replaces that
# directory's ownership with whatever the mounted source has - typically
# root:root, or a different uid on the host.
#
# If sslmanager can't write to /app/data, the SQLite driver
# (modernc.org/sqlite) fails with the misleading error:
#   "unable to open database file: out of memory (14)"
# which is actually a permission problem, not an OOM condition.
#
# This script runs as root at container start, fixes ownership of
# /app/data (and its subdirectories) to match the sslmanager user, then
# drops privileges via su-exec before executing the real entrypoint.
set -e

TARGET_UID="$(id -u sslmanager)"
TARGET_GID="$(id -g sslmanager)"

if [ "$(id -u)" = "0" ]; then
    # Only chown paths not already owned by the target user, to keep
    # startup fast on large data directories across restarts.
    find /app/data ! -uid "$TARGET_UID" -exec chown "$TARGET_UID:$TARGET_GID" {} + 2>/dev/null || true
    exec su-exec sslmanager:sslmanager "$@"
fi

exec "$@"
