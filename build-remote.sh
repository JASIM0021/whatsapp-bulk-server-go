#!/bin/bash
# Build on AWS server remotely from Mac (no Docker needed!)
# This uploads source code and builds on the server, then downloads the binary

set -e

echo "🚀 Remote Build on AWS Server"
echo "=============================="
echo ""

# Check for AWS IP
if [ -z "$1" ]; then
    echo "Usage: ./build-remote.sh YOUR_AWS_IP [USER]"
    echo ""
    echo "Example:"
    echo "  ./build-remote.sh 54.123.45.67"
    echo "  ./build-remote.sh 54.123.45.67 ubuntu"
    exit 1
fi

AWS_IP=$1
AWS_USER=${2:-ubuntu}

echo "🎯 Target: $AWS_USER@$AWS_IP"
echo ""

# Create temporary build directory
BUILD_DIR="build-temp-$(date +%s)"
mkdir -p $BUILD_DIR

echo "📦 Preparing source code..."
# Copy only necessary files
cp -r cmd $BUILD_DIR/
cp -r internal $BUILD_DIR/
cp go.mod $BUILD_DIR/
cp go.sum $BUILD_DIR/
cp .env.production $BUILD_DIR/.env
cp ecosystem.config.js $BUILD_DIR/

# Create build script
cat > $BUILD_DIR/build.sh << 'EOF'
#!/bin/bash
set -e

echo "🔨 Building on server..."

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Installing Go..."
    wget -q https://go.dev/dl/go1.21.6.linux-amd64.tar.gz
    sudo tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz
    export PATH=$PATH:/usr/local/go/bin
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
fi

# Check for build tools
if ! command -v gcc &> /dev/null; then
    echo "Installing build tools..."
    sudo apt update -qq
    sudo apt install -y gcc build-essential
fi

# Build
echo "Building optimized binary..."
CGO_ENABLED=1 go build -ldflags="-s -w" -o server cmd/server/main.go

if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
    ls -lh server
else
    echo "❌ Build failed!"
    exit 1
fi
EOF

chmod +x $BUILD_DIR/build.sh

echo "📤 Uploading to AWS..."
# Create directory on server
ssh $AWS_USER@$AWS_IP "mkdir -p ~/whatsapp-build"

# Upload files
scp -r $BUILD_DIR/* $AWS_USER@$AWS_IP:~/whatsapp-build/

echo "🔨 Building on AWS server..."
echo ""
# Build on server
ssh $AWS_USER@$AWS_IP << 'ENDSSH'
cd ~/whatsapp-build
./build.sh
ENDSSH

echo ""
echo "📥 Downloading binary to Mac..."
# Download the built binary
scp $AWS_USER@$AWS_IP:~/whatsapp-build/server ./server

# Clean up
rm -rf $BUILD_DIR
ssh $AWS_USER@$AWS_IP "rm -rf ~/whatsapp-build"

if [ -f "server" ]; then
    echo ""
    echo "✅ Success! Binary downloaded: ./server"
    echo "📏 Size: $(du -h server | cut -f1)"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📤 Now upload and deploy:"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "scp server $AWS_USER@$AWS_IP:/home/$AWS_USER/whatsapp-backend/"
    echo "scp .env.production $AWS_USER@$AWS_IP:/home/$AWS_USER/whatsapp-backend/.env"
    echo "scp ecosystem.config.js $AWS_USER@$AWS_IP:/home/$AWS_USER/whatsapp-backend/"
    echo ""
    echo "ssh $AWS_USER@$AWS_IP"
    echo "cd /home/$AWS_USER/whatsapp-backend"
    echo "chmod +x server"
    echo "pm2 start ecosystem.config.js --env production"
    echo ""
else
    echo "❌ Build failed!"
    exit 1
fi
