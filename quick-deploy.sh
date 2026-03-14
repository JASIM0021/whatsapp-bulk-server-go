#!/bin/bash
# Quick deploy script - builds on Mac, uploads to AWS, and starts with PM2

set -e

echo "⚡ Quick Deploy to AWS"
echo "====================="
echo ""

# Check for AWS IP
if [ -z "$1" ]; then
    echo "Usage: ./quick-deploy.sh YOUR_AWS_IP"
    echo ""
    echo "Example: ./quick-deploy.sh 54.123.45.67"
    exit 1
fi

AWS_IP=$1
AWS_USER=${2:-ubuntu}  # Default to ubuntu user

echo "🎯 Target: $AWS_USER@$AWS_IP"
echo ""

# Step 1: Build
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Step 1: Building Linux binary..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

./build-for-linux.sh

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Step 2: Creating deployment package..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Create deployment package
mkdir -p deploy-temp
cp server deploy-temp/
cp .env.production deploy-temp/.env
cp ecosystem.config.js deploy-temp/

# Update ecosystem.config.js with correct path
sed -i.bak "s|cwd: '.*'|cwd: '/home/$AWS_USER/whatsapp-backend'|g" deploy-temp/ecosystem.config.js
rm deploy-temp/ecosystem.config.js.bak 2>/dev/null || true

echo "✅ Package ready"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Step 3: Uploading to AWS..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Create directory on server
ssh $AWS_USER@$AWS_IP "mkdir -p /home/$AWS_USER/whatsapp-backend"

# Upload files
scp deploy-temp/server $AWS_USER@$AWS_IP:/home/$AWS_USER/whatsapp-backend/
scp deploy-temp/.env $AWS_USER@$AWS_IP:/home/$AWS_USER/whatsapp-backend/
scp deploy-temp/ecosystem.config.js $AWS_USER@$AWS_IP:/home/$AWS_USER/whatsapp-backend/

echo "✅ Files uploaded"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Step 4: Setting up on server..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Setup and start on server
ssh $AWS_USER@$AWS_IP << 'ENDSSH'
cd /home/ubuntu/whatsapp-backend

# Make binary executable
chmod +x server

# Create data directories
sudo mkdir -p /var/lib/whatsapp/uploads
sudo chown -R ubuntu:ubuntu /var/lib/whatsapp

echo ""
echo "✅ Setup complete"
echo ""
echo "Do you want to start the service with PM2? (y/n)"
read -p "> " start_service

if [ "$start_service" = "y" ] || [ "$start_service" = "Y" ]; then
    # Check if PM2 is installed
    if ! command -v pm2 &> /dev/null; then
        echo "Installing PM2..."
        sudo npm install -g pm2
    fi

    # Stop existing instance if any
    pm2 delete whatsapp-bulk-backend 2>/dev/null || true

    # Start with PM2
    pm2 start ecosystem.config.js --env production
    pm2 save

    echo ""
    echo "✅ Service started!"
    echo ""
    pm2 status
    echo ""
    pm2 logs whatsapp-bulk-backend --lines 20
fi
ENDSSH

# Cleanup
rm -rf deploy-temp

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 Deployment Complete!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Your backend is running at: http://$AWS_IP:4000"
echo ""
echo "Check status: ssh $AWS_USER@$AWS_IP 'pm2 status'"
echo "View logs:    ssh $AWS_USER@$AWS_IP 'pm2 logs whatsapp-bulk-backend'"
echo "Restart:      ssh $AWS_USER@$AWS_IP 'pm2 restart whatsapp-bulk-backend'"
echo ""
