package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// エンコーディング種類
var encodings = map[string]encoding.Encoding{
	"UTF-8":       encoding.Nop,
	"Shift-JIS":   japanese.ShiftJIS,
	"EUC-JP":      japanese.EUCJP,
	"ISO-2022-JP": japanese.ISO2022JP,
}

// 一般的なボーレート（優先順位順）
var commonBaudRates = []int{9600, 115200, 19200, 38400, 57600, 4800, 2400, 1200}

// シリアルポート設定
type SerialConfig struct {
	PortName           string
	BaudRate           int
	DataBits           int
	Parity             serial.Parity
	StopBits           serial.StopBits
	AutoNegotiate      bool
	AutoDetectEncoding bool
	Encoding           encoding.Encoding
	EncodingName       string
	LogFile            string // ログファイルのパス
}

// デフォルト設定
func DefaultConfig() SerialConfig {
	return SerialConfig{
		PortName:           "",
		BaudRate:           9600, // Ciscoデバイスのデフォルト
		DataBits:           8,
		Parity:             serial.NoParity,
		StopBits:           serial.OneStopBit,
		AutoNegotiate:      true,
		AutoDetectEncoding: true,
		Encoding:           encoding.Nop, // デフォルトはUTF-8
		EncodingName:       "UTF-8",
		LogFile:            "", // デフォルトはログなし
	}
}

// 利用可能なシリアルポートを取得
func getAvailablePorts() ([]string, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}

	var portNames []string
	for _, port := range ports {
		info := port.Name
		if port.IsUSB {
			info += fmt.Sprintf(" [USB: %s %s]", port.VID, port.PID)
		}
		if port.Product != "" {
			info += fmt.Sprintf(" - %s", port.Product)
		}
		portNames = append(portNames, info)
	}

	return portNames, nil
}

// ポート名のみを抽出（表示用の情報を除く）
func extractPortName(portInfo string) string {
	parts := strings.Split(portInfo, " ")
	return parts[0]
}

// エンコーディングを自動検出
func detectEncoding(data []byte) (encoding.Encoding, string) {
	// 各エンコーディングでデコードを試みて、最も「正しく」見えるものを選択
	// 簡易的な判定: デコード後のUTF-8文字列に不正なバイトシーケンスが少ないものを選択

	bestEncoding := encoding.Nop
	bestEncodingName := "UTF-8"
	lowestErrorCount := len(data) // 最大エラー数で初期化

	// 試すエンコーディング
	encodingsToTry := map[string]encoding.Encoding{
		"Shift-JIS":   japanese.ShiftJIS,
		"EUC-JP":      japanese.EUCJP,
		"ISO-2022-JP": japanese.ISO2022JP,
	}

	for name, enc := range encodingsToTry {
		decoder := enc.NewDecoder()
		decoded, _, err := transform.Bytes(decoder, data)
		if err != nil {
			continue
		}

		// UTF-8として不正なバイトシーケンスをカウント
		errorCount := 0
		for i := 0; i < len(decoded); i++ {
			if decoded[i] >= 0x80 {
				// マルチバイト文字の先頭バイト
				if i+1 >= len(decoded) || (decoded[i+1]&0xC0) != 0x80 {
					errorCount++
				}
			}
		}

		if errorCount < lowestErrorCount {
			lowestErrorCount = errorCount
			bestEncoding = enc
			bestEncodingName = name
		}
	}

	return bestEncoding, bestEncodingName
}

