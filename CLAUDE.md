# InnoDB Redo Log Analysis Tool

## 開発ルール

### Commit Policy
- **毎回修正後に必ずcommit**: 各機能修正・改善の完了時に即座にcommitする
- **Atomic commits**: 1つの論理的な変更につき1つのcommit
- **Descriptive messages**: 変更内容を明確に記述した日本語または英語のコミットメッセージ
- **Claude Code attribution**: 各commitに以下のフッターを含める：
  ```
  🤖 Generated with [Claude Code](https://claude.ai/code)
  
  Co-Authored-By: Claude <noreply@anthropic.com>
  ```

### UI開発指針
- **tview library使用**: TUIインターフェースにはtviewライブラリを活用
- **ペイン分割UI**: 左ペイン（一覧）+ 右ペイン（詳細）の構成
- **キーボードナビゲーション**: 矢印キー、Tab、Enterでの操作性重視

### データ表示原則
- **Complete field display**: 各レコードの全フィールドを表示
- **Readable format**: バイナリデータからのASCII文字列抽出
- **MySQL format compliance**: MySQL 8.0 mlog_id_t仕様に準拠

## プロジェクト構成

```
innodb-redolog-tool/
├── cmd/redolog-tool/        # CLI application entry point
├── internal/
│   ├── reader/             # Redo log format readers
│   ├── types/              # Data structures and types
│   ├── analyzer/           # Analysis logic (future)
│   └── parser/             # Parsing utilities (future)
├── test/fixtures/          # Test data
└── docs/                   # Documentation
```

## 実装済み機能

- ✅ MySQL 8.0 redo log binary format parsing
- ✅ Block-based reading (512-byte blocks with headers)
- ✅ Checksum calculation and verification
- ✅ ASCII string extraction from binary data
- ✅ Complete record field display
- ✅ Automatic format detection (MySQL vs test format)

## Next: TUI Interface

- 🔄 tviewベースの2ペイン構成
- 📋 左ペイン: レコード番号 + Type一覧
- 📄 右ペイン: 選択レコードの詳細表示
- ⌨️ キーボードナビゲーション対応