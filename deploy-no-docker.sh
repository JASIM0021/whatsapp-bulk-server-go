#!/bin/bash
# Complete deployment without Docker
# Uploads source, builds on server, and starts with PM2 - all automated!

set -e

echo "⚡ Deploy to AWS (No Docker)"
echo "============================"
echo ""

# Check for AWS IP
if [ -z "$1" ]; then
    echo "Usage: ./deploy-no-docker.sh YOUR_AWS_IP [USER]"
    echo ""
    echo "Example:"
    echo "  ./deploy-no-docker.sh 54.123.45.67"
    echo "  ./deploy-no-docker.sh 54.123.45.67 ubuntu"
    echo ""
    echo "This will:"
    echo "  1. Upload source code to AWS"
    echo "  2. Build on AWS server"
    echo "  3. Deploy with PM2"
    echo "  4. Done!"
    exit 1
fi

AWS_IP=$1
AWS_USER=${2:-ubuntu}

echo "🎯 Target: $AWS_USER@$AWS_IP"
echo ""

# Create temporary build directory
BUILD_DIR="deploy-temp-$(date +%s)"
mkdir -p $BUILD_DIR

echo "📦 Step 1: Preparing deployment package..."
# Copy necessary files
cp -r cmd $BUILD_DIR/
cp -r internal $BUILD_DIR/
cp go.mod $BUILD_DIR/
cp go.sum $BUILD_DIR/
cp .env.production $BUILD_DIR/.env
cp ecosystem.config.js $BUILD_DIR/

# Update ecosystem.config.js with correct path
sed -i.bak "s|cwd: '.*'|cwd: '/home/$AWS_USER/whatsapp-backend'|g" $BUILD_DIR/ecosystem.config.js
rm -f $BUILD_DIR/ecosystem.config.js.bak

# Create setup and build script
cat > $BUILD_DIR/setup-and-build.sh << 'EOFSCRIPT'
#!/bin/bash
set -e

echo "🔧 Setting up server..."

# Install Go if not present
if ! command -v go &> /dev/null; then
    echo "📥 Installing Go..."
    wget -q https://go.dev/dl/go1.21.6.linux-amd64.tar.gz
    sudo tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz
    rm go1.21.6.linux-amd64.tar.gz
    export PATH=$PATH:/usr/local/go/bin
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
    echo "✅ Go installed: $(go version)"
fi

# Install build tools if not present
if ! command -v gcc &> /dev/null; then
    echo "📥 Installing build tools..."
    sudo apt update -qq
    sudo apt install -y gcc build-essential
    echo "✅ Build tools installed"
fi

# Install Node.js and PM2 if not present
if ! command -v node &> /dev/null; then
    echo "📥 Installing Node.js..."
    curl -fsSL https://deb.nodesource.com/setup_18.x | sudo -E bash -
    sudo apt-get install -y nodejs
    echo "✅ Node.js installed: $(node --version)"
fi

if ! command -v pm2 &> /dev/null; then
    echo "📥 Installing PM2..."
    sudo npm install -g pm2
    echo "✅ PM2 installed"
fi

# Create data directories
echo "📁 Creating data directories..."
sudo mkdir -p /var/lib/whatsapp/uploads
sudo chown -R $USER:$USER /var/lib/whatsapp

# Build the application
echo ""
echo "🔨 Building application..."
CGO_ENABLED=1 go build -ldflags="-s -w" -o server cmd/server/main.go

if [ $? -eq 0 ]; then
    chmod +x server
    echo "✅ Build successful!"
    echo "📏 Binary size: $(du -h server | cut -f1)"
else
    echo "❌ Build failed!"
    exit 1
fi

# Deploy with PM2
echo ""
echo "🚀 Deploying with PM2..."

# Stop existing instance if any
pm2 delete whatsapp-bulk-backend 2>/dev/null || true

# Start new instance
pm2 start ecosystem.config.js --env production

# Save PM2 config
pm2 save

# Setup auto-start (only first time)
if [ ! -f ~/.pm2-startup-configured ]; then
    pm2 startup | grep -o 'sudo .*' | bash || true
    touch ~/.pm2-startup-configured
fi

echo ""
echo "✅ Deployment complete!"
echo ""
pm2 status
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 Your backend is now running!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Check logs: pm2 logs whatsapp-bulk-backend"
echo "Restart:    pm2 restart whatsapp-bulk-backend"
echo "Stop:       pm2 stop whatsapp-bulk-backend"
echo ""
EOFSCRIPT

chmod +x $BUILD_DIR/setup-and-build.sh

echo "✅ Package prepared"
echo ""
echo "📤 Step 2: Uploading to AWS..."

# Create directory on server and upload
ssh $AWS_USER@$AWS_IP "mkdir -p /home/$AWS_USER/whatsapp-backend"
scp -r $BUILD_DIR/* $AWS_USER@$AWS_IP:/home/$AWS_USER/whatsapp-backend/

echo "✅ Files uploaded"
echo ""
echo "🔨 Step 3: Building and deploying on AWS..."
echo ""

# Run setup and build on server
ssh -t $AWS_USER@$AWS_IP << ENDSSH
cd /home/$AWS_USER/whatsapp-backend
./setup-and-build.sh
ENDSSH

# Clean up local temp directory
rm -rf $BUILD_DIR

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 Deployment Complete!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Your backend is running at: http://$AWS_IP:4000"
echo ""
echo "Useful commands:"
echo "  Status:  ssh $AWS_USER@$AWS_IP 'pm2 status'"
echo "  Logs:    ssh $AWS_USER@$AWS_IP 'pm2 logs whatsapp-bulk-backend'"
echo "  Restart: ssh $AWS_USER@$AWS_IP 'pm2 restart whatsapp-bulk-backend'"
echo ""
echo "Next steps:"
echo "  1. Update frontend .env with your backend URL"
echo "  2. Setup Nginx reverse proxy (optional)"
echo "  3. Setup SSL certificate (optional)"
echo ""
