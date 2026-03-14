#!/bin/bash
# Build script to run directly on Linux server
# Upload all source files to server and run this script there

set -e

echo "🔨 Building WhatsApp Bulk Sender Backend on Linux Server"
echo "=========================================================="
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed!"
    echo ""
    echo "Install Go with:"
    echo "  wget https://go.dev/dl/go1.21.6.linux-amd64.tar.gz"
    echo "  sudo tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz"
    echo "  echo 'export PATH=\$PATH:/usr/local/go/bin' >> ~/.bashrc"
    echo "  source ~/.bashrc"
    exit 1
fi

# Check for build tools
if ! command -v gcc &> /dev/null; then
    echo "❌ GCC is not installed!"
    echo ""
    echo "Install build tools with:"
    echo "  sudo apt update"
    echo "  sudo apt install -y gcc build-essential"
    exit 1
fi

echo "✅ Go version: $(go version)"
echo "✅ GCC version: $(gcc --version | head -1)"
echo ""

# Build
echo "🔨 Building optimized production binary..."
CGO_ENABLED=1 go build -ldflags="-s -w" -o server cmd/server/main.go

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Build successful!"
    echo ""
    echo "Binary: ./server"
    echo "Size: $(du -h server | cut -f1)"
    echo ""
    chmod +x server

    # Test run
    echo "🧪 Testing binary..."
    ./server --version 2>/dev/null || echo "Binary is ready (no --version flag)"

    echo ""
    echo "✅ Ready to deploy!"
    echo ""
    echo "Next steps:"
    echo "1. Update .env file with your settings"
    echo "2. Choose deployment method:"
    echo "   - PM2: pm2 start ecosystem.config.js --env production"
    echo "   - Systemd: sudo cp whatsapp-backend.service /etc/systemd/system/ && sudo systemctl start whatsapp-backend"
else
    echo ""
    echo "❌ Build failed!"
    exit 1
fi
