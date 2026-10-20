# syukujitsu.csv の出どころ

- 内閣府「国民の祝日について」で公開されている CSV
  https://www8.cao.go.jp/chosei/shukujitsu/syukujitsu.csv
- 取得日: 2026-10-20（`scripts/fetch-cao-holidays.sh` で取得。手で書き写していない）
- 文字コード: Shift_JIS（公開されたままのバイト列を保存）
- SHA-256: cec37a743c96995cdb9cb52b685c9003634682a9b0e1a640a6b9b96881fe964a
- 収録範囲: 1955年〜2027年（1,067日）
- 利用条件: 内閣府ウェブサイトの利用規約（政府標準利用規約・CC BY 4.0 互換）に従い、出典を記載して利用

試験 `TestMatchesCabinetOfficeCSV` が、規則から計算した祝日・休日とこの CSV を1日ずつ突き合わせる。
