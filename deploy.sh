#!/bin/bash

# WhatsApp Bulk Sender - Deployment Script
# This script helps deploy the Go backend to production

set -e

echo "🚀 WhatsApp Bulk Sender - Deployment Script"
echo "============================================"
echo ""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if running from backend-go directory
if [ ! -f "cmd/server/main.go" ]; then
    echo -e "${RED}❌ Error: Please run this script from the backend-go directory${NC}"
    exit 1
fi

# Detect OS
OS_TYPE=$(uname -s)

if [ "$OS_TYPE" = "Darwin" ]; then
    echo -e "${YELLOW}⚠️  Detected macOS${NC}"
    echo -e "${YELLOW}Cross-compiling with CGO from macOS to Linux requires Docker.${NC}"
    echo ""
fi

# Ask for deployment type
echo -e "${BLUE}Select deployment type:${NC}"
echo "1) Build using Docker (cross-compile from macOS)"
echo "2) Create source package (build on server)"
echo "3) Build for macOS (local development)"
read -p "Enter choice [1-3]: " choice

case $choice in
    1)
        echo -e "${YELLOW}Building for Linux using Docker...${NC}"

        # Check if Docker is running
        if ! docker info > /dev/null 2>&1; then
            echo -e "${RED}❌ Error: Docker is not running${NC}"
            echo "Please start Docker Desktop and try again"
            exit 1
        fi

        # Build using Docker
        echo -e "${BLUE}Using Go Docker container to build...${NC}"
        docker run --rm \
            -v "$PWD":/app \
            -w /app \
            golang:1.21-alpine \
            sh -c "apk add --no-cache gcc musl-dev sqlite-dev && CGO_ENABLED=1 go build -ldflags='-s -w' -o server cmd/server/main.go"

        if [ $? -eq 0 ]; then
            echo -e "${GREEN}✅ Built successfully using Docker: ./server${NC}"
        else
            echo -e "${RED}❌ Build failed${NC}"
            exit 1
        fi
        ;;
    2)
        echo -e "${YELLOW}Creating source package for server-side build...${NC}"

        # Create deployment directory
        mkdir -p deploy

        # Copy source files
        cp -r cmd deploy/
        cp -r internal deploy/
        cp go.mod deploy/
        cp go.sum deploy/
        cp .env.production deploy/.env
        cp ecosystem.config.js deploy/
        cp whatsapp-backend.service deploy/

        # Create build script for server
        cat > deploy/build.sh << 'EOF'
#!/bin/bash
# Build script for Linux server

echo "Building Go backend..."
CGO_ENABLED=1 go build -ldflags="-s -w" -o server cmd/server/main.go

if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
    echo "Binary created: ./server"
    chmod +x server
else
    echo "❌ Build failed"
    exit 1
fi
EOF
        chmod +x deploy/build.sh

        # Create deployment instructions
        cat > deploy/DEPLOY_INSTRUCTIONS.txt << 'EOF'
Deployment Instructions (Build on Server)
==========================================

1. Upload this folder to your server:
   scp -r deploy ubuntu@your-server-ip:/home/ubuntu/whatsapp-backend/

2. SSH into your server:
   ssh ubuntu@your-server-ip

3. Navigate to the directory:
   cd /home/ubuntu/whatsapp-backend

4. Install Go (if not installed):
   wget https://go.dev/dl/go1.21.6.linux-amd64.tar.gz
   sudo tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz
   echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
   source ~/.bashrc

5. Install build dependencies:
   sudo apt update
   sudo apt install -y gcc build-essential

6. Build the application:
   ./build.sh

7. Update environment variables:
   nano .env
   (Update FRONTEND_URL and other settings)

8. Create necessary directories:
   sudo mkdir -p /var/lib/whatsapp/uploads
   sudo chown -R $USER:$USER /var/lib/whatsapp

9. Choose deployment method:

   Option A - PM2:
   --------------
   sudo npm install -g pm2
   pm2 start ecosystem.config.js --env production
   pm2 save
   pm2 startup

   Option B - Systemd (Recommended):
   ----------------------------------
   sudo cp whatsapp-backend.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable whatsapp-backend
   sudo systemctl start whatsapp-backend
   sudo systemctl status whatsapp-backend

10. Verify it's running:
    curl http://localhost:4000/api/health

