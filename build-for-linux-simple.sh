#!/bin/bash
# Simple cross-compile from Mac to Linux - NO DOCKER NEEDED!
# (After switching to pure Go SQLite)

set -e

echo "🚀 Building for Linux on Mac (No Docker)"
echo "========================================="
echo ""

# Clean old build
rm -f server

echo "🔨 Building..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o server cmd/server/main.go

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Build successful!"
    echo ""
    echo "📦 Binary: ./server"
    echo "📏 Size: $(du -h server | cut -f1)"
    echo "🖥️  Platform: Linux (built on Mac)"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📤 Upload to AWS:"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "scp server ubuntu@YOUR_AWS_IP:/home/ubuntu/whatsapp-backend/"
    echo "scp .env.production ubuntu@YOUR_AWS_IP:/home/ubuntu/whatsapp-backend/.env"
    echo "scp ecosystem.config.js ubuntu@YOUR_AWS_IP:/home/ubuntu/whatsapp-backend/"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🚀 Run on AWS:"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "ssh ubuntu@YOUR_AWS_IP"
    echo "cd whatsapp-backend"
    echo "chmod +x server"
    echo ""
    echo "# Install PM2 (one time)"
    echo "sudo npm install -g pm2"
    echo ""
    echo "# Start"
    echo "pm2 start ecosystem.config.js --env production"
    echo "pm2 save"
    echo ""
    echo "✅ Done!"
else
    echo ""
    echo "❌ Build failed!"
    echo ""
    echo "Did you run ./switch-to-pure-go.sh first?"
    exit 1
fi
