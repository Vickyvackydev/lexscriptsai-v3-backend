#!/bin/bash
# ==============================================================================
# LexScripts AI v3 Droplet Provisioning & Setup Script (Systemd + Nginx)
# ==============================================================================
set -e

echo "==> [1/6] Updating system packages & installing dependencies..."
sudo apt update
sudo apt install -y curl git nginx ufw certbot python3-certbot-nginx rsync ffmpeg

# Install yt-dlp binary system-wide
sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp
sudo chmod a+rx /usr/local/bin/yt-dlp

echo "==> [2/6] Setting up application directories..."
sudo mkdir -p /var/www/staging.v3.lexscriptsai.com
sudo mkdir -p /var/www/lexscriptsai.com
sudo mkdir -p /var/www/lexscripts-backend-staging
sudo mkdir -p /var/www/lexscripts-backend-prod

# Ensure www-data permissions
sudo chown -R www-data:www-data /var/www/
sudo chmod -R 755 /var/www/

echo "==> [3/6] Installing systemd service units..."
sudo cp /var/www/lexscripts-backend-staging/deploy/systemd/lexscripts-backend-staging.service /etc/systemd/system/ 2>/dev/null || \
  sudo cp deploy/systemd/lexscripts-backend-staging.service /etc/systemd/system/
sudo cp /var/www/lexscripts-backend-prod/deploy/systemd/lexscripts-backend-prod.service /etc/systemd/system/ 2>/dev/null || \
  sudo cp deploy/systemd/lexscripts-backend-prod.service /etc/systemd/system/

sudo systemctl daemon-reload
sudo systemctl enable lexscripts-backend-staging
sudo systemctl enable lexscripts-backend-prod

echo "==> [4/6] Configuring Nginx reverse proxy..."
sudo cp deploy/nginx/staging.v3.lexscriptsai.com.conf /etc/nginx/sites-available/
sudo cp deploy/nginx/lexscriptsai.com.conf /etc/nginx/sites-available/

sudo ln -sf /etc/nginx/sites-available/staging.v3.lexscriptsai.com.conf /etc/nginx/sites-enabled/
sudo ln -sf /etc/nginx/sites-available/lexscriptsai.com.conf /etc/nginx/sites-enabled/

# Remove default site if exists
sudo rm -f /etc/nginx/sites-enabled/default

sudo nginx -t
sudo systemctl restart nginx

echo "==> [5/6] Configuring Firewall (UFW)..."
sudo ufw allow 'Nginx Full'
sudo ufw allow OpenSSH
sudo ufw --force enable

echo "==> [6/6] Setup complete!"
echo "Next steps:"
echo "1. Put .env in /var/www/lexscripts-backend-staging/.env and /var/www/lexscripts-backend-prod/.env"
echo "2. Run Certbot for SSL:"
echo "   sudo certbot --nginx -d staging.v3.lexscriptsai.com"
echo "   sudo certbot --nginx -d lexscriptsai.com -d www.lexscriptsai.com"
echo "3. Push to GitHub to trigger the automated CI/CD deployment workflows!"
