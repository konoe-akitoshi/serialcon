#!/bin/bash

# インストール先ディレクトリ
INSTALL_DIR="/usr/local/bin"

# バイナリが存在するか確認
if [ ! -f "./serialcon" ]; then
    echo "エラー: serialconバイナリが見つかりません。"
    echo "先に ./build.sh を実行してバイナリをビルドしてください。"
    exit 1
fi

# インストール
echo "serialconをインストールしています..."
sudo cp ./serialcon $INSTALL_DIR/serialcon
sudo chmod +x $INSTALL_DIR/serialcon

# インストール確認
if [ $? -eq 0 ]; then
    echo "インストール成功！"
    echo "コマンド 'serialcon' を使用してツールを起動できます。"
else
    echo "インストール中にエラーが発生しました。"
    echo "手動でインストールするには:"
    echo "  sudo cp ./serialcon $INSTALL_DIR/serialcon"
    echo "  sudo chmod +x $INSTALL_DIR/serialcon"
fi
