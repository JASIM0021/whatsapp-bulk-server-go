#!/bin/bash
# Simple script to build Linux binary on Mac using Docker
# Just run this, then upload the 'server' file to AWS

set -e

echo "🚀 Building Linux Binary on Mac using Docker"
echo "============================================="
echo ""

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker is not running!"
    echo ""
    echo "Please start Docker Desktop and try again."
    echo ""
    echo "Don't have Docker?"
    echo "  Download: https://www.docker.com/products/docker-desktop"
    exit 1
fi

echo "✅ Docker is running"
echo ""

# Clean up old build
rm -f server

# Build using Docker
echo "🔨 Building Go binary for Linux..."
echo ""

docker run --rm \
    -v "$PWD":/app \
    -w /app \
    golang:1.21-alpine \
    sh -c "apk add --no-cache gcc musl-dev sqlite-dev && \
           CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
           go build -ldflags='-s -w -extldflags \"-static\"' \
           -tags sqlite_omit_load_extension \
           -o server cmd/server/main.go"

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Build successful!"
    echo ""
    echo "📦 Binary created: ./server"
    echo "📏 Size: $(du -h server | cut -f1)"
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
    echo "cd /home/ubuntu/whatsapp-backend"
    echo "chmod +x server"
    echo ""
    echo "# Option A - Direct run (test)"
    echo "./server"
    echo ""
    echo "# Option B - PM2 (recommended)"
    echo "npm install -g pm2"
    echo "pm2 start ecosystem.config.js --env production"
    echo "pm2 save"
    echo ""
    echo "✅ Done!"
else
    echo ""
    echo "❌ Build failed!"
    exit 1
fi
