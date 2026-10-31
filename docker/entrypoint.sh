#!/bin/sh
# Starts pingtxt (hub or agent) as the unprivileged "pingtxt" user.
#
# The container starts as root only for this script: it gives the data
# folder to pingtxt (volumes of older versions were written by root), lets
# pingtxt use a mounted Docker socket, then drops to pingtxt and runs the
# program with no capabilities except CAP_NET_RAW for ICMP ping (when the
# container has it). With "user:" or "--user" set, the program runs as that
# user and this script only execs it.
set -eu

user=pingtxt

if [ "$(id -u)" != 0 ]; then
	exec "$@"
fi

warn() { echo "pingtxt entrypoint: $*" >&2; }

# Data folders: the hub database and backups, the agent's self-updates.
for dir in "${PINGTXT_DATA:-}" "${PINGTXT_UPDATE_DIR:-}"; do
	case "$dir" in
	/?*) ;;
	*) continue ;;
	esac
	mkdir -p "$dir"
	if [ -n "$(find "$dir" ! -user "$user" -print -quit 2>/dev/null)" ]; then
		warn "changing the owner of $dir to $user (once, after the upgrade to the non-root image)"
		if ! chown -R "$user:$user" "$dir"; then
			warn "cannot change the owner of $dir: running as root as before. Fix it with: chown -R 10001:10001 <the folder on the host>"
			exec "$@"
		fi
	fi
done

# Docker socket for "docker container" monitors: join its group.
sock="${DOCKER_HOST:-unix:///var/run/docker.sock}"
sock="${sock#unix://}"
if [ -S "$sock" ]; then
	gid="$(stat -c %g "$sock")"
	group="$(awk -F: -v gid="$gid" '$3 == gid { print $1; exit }' /etc/group)"
	if [ -z "$group" ]; then
		group=docker-host
		addgroup -g "$gid" "$group"
	fi
	addgroup "$user" "$group" 2>/dev/null || true
fi

# Keep CAP_NET_RAW (raw ICMP sockets) only when the container has it;
# without it pingtxt uses unprivileged ICMP sockets (net.ipv4.ping_group_range).
caps=-all
bnd="$(awk '/^CapBnd:/ { print $2 }' /proc/self/status)"
if [ $((0x$bnd >> 13 & 1)) = 1 ]; then
	caps=-all,+net_raw
fi

export HOME=/data
exec setpriv --reuid="$user" --regid="$user" --init-groups --no-new-privs \
	--bounding-set="$caps" --inh-caps="$caps" --ambient-caps="$caps" -- "$@"