// 自動ネゴシエーション
func autoNegotiate(portName string, originalConfig SerialConfig) (SerialConfig, error) {
	config := originalConfig
	config.PortName = portName

	type baudRateScore struct {
		baudRate int
		score    int
		data     []byte
	}

	var bestBaudRate baudRateScore

	fmt.Println("ボーレート自動検出中...")

	// 各ボーレートで複数回試行
	for _, baudRate := range commonBaudRates {
		fmt.Printf("  %d bps テスト中...", baudRate)

		// このボーレートでのスコア
		score := 0
		var responseData []byte

		// 3回試行
		for attempt := 0; attempt < 3; attempt++ {
			// ポートを開く
			port, err := serial.Open(portName, &serial.Mode{
				BaudRate: baudRate,
				DataBits: config.DataBits,
				Parity:   config.Parity,
				StopBits: config.StopBits,
			})

			if err != nil {
				continue
			}

			// テストコマンド送信（複数のコマンドを試す）
			testCommands := [][]byte{
				{13},         // CR
				{13, 10},     // CR+LF
				{27, 91, 65}, // 上矢印キー
				{63, 13},     // ?+CR (ヘルプ)
			}

			for _, cmd := range testCommands {
				_, err = port.Write(cmd)
				if err != nil {
					continue
				}

				// 応答を待つ（タイムアウトを調整）
				buf := make([]byte, 1024)
				port.SetReadTimeout(time.Millisecond * 300)
				n, err := port.Read(buf)

				if err == nil && n > 0 {
					// 応答があればスコアを加算
					score++

					// 最初に応答があった場合はそのデータを保存
					if responseData == nil {
						responseData = make([]byte, n)
						copy(responseData, buf[:n])
					}

					// 応答データの品質を評価
					// プロンプト文字（>、#、$など）が含まれていればボーナススコア
					if bytes.ContainsAny(buf[:n], ">#$:") {
						score += 3
					}

					// 可読文字が多ければボーナススコア
					readableChars := 0
					for _, b := range buf[:n] {
						if (b >= 32 && b <= 126) || b == 13 || b == 10 || b == 9 {
							readableChars++
						}
					}
					if float64(readableChars)/float64(n) > 0.7 {
						score += 2
					}
				}
			}

			port.Close()
		}

		// このボーレートの結果を表示
		if score > 0 {
			fmt.Printf(" 応答あり (スコア: %d)\n", score)
		} else {
			fmt.Println(" 応答なし")
		}

		// 最高スコアを更新
		if score > bestBaudRate.score {
			bestBaudRate = baudRateScore{
				baudRate: baudRate,
				score:    score,
				data:     responseData,
			}
		}
	}

	if bestBaudRate.score == 0 {
		// 自動ネゴシエーション失敗時はデフォルト設定を返す
		fmt.Println("自動ネゴシエーション失敗: デフォルト設定を使用します")
		return config, nil
	}

	// 最適なボーレートを設定
	config.BaudRate = bestBaudRate.baudRate
	fmt.Printf("最適なボーレート: %d bps (スコア: %d)\n", bestBaudRate.baudRate, bestBaudRate.score)

	// エンコーディングの自動検出が有効な場合
	if config.AutoDetectEncoding && bestBaudRate.data != nil && len(bestBaudRate.data) > 0 {
		detectedEncoding, detectedEncodingName := detectEncoding(bestBaudRate.data)
		config.Encoding = detectedEncoding
		config.EncodingName = detectedEncodingName
		fmt.Printf("エンコーディングを自動検出: %s\n", detectedEncodingName)
	}

	return config, nil
}

// シリアルポートに接続してインタラクティブモードを開始
func connectToPort(config SerialConfig) error {
	var logFile *os.File
	var err error

	// ログファイルが指定されている場合は開く
	if config.LogFile != "" {
		// ディレクトリが存在しない場合は作成
		logDir := filepath.Dir(config.LogFile)
		if logDir != "." {
			if err := os.MkdirAll(logDir, 0755); err != nil {
				return fmt.Errorf("ログディレクトリの作成に失敗しました: %v", err)
			}
		}

		// ログファイルを開く（追記モード）
		logFile, err = os.OpenFile(config.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("ログファイルを開けませんでした: %v", err)
		}
		defer logFile.Close()

		// ログファイルにセッション開始情報を書き込む
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		logFile.WriteString(fmt.Sprintf("\n\n===== セッション開始: %s =====\n", timestamp))
		logFile.WriteString(fmt.Sprintf("ポート: %s, ボーレート: %d, エンコーディング: %s\n\n",
			config.PortName, config.BaudRate, config.EncodingName))
	}
	// ポートを開く
	port, err := serial.Open(config.PortName, &serial.Mode{
		BaudRate: config.BaudRate,
		DataBits: config.DataBits,
		Parity:   config.Parity,
		StopBits: config.StopBits,
	})

	if err != nil {
		return fmt.Errorf("ポートを開けませんでした: %v", err)
	}

	defer port.Close()

	// 接続情報を表示
	fmt.Printf("\n接続しました: %s (ボーレート: %d, データビット: %d)\n",
		config.PortName, config.BaudRate, config.DataBits)
	fmt.Println("終了するには Ctrl+C を押してください")

	// 非ブロッキングモードに設定
	port.SetReadTimeout(time.Millisecond * 10)

	// ユーザー入力を処理するゴルーチン
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				continue
			}

			_, err = port.Write(buf[:n])
			if err != nil {
				log.Printf("書き込みエラー: %v", err)
				return
			}
		}
	}()

	// シリアルポートからの出力を処理
	buf := make([]byte, 1024) // より大きなバッファを使用
	readBuf := bytes.NewBuffer(make([]byte, 0, 1024))

	// 接続情報にエンコーディングを追加表示
	fmt.Printf("エンコーディング: %s\n", config.EncodingName)

	// エンコーディング変換用のトランスフォーマー
	var transformer transform.Transformer
	if config.Encoding != encoding.Nop {
		transformer = config.Encoding.NewDecoder()
	}

	for {
		n, err := port.Read(buf)
		if err != nil && err != io.EOF && !strings.Contains(err.Error(), "timeout") {
			return fmt.Errorf("読み込みエラー: %v", err)
		}

		if n > 0 {
			// バッファに追加
			readBuf.Write(buf[:n])

			// エンコーディング変換
			var outputData []byte
			if transformer != nil {
				// 変換処理
				decoded, err := io.ReadAll(transform.NewReader(readBuf, transformer))
				if err == nil && len(decoded) > 0 {
					outputData = decoded
				} else {
					// 変換エラー時は元のバイト列を使用
					outputData = readBuf.Bytes()
				}
			} else {
				// UTF-8の場合は変換しない
				outputData = readBuf.Bytes()
			}

			// 標準出力に書き込み
			os.Stdout.Write(outputData)

			// ログファイルに書き込み
			if logFile != nil {
				logFile.Write(outputData)
			}

			// バッファをクリア
			readBuf.Reset()
		}

		time.Sleep(time.Millisecond * 10)
	}
}

