#!/bin/bash

# Deploy CORS Fix to Server
# This script updates the backend on your server

set -e

SERVER="13.60.14.202"
USER="ubuntu"  # Change if different
REMOTE_DIR="/home/ubuntu/bulk-whatsapp-backend"  # Change to your actual path

echo "🚀 Deploying CORS fix to server..."

# Copy updated files to server
echo "📦 Copying updated CORS middleware..."
scp backend-go/internal/middleware/cors.go ${USER}@${SERVER}:${REMOTE_DIR}/internal/middleware/cors.go

echo "📦 Copying production .env file..."
scp backend-go/.env.production ${USER}@${SERVER}:${REMOTE_DIR}/.env

# Rebuild and restart the service
echo "🔨 Rebuilding Go backend on server..."
ssh ${USER}@${SERVER} << 'ENDSSH'
cd /home/ubuntu/bulk-whatsapp-backend

# Build the Go application
go build -o bin/server ./cmd/server

# Restart the service (adjust based on how your server runs)
# Option 1: If using systemd
sudo systemctl restart whatsapp-backend

# Option 2: If using PM2
# pm2 restart whatsapp-backend

# Option 3: If using a custom script
# ./restart.sh

echo "✅ Backend restarted successfully"
ENDSSH

echo "✅ Deployment complete!"
echo ""
echo "🔍 Testing CORS..."
curl -H "Origin: https://whatsapp-bulk-ui.vercel.app" \
     -H "Access-Control-Request-Method: GET" \
     -H "Access-Control-Request-Headers: Content-Type" \
     -X OPTIONS \
     http://${SERVER}:4000/api/whatsapp/status \
     -v

echo ""
echo "✅ Done! Your frontend should now be able to access the backend."
