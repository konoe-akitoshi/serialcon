#!/bin/bash

# バージョン
VERSION="0.1.0"

# ビルドディレクトリ
BUILD_DIR="./build"
mkdir -p $BUILD_DIR

echo "SerialCon $VERSION のビルドを開始します..."

# Mac用ビルド
echo "Mac用バイナリをビルド中..."
GOOS=darwin GOARCH=amd64 go build -o $BUILD_DIR/serialcon-$VERSION-darwin-amd64
GOOS=darwin GOARCH=arm64 go build -o $BUILD_DIR/serialcon-$VERSION-darwin-arm64

# Linux用ビルド
echo "Linux用バイナリをビルド中..."
GOOS=linux GOARCH=amd64 go build -o $BUILD_DIR/serialcon-$VERSION-linux-amd64
GOOS=linux GOARCH=arm64 go build -o $BUILD_DIR/serialcon-$VERSION-linux-arm64

# Windows用ビルド
echo "Windows用バイナリをビルド中..."
GOOS=windows GOARCH=amd64 go build -o $BUILD_DIR/serialcon-$VERSION-windows-amd64.exe

echo "ビルド完了！バイナリは $BUILD_DIR ディレクトリに保存されました。"

# 現在の環境用のバイナリをコピー
if [[ "$OSTYPE" == "darwin"* ]]; then
    if [[ $(uname -m) == "arm64" ]]; then
        cp $BUILD_DIR/serialcon-$VERSION-darwin-arm64 ./serialcon
    else
        cp $BUILD_DIR/serialcon-$VERSION-darwin-amd64 ./serialcon
    fi
    echo "現在の環境用のバイナリを ./serialcon にコピーしました。"
    echo "実行するには: ./serialcon"
elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
    if [[ $(uname -m) == "aarch64" ]]; then
        cp $BUILD_DIR/serialcon-$VERSION-linux-arm64 ./serialcon
    else
        cp $BUILD_DIR/serialcon-$VERSION-linux-amd64 ./serialcon
    fi
    echo "現在の環境用のバイナリを ./serialcon にコピーしました。"
    echo "実行するには: ./serialcon"
fi

# 実行権限を付与
chmod +x ./serialcon