// メイン関数
func main() {
	// コマンドラインオプションの解析
	var logFilePath string
	flag.StringVar(&logFilePath, "log", "", "ログファイルのパス（例: ./logs/session.log）")
	flag.Parse()

	// tviewアプリケーションの作成
	app := tview.NewApplication()

	// 設定
	config := DefaultConfig()
	config.LogFile = logFilePath

	// ログファイルが指定されている場合は表示
	if config.LogFile != "" {
		fmt.Printf("ログファイル: %s\n", config.LogFile)
	}

	// 利用可能なポートを取得
	ports, err := getAvailablePorts()
	if err != nil {
		log.Fatalf("ポートの列挙に失敗しました: %v", err)
	}

	if len(ports) == 0 {
		log.Fatalf("利用可能なシリアルポートが見つかりませんでした")
	}

	// メインフォーム
	form := tview.NewForm()

	// ポート選択リスト
	portList := tview.NewList().
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorBlue)

	for i, port := range ports {
		portList.AddItem(port, "", rune('a'+i), nil)
	}

	portList.SetSelectedFunc(func(index int, _ string, _ string, _ rune) {
		portInfo := ports[index]
		config.PortName = extractPortName(portInfo)

		// 自動ネゴシエーションが有効な場合
		if config.AutoNegotiate {
			app.Stop()
			fmt.Printf("ポート %s に接続中...\n", config.PortName)
			fmt.Println("自動ネゴシエーション中...")

			negotiatedConfig, err := autoNegotiate(config.PortName, config)
			if err != nil {
				log.Fatalf("自動ネゴシエーションに失敗しました: %v", err)
			}

			err = connectToPort(negotiatedConfig)
			if err != nil {
				log.Fatalf("接続エラー: %v", err)
			}
		} else {
			// 手動設定の場合
			app.Stop()
			err := connectToPort(config)
			if err != nil {
				log.Fatalf("接続エラー: %v", err)
			}
		}
	})

	// ボーレート選択
	baudRateDropDown := tview.NewDropDown().
		SetLabel("ボーレート: ").
		SetOptions([]string{
			"9600", "19200", "38400", "57600", "115200",
		}, func(text string, index int) {
			baudRate, _ := strconv.Atoi(text)
			config.BaudRate = baudRate
		})
	baudRateDropDown.SetCurrentOption(0) // デフォルトは9600

	// エンコーディング選択
	encodingNames := []string{"UTF-8", "Shift-JIS", "EUC-JP", "ISO-2022-JP"}
	encodingDropDown := tview.NewDropDown().
		SetLabel("エンコーディング: ").
		SetOptions(encodingNames, func(text string, index int) {
			config.EncodingName = text
			config.Encoding = encodings[text]
			fmt.Printf("エンコーディングを %s に設定しました\n", text)
		})
	encodingDropDown.SetCurrentOption(0) // デフォルトはUTF-8

	// 自動ネゴシエーションチェックボックス
	autoNegotiateCheckbox := tview.NewCheckbox().
		SetLabel("自動ネゴシエーション: ").
		SetChecked(config.AutoNegotiate).
		SetChangedFunc(func(checked bool) {
			config.AutoNegotiate = checked
			// 自動ネゴシエーションが有効な場合、ボーレート選択を無効化
			baudRateDropDown.SetDisabled(checked)
		})

	// 自動エンコーディング検出チェックボックス
	autoDetectEncodingCheckbox := tview.NewCheckbox().
		SetLabel("エンコーディング自動検出: ").
		SetChecked(config.AutoDetectEncoding).
		SetChangedFunc(func(checked bool) {
			config.AutoDetectEncoding = checked
			// 自動検出が有効な場合、エンコーディング選択を無効化
			encodingDropDown.SetDisabled(checked)
		})

	// フォームにコンポーネントを追加
	form.AddFormItem(autoNegotiateCheckbox)
	form.AddFormItem(autoDetectEncodingCheckbox)
	form.AddFormItem(baudRateDropDown)
	form.AddFormItem(encodingDropDown)

	// レイアウト
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(tview.NewTextView().
			SetText("利用可能なシリアルポート（選択してください）:").
			SetTextColor(tcell.ColorYellow), 1, 0, false).
		AddItem(portList, 0, 1, true).
		AddItem(form, 4, 0, false)

	// アプリケーションの実行
	if err := app.SetRoot(flex, true).EnableMouse(true).Run(); err != nil {
		log.Fatalf("アプリケーションエラー: %v", err)
	}
}
