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

# Ask for deployment type
echo -e "${BLUE}Select deployment type:${NC}"
echo "1) Build for Linux (AWS/VPS)"
echo "2) Build for production (optimized)"
echo "3) Build and create deployment package"
read -p "Enter choice [1-3]: " choice

case $choice in
    1)
        echo -e "${YELLOW}Building for Linux...${NC}"
        GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -o server cmd/server/main.go
        echo -e "${GREEN}✅ Built successfully: ./server${NC}"
        ;;
    2)
        echo -e "${YELLOW}Building optimized production binary...${NC}"
        GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -ldflags="-s -w" -o server cmd/server/main.go
        echo -e "${GREEN}✅ Built successfully (optimized): ./server${NC}"
        ;;
    3)
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
echo -e "${GREEN}Deployment build complete!${NC}"
echo -e "${BLUE}Next steps:${NC}"
echo "1. Upload the binary to your server"
echo "2. Follow DEPLOYMENT.md for complete setup"
echo "3. Configure Nginx and SSL"
echo ""
echo -e "${YELLOW}Quick upload command:${NC}"
echo "scp server ubuntu@your-server-ip:/home/ubuntu/whatsapp-backend/"
