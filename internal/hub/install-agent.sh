#!/bin/sh
# pingtxt agent installer.
#
#   curl -fsSL https://HUB/install-agent.sh | sudo sh -s -- --hub https://HUB --token pt_xxx
#   curl -fsSL https://HUB/install-agent.sh | sudo sh -s -- --uninstall
#
# Downloads the agent binary from the hub, installs it to /usr/local/bin and
# registers a systemd service (or prints the command to run when systemd is missing).
set -eu

HUB=""
TOKEN=""
UNINSTALL=0
BIN=/usr/local/bin/pingtxt-agent
UNIT=/etc/systemd/system/pingtxt-agent.service
ENVFILE=/etc/pingtxt-agent.env

has_systemd() { [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --hub) HUB="$2"; shift 2 ;;
    --token) TOKEN="$2"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h|--help) sed -n '2,9p' "$0" 2>/dev/null || true; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then
  echo "please run as root (sudo)" >&2
  exit 1
fi

if [ "$UNINSTALL" -eq 1 ]; then
  if has_systemd; then
    systemctl disable --now pingtxt-agent 2>/dev/null || true
  fi
  rm -f "$UNIT" "$BIN" "$ENVFILE"
  if has_systemd; then systemctl daemon-reload; fi
  echo "pingtxt agent removed"
  exit 0
fi

[ -n "$HUB" ] || { echo "--hub is required" >&2; exit 1; }
[ -n "$TOKEN" ] || { echo "--token is required" >&2; exit 1; }
HUB=$(echo "$HUB" | sed 's#/*$##')

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l|armv6l|arm) ARCH=arm ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

URL="$HUB/download/pingtxt-agent-$OS-$ARCH"
echo "> downloading $URL"
TMP=$(mktemp)
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$URL" -o "$TMP"
else
  wget -qO "$TMP" "$URL"
fi
install -m 0755 "$TMP" "$BIN"
rm -f "$TMP"

umask 077
cat > "$ENVFILE" <<EOF
PINGTXT_HUB=$HUB
PINGTXT_TOKEN=$TOKEN
EOF

if has_systemd; then
  cat > "$UNIT" <<EOF
[Unit]
Description=pingtxt monitoring agent
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=$ENVFILE
ExecStart=$BIN
Restart=always
RestartSec=5
DynamicUser=yes
AmbientCapabilities=CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_RAW
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now pingtxt-agent
  echo "> pingtxt agent installed and running (systemctl status pingtxt-agent)"
else
  echo "> systemd not found. Start the agent with:"
  echo "  set -a; . $ENVFILE; set +a; $BIN"
fi
