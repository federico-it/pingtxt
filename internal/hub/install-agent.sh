#!/bin/sh
# pingtxt agent installer.
#
#   curl -fsSL https://HUB/install-agent.sh | sudo sh -s -- --hub https://HUB --token pt_xxx
#   curl -fsSL https://HUB/install-agent.sh | sudo sh -s -- --uninstall
#
# Downloads the agent binary from the hub, checks its SHA-256, installs it to
# /opt/pingtxt-agent and registers a systemd service (or prints the command to
# run when systemd is missing). The service runs as the "pingtxt-agent" user,
# which owns /opt/pingtxt-agent so the agent can update itself when the hub
# is upgraded. Re-running the script upgrades older installs.
set -eu

HUB=""
TOKEN=""
UNINSTALL=0
DIR=/opt/pingtxt-agent
BIN=$DIR/pingtxt-agent
LINK=/usr/local/bin/pingtxt-agent
USER_NAME=pingtxt-agent
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
  rm -f "$UNIT" "$LINK" "$ENVFILE"
  rm -rf "$DIR"
  if id "$USER_NAME" >/dev/null 2>&1; then
    userdel "$USER_NAME" 2>/dev/null || deluser "$USER_NAME" 2>/dev/null || true
  fi
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

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  else
    wget -qO "$2" "$1"
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

URL="$HUB/download/pingtxt-agent-$OS-$ARCH"
echo "> downloading $URL"
TMP=$(mktemp)
SUM=$(mktemp)
trap 'rm -f "$TMP" "$SUM"' EXIT
fetch "$URL" "$TMP"
if fetch "$URL.sha256" "$SUM" 2>/dev/null; then
  WANT=$(cut -d' ' -f1 "$SUM")
  GOT=$(sha256 "$TMP")
  if [ -n "$GOT" ] && [ "$GOT" != "$WANT" ]; then
    echo "checksum mismatch: expected $WANT, got $GOT" >&2
    exit 1
  fi
  [ -n "$GOT" ] && echo "> checksum ok"
fi

# Dedicated user that owns the install folder, so the agent can replace its
# own binary on updates.
if ! id "$USER_NAME" >/dev/null 2>&1; then
  if command -v useradd >/dev/null 2>&1; then
    useradd --system --no-create-home --home-dir "$DIR" --shell /usr/sbin/nologin "$USER_NAME"
  else
    adduser -S -D -H -h "$DIR" -s /sbin/nologin "$USER_NAME"
  fi
fi
mkdir -p "$DIR"
install -m 0755 "$TMP" "$BIN"
chown -R "$USER_NAME:" "$DIR"
# Older installs put the binary straight in /usr/local/bin.
rm -f "$LINK"
ln -s "$BIN" "$LINK"

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
User=$USER_NAME
Group=$USER_NAME
AmbientCapabilities=CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_RAW
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=$DIR

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable pingtxt-agent
  systemctl restart pingtxt-agent
  echo "> pingtxt agent installed and running (systemctl status pingtxt-agent)"
else
  echo "> systemd not found. Start the agent with:"
  echo "  set -a; . $ENVFILE; set +a; sudo -E -u $USER_NAME $BIN"
fi
