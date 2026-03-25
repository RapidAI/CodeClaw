#!/bin/bash
# WebUI 构建脚本
# 用法: ./scripts/build-webui.sh

set -e

echo "🔨 Building WebUI..."

cd "$(dirname "$0")/../webui"

# 检查 node_modules
if [ ! -d "node_modules" ]; then
    echo "📦 Installing dependencies..."
    npm install
fi

# 构建
echo "🏗️  Running build..."
npm run build

if [ ! -f "../static/admin/index.html" ]; then
    echo "❌ WebUI build failed: static/admin/index.html not found"
    exit 1
fi

echo "✅ WebUI built successfully!"
echo "📁 Output: static/admin/"
