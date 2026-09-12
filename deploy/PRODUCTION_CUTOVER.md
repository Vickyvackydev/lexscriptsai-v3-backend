# LexScriptsAI V3 — Production Cutover & Deployment Guide

This guide contains the exact steps and commands to transition `lexscriptsai.com` from Version 2 to Version 3 on your DigitalOcean droplet (`137.184.69.56`).

---

## Prerequisites (Before Cutover)

1. Ensure the **V3 Production Backend** is running on port `8080`:
   ```bash
   sudo systemctl status lexscripts-backend-prod
   ```
   *(If not running, start it: `sudo systemctl start lexscripts-backend-prod && sudo systemctl enable lexscripts-backend-prod`)*

2. Ensure the **V3 Production Frontend** files are built and deployed to `/var/www/lexscriptsai.com`:
   ```bash
   ls -la /var/www/lexscriptsai.com
   ```

---

## Step 1: Update DNS Records

In your DNS provider (e.g., Cloudflare, Namecheap, GoDaddy):
- Update the **A record** for `lexscriptsai.com` to point to:
  ```text
  137.184.69.56
  ```
- Update the **A record** for `www.lexscriptsai.com` to point to:
  ```text
  137.184.69.56
  ```
> **Note for Cloudflare users**: When initially obtaining the Let's Encrypt SSL certificate via Certbot, set the proxy status to **DNS Only** (grey cloud), or use HTTP verification.

---

## Step 2: Issue SSL Certificate for Production Domain

SSH into your Droplet and run Certbot:

```bash
sudo certbot --nginx -d lexscriptsai.com -d www.lexscriptsai.com
```

Certbot will automatically verify the domain, generate certificates under `/etc/letsencrypt/live/lexscriptsai.com/`, and configure SSL renewal.

---

## Step 3: Install the 2GB Production Nginx Configuration

Run this command on your droplet to write the optimized production Nginx site configuration (with 2GB upload support, direct body streaming, and 30-minute upload timeouts):

```bash
sudo tee /etc/nginx/sites-available/lexscriptsai.com.conf > /dev/null << 'EOF'
# Nginx configuration for lexscriptsai.com (Production)

# HTTP - Redirect to HTTPS
server {
    listen 80;
    listen [::]:80;
    server_name lexscriptsai.com www.lexscriptsai.com;
    return 301 https://$host$request_uri;
}

# HTTPS - Production Environment
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name lexscriptsai.com www.lexscriptsai.com;

    # SSL configuration (managed by Certbot)
    ssl_certificate /etc/letsencrypt/live/lexscriptsai.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/lexscriptsai.com/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    # Frontend static files (Vite build)
    root /var/www/lexscriptsai.com;
    index index.html;

    # Allow up to 2GB files
    client_max_body_size 2048M;

    # Gzip compression
    gzip on;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript;

    # Frontend SPA routing
    location / {
        try_files $uri $uri/ /index.html;
    }

    # Static assets cache
    location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)$ {
        expires 30d;
        add_header Cache-Control "public, no-transform";
    }

    # Backend REST API reverse proxy (Port 8080 for production backend)
    location /api/ {
        proxy_pass http://127.0.0.1:8080/api/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Large file uploads support up to 2GB
        client_max_body_size 2048M;
        client_body_buffer_size 1M;
        client_body_timeout 1800s;
        proxy_read_timeout 1800s;
        proxy_connect_timeout 300s;
        proxy_send_timeout 1800s;
        proxy_request_buffering off;
        proxy_buffering off;
    }

    # WebSocket reverse proxy for real-time collaborative editing
    location /api/ws {
        proxy_pass http://127.0.0.1:8080/api/ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
}
EOF
```

---

## Step 4: Enable Production Site and Reload Nginx

Activate the site by linking it into `sites-enabled`, verify syntax, and reload:

```bash
sudo ln -sf /etc/nginx/sites-available/lexscriptsai.com.conf /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

---

## Step 5: Verification Checklist

1. Visit `https://lexscriptsai.com` in your browser.
2. Confirm the SSL certificate is valid and shows the secure padlock.
3. Test uploading a large audio/video file (>10MB and up to 2GB) to verify end-to-end upload streaming.
4. Check backend production service logs for clean requests:
   ```bash
   sudo journalctl -u lexscripts-backend-prod -f
   ```
