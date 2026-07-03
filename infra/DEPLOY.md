# Deploy (Docker Compose)

Canli yayin Docker Compose ile, sunucudaki bir git checkout'undan yapilir. GitHub Actions/CI yok — deploy elle, SSH uzerinden tetiklenir (door-control projesiyle ayni felsefe), tek fark sunucudaki kopyanin gercek bir `git clone` olmasi ve deploy'un `git pull` ile guncellenmesi.

## Ilk kurulum (sunucuda, tek seferlik)

1. Repo icin salt-okunur bir deploy key uret:

   ```bash
   ssh-keygen -t ed25519 -f ~/.ssh/telegram-bist-bot_deploy -N "" -C "telegram-bist-bot-deploy@$(hostname)"
   cat ~/.ssh/telegram-bist-bot_deploy.pub
   ```

   Bu public key'i GitHub'da repo Settings → Deploy keys → Add deploy key ile ekle ("Allow write access" **isaretlenmemeli**, sadece okuma yeterli).

2. Bu key'e ozel bir SSH host alias'i tanimla (sunucuda baska deploy key'lerle cakismasin diye):

   ```bash
   cat >> ~/.ssh/config <<'EOF'
   Host github-telegram-bist-bot
     HostName github.com
     User git
     IdentityFile ~/.ssh/telegram-bist-bot_deploy
     IdentitiesOnly yes
   EOF
   chmod 600 ~/.ssh/config
   ```

3. Repoyu clone'la:

   ```bash
   sudo mkdir -p /opt/telegram-bist-bot-docker
   sudo chown ubuntu:ubuntu /opt/telegram-bist-bot-docker
   git clone github-telegram-bist-bot:mirza-simsek/telegram-bist-bot.git /opt/telegram-bist-bot-docker
   ```

4. Production env dosyasini olustur ve doldur:

   ```bash
   cd /opt/telegram-bist-bot-docker
   cp infra/.env.production.example infra/.env.production
   nano infra/.env.production   # TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID, TELEGRAM_ALLOWED_CHAT_IDS gercek degerlerle doldurulur
   chmod 600 infra/.env.production
   ```

   `infra/.env.production` git'e eklenmez (`.gitignore`'da).

5. `docker`/`docker compose` sunucuda kurulu olmali (door-control icin zaten kurulu ve calisiyor).

## Gunluk deploy

```bash
cd /opt/telegram-bist-bot-docker
make -f infra/Makefile deploy
```

Bu komut: `git pull --ff-only` ile en son kodu ceker, ardindan `docker compose ... up -d --build bistbot` ile imaji yeniden derleyip container'i yeniden baslatir.

## Loglari izleme

```bash
cd /opt/telegram-bist-bot-docker
make -f infra/Makefile logs
```

## Durdurma

```bash
cd /opt/telegram-bist-bot-docker
make -f infra/Makefile down
```

## Rollback (systemd cutover tamamlanana kadar gecerli)

Eski systemd kurulumu `/opt/telegram-bist-bot` altinda, devre disi ama silinmeden duruyor:

```bash
docker compose --env-file infra/.env.production -f infra/compose.prod.yml down
sudo systemctl start bistbot
sudo systemctl status bistbot --no-pager
```

## Onemli: ayni anda iki instance calistirma

Telegram bot token'i ayni anda sadece tek bir `getUpdates` long-poll baglantisini destekler. Docker ve systemd kurulumlari **asla ayni anda ayni bot token'iyla** calismamali — aksi halde `409 Conflict: terminated by other getUpdates request` hatasi alinir ve mesajlar/komutlar rastgele kaybolur. Cutover sirasinda once systemd durdurulur, sonra Docker baslatilir (bkz. proje plani).
