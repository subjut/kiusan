#!/bin/sh
# Kiusan image entrypoint.
#
# Started as root (the default): makes sure the data dir exists and is owned
# by the run user, then drops privileges to it. The run user is
# KIUSAN_UID/KIUSAN_GID, defaulting to the image's own kiusan user, so a
# plain `docker run` behaves like a hardcoded USER directive would.
#
# Started as a non-root user (e.g. `docker run --user 1000`): runs as-is —
# the entrypoint can only fix ownership from root, so the data dir's
# ownership is the operator's in that mode.
set -eu

uid="${KIUSAN_UID:-$(id -u kiusan)}"
gid="${KIUSAN_GID:-$uid}"

if [ "$(id -u)" = "0" ]; then
	mkdir -p "$KIUSAN_DATA_DIR"
	chown -R "$uid:$gid" "$KIUSAN_DATA_DIR"
	exec gosu "$uid:$gid" kiusan "$@"
fi

exec kiusan "$@"
