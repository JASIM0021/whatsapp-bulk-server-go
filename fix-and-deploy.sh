#!/bin/bash
# Fix PM2 logs issue and redeploy

set -e

if [ -z "$1" ]; then
    echo "Usage: ./fix-and-deploy.sh YOUR_AWS_IP"
    echo ""
    echo "Example: ./fix-and-deploy.sh 54.123.45.67"
    exit 1
fi

AWS_IP=$1
AWS_USER=${2:-ubuntu}

echo "🔧 Fixing PM2 logs issue and redeploying..."
echo "==========================================="
echo ""
echo "Target: $AWS_USER@$AWS_IP"
echo ""

# Upload updated ecosystem.config.js
echo "📤 Uploading fixed PM2 config..."
scp ecosystem.config.js $AWS_USER@$AWS_IP:/home/ubuntu/whatsapp-bulk-server-go/

# Restart PM2 with new config
echo "🔄 Restarting PM2..."
ssh $AWS_USER@$AWS_IP << 'ENDSSH'
cd /home/ubuntu/whatsapp-bulk-server-go

# Delete old instance
pm2 delete whatsapp-bulk-backend 2>/dev/null || true

# Start with new config
pm2 start ecosystem.config.js --env production

# Save
pm2 save

echo ""
echo "✅ Deployed!"
echo ""
pm2 status
echo ""
echo "View logs with: pm2 logs whatsapp-bulk-backend"
ENDSSH

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 Fixed and deployed!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Check status: ssh $AWS_USER@$AWS_IP 'pm2 status'"
echo "View logs:    ssh $AWS_USER@$AWS_IP 'pm2 logs whatsapp-bulk-backend'"
echo ""
