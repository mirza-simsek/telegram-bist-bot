#!/usr/bin/env sh
# DEPRECATED: superseded by infra/ Docker Compose deploy. Kept only for rollback until systemd cutover is complete (see infra/DEPLOY.md).
set -eu

APP_DIR="${APP_DIR:-/opt/telegram-bist-bot}"
BIST_DATA_SCRAP_DIR="${BIST_DATA_SCRAP_DIR:-/opt/bist_data_scrap}"
BIST_DATA_SCRAP_SRC="${BIST_DATA_SCRAP_SRC:-/Users/mirzasimsek/Developer/github/bist_data_scrap}"
REMOTE_ARCH="${REMOTE_ARCH:-auto}"
REMOTE_OS="${REMOTE_OS:-linux}"
SSH_USER="${SSH_USER:-ubuntu}"
SSH_HOST="${SSH_HOST:-}"
SSH_KEY="${SSH_KEY:-}"
SERVICE_NAME="${SERVICE_NAME:-bistbot}"
SERVICE_USER="${SERVICE_USER:-bistbot}"

if [ -z "$SSH_HOST" ]; then
	echo "usage: SSH_HOST=<oracle-ip> [SSH_USER=ubuntu|opc] [SSH_KEY=~/.ssh/id_rsa] [REMOTE_ARCH=auto|arm64|amd64] $0"
	exit 1
fi

if [ ! -d "$BIST_DATA_SCRAP_SRC" ]; then
	echo "BIST_DATA_SCRAP_SRC not found: $BIST_DATA_SCRAP_SRC"
	exit 1
fi

ssh_args=""
scp_args=""
if [ -n "$SSH_KEY" ]; then
	ssh_args="-i $SSH_KEY"
	scp_args="-i $SSH_KEY"
fi

remote="${SSH_USER}@${SSH_HOST}"
stamp="$(date +%Y%m%d%H%M%S)"
work_dir="$(mktemp -d)"
archive="$work_dir/bistbot-deploy-$stamp.tar.gz"
stage="$work_dir/stage"
mkdir -p "$stage/telegram-bist-bot" "$stage/bist_data_scrap"

cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT

if [ "$REMOTE_ARCH" = "auto" ]; then
	echo "detecting remote architecture..."
	remote_uname="$(ssh $ssh_args "$remote" 'uname -m')"
	case "$remote_uname" in
		aarch64|arm64)
			REMOTE_ARCH="arm64"
			;;
		x86_64|amd64)
			REMOTE_ARCH="amd64"
			;;
		*)
			echo "unsupported remote architecture: $remote_uname"
			exit 1
			;;
	esac
fi

echo "building $REMOTE_OS/$REMOTE_ARCH binary..."
GOOS="$REMOTE_OS" GOARCH="$REMOTE_ARCH" CGO_ENABLED=0 go build -o "$stage/telegram-bist-bot/bistbot" ./cmd/bistbot

echo "staging telegram bot files..."
rsync -a \
	--exclude '.git/' \
	--exclude '.idea/' \
	--exclude '.venv/' \
	--exclude 'build/' \
	--exclude 'logs/' \
	--exclude '__pycache__/' \
	--exclude 'bistbot' \
	./ "$stage/telegram-bist-bot/"
chmod +x "$stage/telegram-bist-bot/bistbot"

if [ -f .env ]; then
	cp .env "$stage/telegram-bist-bot/.env"
else
	cp .env.example "$stage/telegram-bist-bot/.env"
fi

tmp_env="$stage/telegram-bist-bot/.env.tmp"
awk -v py="$APP_DIR/.venv/bin/python" -v bridge="scripts/bist_data_scrap_bridge.py" -v data_dir="$BIST_DATA_SCRAP_DIR" '
BEGIN {
	seen_py=0; seen_bridge=0; seen_data=0; seen_workers=0
}
$0 ~ /^PYTHON_EXECUTABLE=/ {
	print "PYTHON_EXECUTABLE=" py; seen_py=1; next
}
$0 ~ /^PYTHON_SCANNER_SCRIPT=/ {
	print "PYTHON_SCANNER_SCRIPT=" bridge; seen_bridge=1; next
}
$0 ~ /^BIST_DATA_SCRAP_DIR=/ {
	print "BIST_DATA_SCRAP_DIR=" data_dir; seen_data=1; next
}
$0 ~ /^PYTHON_SCANNER_WORKERS=/ {
	print "PYTHON_SCANNER_WORKERS=4"; seen_workers=1; next
}
{ print }
END {
	if (!seen_py) print "PYTHON_EXECUTABLE=" py
	if (!seen_bridge) print "PYTHON_SCANNER_SCRIPT=" bridge
	if (!seen_data) print "BIST_DATA_SCRAP_DIR=" data_dir
	if (!seen_workers) print "PYTHON_SCANNER_WORKERS=4"
}
' "$stage/telegram-bist-bot/.env" > "$tmp_env"
mv "$tmp_env" "$stage/telegram-bist-bot/.env"

