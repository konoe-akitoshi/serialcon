#!/bin/bash

# SerialConアップデートスクリプト

echo "SerialConのアップデートを開始します..."

# カレントディレクトリを保存
CURRENT_DIR=$(pwd)

# スクリプトのディレクトリに移動
cd "$(dirname "$0")"

# リポジトリを最新の状態に更新
echo "リポジトリを更新中..."
git pull

# 変更があったかチェック
if [ $? -ne 0 ]; then
    echo "リポジトリの更新に失敗しました。"
    cd "$CURRENT_DIR"
    exit 1
fi

# 再ビルド
echo "バイナリを再ビルド中..."
./build.sh

# 再インストール
echo "システムに再インストール中..."
sudo ./install.sh

if [ $? -eq 0 ]; then
    echo "アップデート完了！"
    echo "最新バージョンのSerialConが利用可能になりました。"
else
    echo "インストール中にエラーが発生しました。"
fi

# 元のディレクトリに戻る
cd "$CURRENT_DIR"
