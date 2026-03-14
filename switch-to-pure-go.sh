#!/bin/bash
# Switch from CGO SQLite to Pure Go SQLite
# This allows cross-compilation from Mac to Linux without Docker!

set -e

echo "🔧 Switching to Pure Go SQLite"
echo "=============================="
echo ""
echo "This will:"
echo "  ✅ Remove mattn/go-sqlite3 (requires CGO)"
echo "  ✅ Install modernc.org/sqlite (pure Go)"
echo "  ✅ Enable cross-compilation from Mac"
echo ""
read -p "Continue? (y/n) " -n 1 -r
echo ""

if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Cancelled"
    exit 1
fi

echo "📦 Updating go.mod..."

# Remove old driver
go mod edit -droprequire github.com/mattn/go-sqlite3

# Add pure Go driver
go get modernc.org/sqlite

# Update dependencies
go mod tidy

echo ""
echo "✅ Dependencies updated!"
echo ""
echo "📝 Note: whatsmeow will automatically use the new driver"
echo ""
echo "🔨 Testing build..."

# Test build for Mac
CGO_ENABLED=0 go build -o server-test cmd/server/main.go

if [ $? -eq 0 ]; then
    rm server-test
    echo "✅ Build test successful!"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🎉 Ready to cross-compile!"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "Build for Linux (on Mac):"
    echo "  GOOS=linux GOARCH=amd64 go build -o server cmd/server/main.go"
    echo ""
    echo "Or use:"
    echo "  ./build-for-linux-simple.sh"
    echo ""
else
    echo "❌ Build failed!"
    exit 1
fi
