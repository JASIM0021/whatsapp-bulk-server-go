# 🚀 Deploy Without Docker

**Two simple ways to deploy without needing Docker on your Mac!**

---

## ⚡ Option 1: Fully Automated (Recommended)

**One command does everything:**

```bash
./deploy-no-docker.sh YOUR_AWS_IP
```

**What it does:**
1. ✅ Uploads source code to AWS
2. ✅ Installs Go on AWS (if needed)
3. ✅ Builds on AWS server
4. ✅ Installs PM2 (if needed)
5. ✅ Starts the service
6. ✅ Done! 🎉

**Example:**
```bash
./deploy-no-docker.sh 54.123.45.67
```

**Total time:** ~3-5 minutes (first time includes installations)

**Server pressure:** Only during build (~30 seconds), then normal operation

---

## 🔄 Option 2: Build Remote, Download Binary

**Build on AWS, download binary, then upload to final location:**

```bash
./build-remote.sh YOUR_AWS_IP
```

**What it does:**
1. ✅ Uploads source to AWS
2. ✅ Builds on AWS server
3. ✅ Downloads binary back to Mac
4. ✅ You can reuse this binary multiple times

**Example:**
```bash
./build-remote.sh 54.123.45.67

# Binary is now at ./server
# Upload wherever you want:
scp server ubuntu@54.123.45.67:/home/ubuntu/whatsapp-backend/
```

**Benefits:**
- Build once, deploy many times
- Test binary locally before deploying
- Keep a copy of the binary

---

## 📊 Comparison

| Method | Build Location | Setup Time | Reusable Binary | Automation |
|--------|---------------|------------|-----------------|------------|
| **deploy-no-docker.sh** | AWS | 3-5 min | No | Full ✅ |
| **build-remote.sh** | AWS | 2-3 min | Yes ✅ | Partial |

---

## 🎯 Recommended: deploy-no-docker.sh

**Why?**
- ✅ Fully automated
- ✅ Handles everything (Go, PM2, build, deploy)
- ✅ One command to deploy
- ✅ Easy to update later

**Usage:**
```bash
./deploy-no-docker.sh 54.123.45.67
```

**Update later:**
```bash
# Make your code changes
./deploy-no-docker.sh 54.123.45.67
# Done! Automatically rebuilds and restarts
```

---

## 📋 Prerequisites

### On Mac:
- ✅ SSH access to AWS
- ✅ This repository
- ✅ That's it! No Docker needed!

### On AWS:
- ✅ Ubuntu 20.04+ (or similar)
- ✅ SSH access
- ✅ Internet connection

**The script will automatically install:**
- Go 1.21+
- GCC and build tools
- Node.js
- PM2

---

## 🔧 What Happens During Build

### First Deployment:
```
📦 Uploading source code...        (10 seconds)
📥 Installing Go on AWS...         (30 seconds)
📥 Installing build tools...       (30 seconds)
🔨 Building application...         (20 seconds)
📥 Installing PM2...               (20 seconds)
🚀 Starting service...             (5 seconds)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total: ~2-3 minutes
```

### Subsequent Deployments:
```
📦 Uploading source code...        (10 seconds)
🔨 Building application...         (20 seconds)
🚀 Restarting service...           (5 seconds)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total: ~35 seconds
```

**Server CPU usage during build:** ~30 seconds max

---

## 🎮 Commands

### Deploy/Update:
```bash
./deploy-no-docker.sh YOUR_AWS_IP
```

### Check Status:
```bash
ssh ubuntu@YOUR_AWS_IP 'pm2 status'
```

### View Logs:
```bash
ssh ubuntu@YOUR_AWS_IP 'pm2 logs whatsapp-bulk-backend'
```

### Restart:
```bash
ssh ubuntu@YOUR_AWS_IP 'pm2 restart whatsapp-bulk-backend'
```

---

## 🔍 What Gets Installed on AWS

**First time only:**
- Go 1.21.6 (~150MB)
- GCC + build tools (~50MB)
- Node.js 18+ (~40MB)
- PM2 (global npm package)

**Total:** ~250MB (one-time)

**Your app:** ~30MB

---

## 💡 Tips

### Faster Subsequent Deployments
After first deployment, updates are super fast:
```bash
# Make changes
./deploy-no-docker.sh YOUR_AWS_IP

# ~35 seconds later: deployed! ✅
```

### Custom User
If not using 'ubuntu' user:
```bash
./deploy-no-docker.sh YOUR_AWS_IP ec2-user
```

### Edit Environment Before Deploy
```bash
# Edit .env.production locally first
nano backend-go/.env.production

# Then deploy
./deploy-no-docker.sh YOUR_AWS_IP
```

---

## 🆘 Troubleshooting

### "Permission denied (publickey)"
```bash
# Add your SSH key to AWS
ssh-copy-id ubuntu@YOUR_AWS_IP
```

### "Connection refused"
```bash
# Check AWS security group allows SSH (port 22)
# Add your IP to inbound rules
```

### Script hangs during installation
```bash
# SSH in and check:
ssh ubuntu@YOUR_AWS_IP
# Kill any hanging apt processes:
sudo killall -9 apt apt-get
sudo rm /var/lib/dpkg/lock*
sudo dpkg --configure -a
```

### Want to see what it's doing?
```bash
# The script shows all output
# Watch it work in real-time
./deploy-no-docker.sh YOUR_AWS_IP
```

---

## 🎉 Example Session

```bash
$ cd backend-go

$ ./deploy-no-docker.sh 54.123.45.67

⚡ Deploy to AWS (No Docker)
============================

🎯 Target: ubuntu@54.123.45.67

📦 Step 1: Preparing deployment package...
✅ Package prepared

📤 Step 2: Uploading to AWS...
✅ Files uploaded

🔨 Step 3: Building and deploying on AWS...

🔧 Setting up server...
✅ Go installed: go version go1.21.6 linux/amd64
✅ Build tools installed
✅ Node.js installed: v18.19.0
✅ PM2 installed
📁 Creating data directories...

🔨 Building application...
✅ Build successful!
📏 Binary size: 28M

🚀 Deploying with PM2...
✅ Deployment complete!

┌─────┬──────────────────────┬─────────┬─────────┬──────────┐
│ id  │ name                 │ status  │ cpu     │ memory   │
├─────┼──────────────────────┼─────────┼─────────┼──────────┤
│ 0   │ whatsapp-bulk-back…  │ online  │ 0%      │ 45.5mb   │
└─────┴──────────────────────┴─────────┴─────────┴──────────┘

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🎉 Deployment Complete!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Your backend is running at: http://54.123.45.67:4000

✅ Done!
```

---

## 🚀 Summary

**Without Docker, you have two great options:**

1. **Fully Automated:** `./deploy-no-docker.sh YOUR_AWS_IP`
   - Everything handled for you
   - One command to deploy
   - Perfect for regular deployments

2. **Build Remote:** `./build-remote.sh YOUR_AWS_IP`
   - Build on AWS, download binary
   - Keep binary for reuse
   - More control

**Both are fast, simple, and don't require Docker!**

Choose what works best for you. For most users: **deploy-no-docker.sh** is the way to go! ⚡