Done! Your backend is deployed.
EOF

        # Create tar.gz
        tar -czf whatsapp-backend-source.tar.gz deploy/
        rm -rf deploy/

        echo -e "${GREEN}✅ Source package created: whatsapp-backend-source.tar.gz${NC}"
        echo -e "${BLUE}📦 Contents:${NC}"
        echo "   - Source code (cmd/, internal/)"
        echo "   - Go modules (go.mod, go.sum)"
        echo "   - build.sh (build script for server)"
        echo "   - .env (production config)"
        echo "   - ecosystem.config.js (PM2 config)"
        echo "   - whatsapp-backend.service (systemd config)"
        echo "   - DEPLOY_INSTRUCTIONS.txt"
        echo ""
        echo -e "${YELLOW}📤 Upload to server:${NC}"
        echo "   scp whatsapp-backend-source.tar.gz ubuntu@your-server-ip:~/"
        echo ""
        echo -e "${BLUE}📝 On server, extract and run:${NC}"
        echo "   tar -xzf whatsapp-backend-source.tar.gz"
        echo "   cd deploy"
        echo "   ./build.sh"
        ;;
    3)
        echo -e "${YELLOW}Building for macOS (local development)...${NC}"
        CGO_ENABLED=1 go build -o server cmd/server/main.go
        echo -e "${GREEN}✅ Built successfully: ./server${NC}"
        echo -e "${BLUE}This binary is for macOS only. Use option 1 or 2 for Linux deployment.${NC}"
        ;;
    *)
        echo -e "${YELLOW}Building and creating deployment package...${NC}"

        # Build
        GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -ldflags="-s -w" -o server cmd/server/main.go

        # Create deployment directory
        mkdir -p deploy
        cp server deploy/
        cp .env.production deploy/.env
        cp ecosystem.config.js deploy/
        cp whatsapp-backend.service deploy/

        # Create deployment instructions
        cat > deploy/DEPLOY_INSTRUCTIONS.txt << 'EOF'
Deployment Instructions
=======================

1. Upload this folder to your server:
   scp -r deploy ubuntu@your-server-ip:/home/ubuntu/whatsapp-backend/

2. SSH into your server:
   ssh ubuntu@your-server-ip

3. Navigate to the directory:
   cd /home/ubuntu/whatsapp-backend

4. Update environment variables:
   nano .env
   (Update FRONTEND_URL and other settings)

5. Create necessary directories:
   sudo mkdir -p /var/lib/whatsapp/uploads
   sudo chown -R ubuntu:ubuntu /var/lib/whatsapp

6. Choose deployment method:

   Option A - PM2:
   --------------
   npm install -g pm2
   pm2 start ecosystem.config.js --env production
   pm2 save
   pm2 startup

   Option B - Systemd (Recommended):
   ----------------------------------
   sudo cp whatsapp-backend.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable whatsapp-backend
   sudo systemctl start whatsapp-backend
   sudo systemctl status whatsapp-backend

7. Check if it's running:
   curl http://localhost:4000/api/health

8. Setup Nginx reverse proxy (see DEPLOYMENT.md)

Done! Your backend is deployed.
EOF

        # Create tar.gz
        tar -czf whatsapp-backend-deploy.tar.gz deploy/

        echo -e "${GREEN}✅ Deployment package created: whatsapp-backend-deploy.tar.gz${NC}"
        echo -e "${BLUE}📦 Contents:${NC}"
        echo "   - server (Linux binary)"
        echo "   - .env (production config)"
        echo "   - ecosystem.config.js (PM2 config)"
        echo "   - whatsapp-backend.service (systemd config)"
        echo "   - DEPLOY_INSTRUCTIONS.txt"
        echo ""
        echo -e "${YELLOW}📤 Upload to server:${NC}"
        echo "   scp whatsapp-backend-deploy.tar.gz ubuntu@your-server-ip:~/"
        ;;
    *)
        echo -e "${RED}Invalid choice${NC}"
        exit 1
        ;;
esac

echo ""
echo -e "${GREEN}✅ Complete!${NC}"
echo ""
echo -e "${BLUE}📚 Next steps:${NC}"
if [ "$choice" = "1" ]; then
    echo "1. Upload the binary to your server:"
    echo "   scp server ubuntu@your-server-ip:/home/ubuntu/"
    echo ""
    echo "2. Follow DEPLOYMENT.md for complete setup"
elif [ "$choice" = "2" ]; then
    echo "1. Upload the source package to your server"
    echo "2. Extract and run ./build.sh on the server"
    echo "3. Follow DEPLOYMENT.md for complete setup"
else
    echo "1. For production deployment, run this script again and choose option 1 or 2"
    echo "2. Local binary is at ./server (macOS only)"
fi