echo "staging bist_data_scrap files..."
rsync -a \
	--exclude '.git/' \
	--exclude '.idea/' \
	--exclude '.venv/' \
	--exclude '__pycache__/' \
	"$BIST_DATA_SCRAP_SRC"/ "$stage/bist_data_scrap/"

tar -C "$stage" -czf "$archive" .

echo "uploading archive to $remote..."
scp $scp_args "$archive" "$remote:/tmp/bistbot-deploy-$stamp.tar.gz"

echo "installing service on remote host..."
ssh $ssh_args "$remote" "APP_DIR='$APP_DIR' BIST_DATA_SCRAP_DIR='$BIST_DATA_SCRAP_DIR' SERVICE_NAME='$SERVICE_NAME' SERVICE_USER='$SERVICE_USER' DEPLOY_ARCHIVE='/tmp/bistbot-deploy-$stamp.tar.gz' sh -s" <<'REMOTE'
set -eu

APP_DIR="${APP_DIR:-/opt/telegram-bist-bot}"
BIST_DATA_SCRAP_DIR="${BIST_DATA_SCRAP_DIR:-/opt/bist_data_scrap}"
SERVICE_NAME="${SERVICE_NAME:-bistbot}"
SERVICE_USER="${SERVICE_USER:-bistbot}"
DEPLOY_ARCHIVE="${DEPLOY_ARCHIVE:?DEPLOY_ARCHIVE is required}"
TMP_DIR="/tmp/bistbot-deploy-extract"

if command -v apt-get >/dev/null 2>&1; then
	sudo apt-get update
	sudo DEBIAN_FRONTEND=noninteractive apt-get install -y python3 python3-venv python3-pip ca-certificates tar
elif command -v dnf >/dev/null 2>&1; then
	sudo dnf install -y python3 python3-pip ca-certificates tar
elif command -v yum >/dev/null 2>&1; then
	sudo yum install -y python3 python3-pip ca-certificates tar
fi

if ! id "$SERVICE_USER" >/dev/null 2>&1; then
	sudo useradd --system --home "$APP_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
fi

sudo rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR"
tar -xzf "$DEPLOY_ARCHIVE" -C "$TMP_DIR"

sudo mkdir -p "$APP_DIR" "$BIST_DATA_SCRAP_DIR"
sudo find "$APP_DIR" -mindepth 1 -maxdepth 1 ! -name '.venv' -exec rm -rf {} +
sudo find "$BIST_DATA_SCRAP_DIR" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
sudo cp -a "$TMP_DIR/telegram-bist-bot"/. "$APP_DIR"/
sudo cp -a "$TMP_DIR/bist_data_scrap"/. "$BIST_DATA_SCRAP_DIR"/

if [ ! -d "$APP_DIR/.venv" ]; then
	sudo python3 -m venv "$APP_DIR/.venv"
fi
sudo "$APP_DIR/.venv/bin/pip" install --upgrade pip
sudo "$APP_DIR/.venv/bin/pip" install -r "$APP_DIR/requirements.txt"

sudo chmod +x "$APP_DIR/bistbot"
sudo cp "$APP_DIR/deploy/systemd/bistbot.service" "/etc/systemd/system/$SERVICE_NAME.service"
sudo chown -R "$SERVICE_USER:$SERVICE_USER" "$APP_DIR" "$BIST_DATA_SCRAP_DIR"
sudo systemctl daemon-reload
sudo systemctl enable "$SERVICE_NAME"
sudo systemctl restart "$SERVICE_NAME"
sudo systemctl status "$SERVICE_NAME" --no-pager
REMOTE

echo "remote deploy completed"
echo "next local step after confirming Telegram works: ./scripts/stop_screen.sh"
