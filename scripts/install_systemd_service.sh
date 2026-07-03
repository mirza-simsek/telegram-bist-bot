#!/usr/bin/env sh
# DEPRECATED: superseded by infra/ Docker Compose deploy. Kept only for rollback until systemd cutover is complete (see infra/DEPLOY.md).
set -eu

APP_DIR="${APP_DIR:-/opt/telegram-bist-bot}"
BIST_DATA_SCRAP_DIR="${BIST_DATA_SCRAP_DIR:-/opt/bist_data_scrap}"
BIST_DATA_SCRAP_SRC="${BIST_DATA_SCRAP_SRC:-}"
SERVICE_NAME="${SERVICE_NAME:-bistbot}"
SERVICE_USER="${SERVICE_USER:-bistbot}"
PYTHON_BIN="${PYTHON_BIN:-python3}"

if [ "$(id -u)" -ne 0 ]; then
	echo "run as root: sudo APP_DIR=$APP_DIR SERVICE_NAME=$SERVICE_NAME $0"
	exit 1
fi

if ! id "$SERVICE_USER" >/dev/null 2>&1; then
	useradd --system --home "$APP_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
fi

mkdir -p "$APP_DIR"
cp -R . "$APP_DIR"
chown -R "$SERVICE_USER:$SERVICE_USER" "$APP_DIR"

if [ -n "$BIST_DATA_SCRAP_SRC" ]; then
	mkdir -p "$BIST_DATA_SCRAP_DIR"
	cp -R "$BIST_DATA_SCRAP_SRC"/. "$BIST_DATA_SCRAP_DIR"
	chown -R "$SERVICE_USER:$SERVICE_USER" "$BIST_DATA_SCRAP_DIR"
fi

cd "$APP_DIR"

if [ ! -f .env ]; then
	cp .env.example .env
	echo "created $APP_DIR/.env; fill TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID, PYTHON_EXECUTABLE and BIST_DATA_SCRAP_DIR before starting"
fi

if [ ! -d .venv ]; then
	"$PYTHON_BIN" -m venv .venv
fi
.venv/bin/pip install --upgrade pip
.venv/bin/pip install -r requirements.txt

if command -v go >/dev/null 2>&1; then
	go build -o bistbot ./cmd/bistbot
elif [ -x ./bistbot ]; then
	chmod +x ./bistbot
else
	echo "go is not installed and no prebuilt ./bistbot binary exists"
	exit 1
fi
chown -R "$SERVICE_USER:$SERVICE_USER" "$APP_DIR"
if [ -d "$BIST_DATA_SCRAP_DIR" ]; then
	chown -R "$SERVICE_USER:$SERVICE_USER" "$BIST_DATA_SCRAP_DIR"
fi

cp deploy/systemd/bistbot.service "/etc/systemd/system/$SERVICE_NAME.service"
systemctl daemon-reload
systemctl enable "$SERVICE_NAME"

echo "installed $SERVICE_NAME"
echo "edit $APP_DIR/.env, then run: systemctl restart $SERVICE_NAME"
