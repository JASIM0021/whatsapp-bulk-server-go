# 🚀 Simple Deploy from Mac to AWS

**Build on Mac, Run on AWS - No server pressure!**

---

## ⚡ Super Quick (Automated)

One command to build, upload, and deploy:

```bash
./quick-deploy.sh YOUR_AWS_IP
```

Example:
```bash
./quick-deploy.sh 54.123.45.67
```

This will:
1. ✅ Build Linux binary on your Mac (using Docker)
2. ✅ Upload to AWS
3. ✅ Setup directories
4. ✅ Start with PM2
5. ✅ Done! 🎉

**Requirements:**
- Docker Desktop running on Mac
- SSH access to AWS server
- AWS server has Node.js installed (for PM2)

---

## 📦 Manual (Step by Step)

### Step 1: Build on Mac

```bash
./build-for-linux.sh
```

This creates a `server` binary (Linux-compatible, built on Mac using Docker).

### Step 2: Upload to AWS

```bash
# Replace YOUR_AWS_IP with your server IP
scp server ubuntu@YOUR_AWS_IP:/home/ubuntu/whatsapp-backend/
scp .env.production ubuntu@YOUR_AWS_IP:/home/ubuntu/whatsapp-backend/.env
scp ecosystem.config.js ubuntu@YOUR_AWS_IP:/home/ubuntu/whatsapp-backend/
```

### Step 3: Setup on AWS

```bash
# SSH into your server
ssh ubuntu@YOUR_AWS_IP

# Navigate to directory
cd /home/ubuntu/whatsapp-backend

# Make executable
chmod +x server

# Update environment
nano .env
# Change FRONTEND_URL to your domain
```

### Step 4: Run with PM2

```bash
# Install PM2 (one-time)
sudo npm install -g pm2

# Start service
pm2 start ecosystem.config.js --env production

# Save PM2 config
pm2 save

# Enable auto-start on boot
pm2 startup
# Run the command it prints

# Check status
pm2 status

# View logs
pm2 logs whatsapp-bulk-backend
```

---

## 🎯 PM2 Commands

```bash
# Status
pm2 status

# Logs (real-time)
pm2 logs whatsapp-bulk-backend

# Restart
pm2 restart whatsapp-bulk-backend

# Stop
pm2 stop whatsapp-bulk-backend

# Delete
pm2 delete whatsapp-bulk-backend

# Monitor
pm2 monit
```

---

## 🔄 Update Deployment

When you make changes:

```bash
# 1. Build new version on Mac
./build-for-linux.sh

# 2. Upload to AWS
scp server ubuntu@YOUR_AWS_IP:/home/ubuntu/whatsapp-backend/

# 3. Restart on AWS
ssh ubuntu@YOUR_AWS_IP 'pm2 restart whatsapp-bulk-backend'
```

Or use the quick script:
```bash
./quick-deploy.sh YOUR_AWS_IP
```

---

## 📋 Prerequisites

### On Mac:
- ✅ Docker Desktop (running)
- ✅ This repository

### On AWS Server:
- ✅ Ubuntu 20.04+ or similar
- ✅ Node.js 18+ (for PM2)
- ✅ SSH access

### Install Node.js on AWS (if not installed):
```bash
curl -fsSL https://deb.nodesource.com/setup_18.x | sudo -E bash -
sudo apt-get install -y nodejs
node --version  # Should show v18.x or higher
```

---

## 🐳 Docker Setup (One-time)

1. **Download Docker Desktop:**
   - https://www.docker.com/products/docker-desktop

2. **Install and start Docker Desktop**

3. **Verify Docker is running:**
   ```bash
   docker --version
   ```

That's it! The build script will use Docker automatically.

---

## 🔍 Troubleshooting

### "Docker is not running"
- Open Docker Desktop
- Wait for it to fully start (whale icon in menu bar)
- Try again

### "Permission denied" on AWS
```bash
# Fix permissions
ssh ubuntu@YOUR_AWS_IP 'sudo chown -R ubuntu:ubuntu /home/ubuntu/whatsapp-backend'
```

### "Port 4000 already in use"
```bash
# Kill existing process
ssh ubuntu@YOUR_AWS_IP 'pm2 delete whatsapp-bulk-backend'
# Or
ssh ubuntu@YOUR_AWS_IP 'lsof -ti:4000 | xargs kill -9'
```

### PM2 not found on AWS
```bash
ssh ubuntu@YOUR_AWS_IP 'sudo npm install -g pm2'
```

---

## 📊 File Sizes

After building, you'll have:
- `server` - ~25-30MB (Linux binary)

This single file contains everything needed to run the backend!

---

## 🎉 That's It!

**Simple workflow:**
1. Make changes locally
2. Run `./build-for-linux.sh`
3. Upload `server` to AWS
4. Restart with PM2

**Or even simpler:**
```bash
./quick-deploy.sh YOUR_AWS_IP
```

No need to build on the server. No pressure on AWS resources. Just build locally and run! 🚀
