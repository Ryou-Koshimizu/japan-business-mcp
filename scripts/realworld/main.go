// Command realworld は「実機試験」：配る形の実行ファイルを、本物の MCP クライアント（公式 Go SDK）から
// 標準入出力と Streamable HTTP の両方で叩き、公的機関が公開している実データと突き合わせる。
//
//	go run ./scripts/realworld -bin bin/japan-business-mcp
//
// 使うデータ（実行のたびに公開元から取得し、-cache に置く）：
//   - 内閣府「国民の祝日について」の CSV（1955〜2027年）
//   - 国税庁 法人番号公表サイトの全件データ（鳥取県）
//   - 国税庁 適格請求書発行事業者公表サイトの全件データ（個人の分割5・人格のない社団等）
//
// 誤検出（正しい番号を誤りとする）・見逃し（1桁違いを正しいとする）・誤答・エラーを数え、
// 1件でもあれば終了コード 1 で終わる。
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

type check struct {
	Name     string   `json:"name"`
	Cases    int      `json:"cases"`
	Failures int      `json:"failures"`
	Examples []string `json:"failure_examples,omitempty"`
}

type transportReport struct {
	Transport string   `json:"transport"`
	Checks    []*check `json:"checks"`
	Calls     int      `json:"tool_calls"`
	Seconds   float64  `json:"seconds"`
}

type report struct {
	When       string             `json:"when"`
	Binary     string             `json:"binary"`
	Version    string             `json:"server_version"`
	Data       map[string]string  `json:"data_sha256"`
	Sources    map[string]string  `json:"data_sources"`
	Transports []*transportReport `json:"transports"`
	Failures   int                `json:"total_failures"`
	Totals     map[string]int     `json:"totals"`
}

func (c *check) fail(format string, a ...any) {
	c.Failures++
	if len(c.Examples) < 10 {
		c.Examples = append(c.Examples, fmt.Sprintf(format, a...))
	}
}

func main() {
	bin := flag.String("bin", "bin/japan-business-mcp", "試す実行ファイル")
	cache := flag.String("cache", ".cache/realworld", "取得したデータの置き場")
	out := flag.String("out", "docs/realworld/latest.json", "結果の JSON")
	stats := flag.String("stats", "web/stats.js", "遊び場に出す数字（空なら書かない）")
	port := flag.String("port", "8105", "HTTP で待ち受けるポート（127.0.0.1）")
	flag.Parse()
	if err := run(*bin, *cache, *out, *stats, *port); err != nil {
		fmt.Fprintln(os.Stderr, "realworld:", err)
		os.Exit(1)
	}
}

func run(bin, cache, out, statsPath, port string) error {
	ctx := context.Background()
	if err := os.MkdirAll(cache, 0o750); err != nil {
		return err
	}
	data, err := fetchAll(ctx, cache)
	if err != nil {
		return err
	}
	rep := &report{When: time.Now().Format(time.RFC3339), Binary: bin, Data: map[string]string{}, Sources: map[string]string{}, Totals: map[string]int{}}
	for k, v := range data.files {
		rep.Data[k] = v.sha
		rep.Sources[k] = v.url
	}

	// 標準入出力
	{
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "realworld-stdio", Version: "1"}, nil).
			Connect(ctx, &mcp.CommandTransport{Command: exec.CommandContext(ctx, bin)}, nil)
		if err != nil {
			return fmt.Errorf("stdio で接続できない: %w", err)
		}
		rep.Version = cs.InitializeResult().ServerInfo.Version
		tr := exercise(ctx, cs, "stdio", data)
		_ = cs.Close()
		rep.Transports = append(rep.Transports, tr)
	}
	// Streamable HTTP
	{
		cmd := exec.CommandContext(ctx, bin, "-http", "127.0.0.1:"+port)
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			return err
		}
		sc := bufio.NewScanner(stderr)
		endpoint := ""
		for sc.Scan() {
			if u, ok := strings.CutPrefix(sc.Text(), "listening on "); ok {
				endpoint = u
				break
			}
		}
		go func() { _, _ = io.Copy(io.Discard, stderr) }()
		if endpoint == "" {
			_ = cmd.Process.Kill()
			return errors.New("HTTP の待ち受けが始まらない")
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "realworld-http", Version: "1"}, nil).
			Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
		if err != nil {
			_ = cmd.Process.Kill()
			return fmt.Errorf("HTTP で接続できない: %w", err)
		}
		tr := exercise(ctx, cs, "streamable-http", data)
		_ = cs.Close()
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
		rep.Transports = append(rep.Transports, tr)
	}

	for _, tr := range rep.Transports {
		for _, c := range tr.Checks {
			rep.Failures += c.Failures
			fmt.Printf("%-16s %-44s %8d 件  誤り %d\n", tr.Transport, c.Name, c.Cases, c.Failures)
			for _, e := range c.Examples {
				fmt.Println("    ", e)
			}
		}
		fmt.Printf("%-16s 呼び出し %d 回・%.1f 秒\n", tr.Transport, tr.Calls, tr.Seconds)
	}
	first := rep.Transports[0]
	for _, c := range first.Checks {
		rep.Totals[c.Name] = c.Cases
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.WriteFile(out, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if statsPath != "" && rep.Failures == 0 {
		if err := writeStats(statsPath, rep); err != nil {
			return err
		}
	}
	fmt.Printf("合計の誤り: %d\n", rep.Failures)
	if rep.Failures > 0 {
		return fmt.Errorf("誤りが %d 件あります", rep.Failures)
	}
	return nil
}

// ───── 道具を呼ぶ ─────

type caller struct {
	cs    *mcp.ClientSession
	calls int
}

func (c *caller) call(ctx context.Context, tool string, args any, out any) error {
	c.calls++
	res, err := c.cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return err
	}
	if res.IsError {
		msg := ""
		if len(res.Content) > 0 {
			if t, ok := res.Content[0].(*mcp.TextContent); ok {
				msg = t.Text
			}
		}
		return fmt.Errorf("isError: %s", msg)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func exercise(ctx context.Context, cs *mcp.ClientSession, name string, d *dataset) *transportReport {
	t0 := time.Now()
	c := &caller{cs: cs}
	tr := &transportReport{Transport: name}
	tr.Checks = append(tr.Checks,
		checkHolidays(ctx, c, d),
		checkBusinessDays(ctx, c, d),
		checkNumbers(ctx, c, d.validNumbers, "公開データの登録番号・法人番号（誤検出）"),
		checkMutants(ctx, c, d.validNumbers),
		checkAddresses(ctx, c, d.addresses),
		checkWareki(ctx, c),
	)
	tr.Calls = c.calls
	tr.Seconds = time.Since(t0).Seconds()
	return tr
}

// 内閣府 CSV の表記に合わせる（振替休日・国民の休日はどちらも「休日」など）。
func caoLabel(date, name, kind string) string {
	switch {
	case kind == "substitute" || kind == "citizens":
		return "休日"
	case kind == "special" && strings.HasPrefix(date, "2019-"):
		return "休日（祝日扱い）"
	case kind == "special" && strings.HasSuffix(name, "結婚の儀"):
		return "結婚の儀"
	case kind == "special" && strings.HasSuffix(name, "大喪の礼"):
		return "大喪の礼"
	case date == "2019-10-14":
		return "体育の日（スポーツの日）"
	}
	return name
}

func checkHolidays(ctx context.Context, c *caller, d *dataset) *check {
	ch := &check{Name: "祝日（内閣府 CSV と1日ずつ）"}
	years := map[int]bool{}
	for date := range d.cao {
		years[atoi(date[:4])] = true
	}
	got := map[string]string{}
	for y := range years {
		var out struct {
			Holidays []struct{ Date, Name, Kind string } `json:"holidays"`
		}
		if err := c.call(ctx, "holidays_list", map[string]any{"year": y}, &out); err != nil {
			ch.fail("%d: %v", y, err)
			continue
		}
		for _, h := range out.Holidays {
			got[h.Date] = caoLabel(h.Date, h.Name, h.Kind)
		}
	}
	for date, name := range d.cao {
		ch.Cases++
		if g, ok := got[date]; !ok {
			ch.fail("%s %s が無い", date, name)
		} else if g != name {
			ch.fail("%s: %s ≠ 内閣府 %s", date, g, name)
		}
	}
	for date, g := range got {
		if _, ok := d.cao[date]; !ok {
			ch.Cases++
			ch.fail("%s %s は内閣府 CSV に無い", date, g)
		}
	}
	return ch
}

func checkBusinessDays(ctx context.Context, c *caller, d *dataset) *check {
	ch := &check{Name: "営業日の判定（2025〜2027年の全日）"}
	for t := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC); t.Year() <= 2027; t = t.AddDate(0, 0, 1) {
		date := t.Format("2006-01-02")
		_, hol := d.cao[date]
		want := !hol && t.Weekday() != time.Saturday && t.Weekday() != time.Sunday
		var out struct {
			Business bool `json:"is_business_day"`
		}
		ch.Cases++
		if err := c.call(ctx, "business_day_check", map[string]any{"date": date}, &out); err != nil {
			ch.fail("%s: %v", date, err)
		} else if out.Business != want {
			ch.fail("%s: %v（正しくは %v）", date, out.Business, want)
		}
	}
	return ch
}

type numResult struct {
	Results []struct {
		Input       string `json:"input"`
		Valid       bool   `json:"valid"`
		FormatError string `json:"format_error"`
	} `json:"results"`
}

func checkNumbers(ctx context.Context, c *caller, nums []string, name string) *check {
	ch := &check{Name: name}
	for i := 0; i < len(nums); i += 1000 {
		batch := nums[i:min(i+1000, len(nums))]
		var out numResult
		if err := c.call(ctx, "registration_number_validate", map[string]any{"numbers": batch}, &out); err != nil {
			ch.Cases += len(batch)
			ch.fail("batch %d: %v", i, err)
			continue
		}
		for j, r := range out.Results {
			ch.Cases++
			if !r.Valid || r.FormatError != "" || r.Input != batch[j] {
				ch.fail("%s を誤りと判定（%s）", batch[j], r.FormatError)
			}
		}
	}
	return ch
}

// 実在の番号の1桁を変えたもの。0↔9 の入れ替え（検査用数字の式では区別できない組）は作らない。
func checkMutants(ctx context.Context, c *caller, nums []string) *check {
	ch := &check{Name: "1桁を変えた番号（見逃し）"}
	muts := make([]string, 0, len(nums))
	for i, n := range nums {
		prefix := ""
		digits := n
		if strings.HasPrefix(n, "T") {
			prefix, digits = "T", n[1:]
		}
		pos := i % 13
		old := digits[pos] - '0'
		k := byte(1 + i%8) // 1〜8 ずらす
		nd := (old + k) % 10
		if (old == 0 && nd == 9) || (old == 9 && nd == 0) {
			nd = (nd + 1) % 10
		}
		if nd == old {
			nd = (nd + 1) % 10
		}
		muts = append(muts, prefix+digits[:pos]+string('0'+nd)+digits[pos+1:])
	}
	for i := 0; i < len(muts); i += 1000 {
		batch := muts[i:min(i+1000, len(muts))]
		var out numResult
		if err := c.call(ctx, "registration_number_validate", map[string]any{"numbers": batch}, &out); err != nil {
			ch.Cases += len(batch)
			ch.fail("batch %d: %v", i, err)
			continue
		}
		for j, r := range out.Results {
			ch.Cases++
			if r.Valid {
				ch.fail("%s を正しいと判定（元 %s）", batch[j], nums[i+j])
			}
		}
	}
	return ch
}

var (
	reDigitsRun   = regexp.MustCompile(`[0-9]+`)
	reKanjiBefore = regexp.MustCompile(`[〇一二三四五六七八九十百千](丁目|番地|番|号)`)
	reLeftover    = regexp.MustCompile(`[0-9](丁目|番地)[0-9]`)
)

// 住所の正規化：2回かけても同じ、全角の英数字が残らない、数字の並び（区切り）が崩れない。
func checkAddresses(ctx context.Context, c *caller, addrs []string) *check {
	ch := &check{Name: "法人の所在地の正規化（鳥取県の全法人）"}
	for _, a := range addrs {
		ch.Cases++
		var r1, r2 struct {
			Output string `json:"output"`
		}
		if err := c.call(ctx, "text_normalize", map[string]any{"text": a, "mode": "address"}, &r1); err != nil {
			ch.fail("%s: %v", a, err)
			continue
		}
		if err := c.call(ctx, "text_normalize", map[string]any{"text": r1.Output, "mode": "address"}, &r2); err != nil {
			ch.fail("%s: %v", a, err)
			continue
		}
		switch {
		case r2.Output != r1.Output:
			ch.fail("2回目で変わる: %s → %s → %s", a, r1.Output, r2.Output)
		case strings.IndexFunc(r1.Output, func(r rune) bool { return r >= '０' && r <= '９' || r >= 'Ａ' && r <= 'ｚ' }) >= 0:
			ch.fail("全角が残る: %s → %s", a, r1.Output)
		case reLeftover.MatchString(r1.Output):
			ch.fail("丁目・番地が残る: %s → %s", a, r1.Output)
		case !reKanjiBefore.MatchString(a) && !equalRuns(reDigitsRun.FindAllString(norm.NFKC.String(a), -1), reDigitsRun.FindAllString(r1.Output, -1)):
			ch.fail("数字の並びが変わる: %s → %s", a, r1.Output)
		}
	}
	return ch
}

func equalRuns(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 和暦⇔西暦：1873〜2150年を7日おきに、西暦→和暦→西暦で元に戻る。
func checkWareki(ctx context.Context, c *caller) *check {
	ch := &check{Name: "和暦⇔西暦の往復（1873〜2150年・7日おき）"}
	for t := time.Date(1873, 1, 1, 0, 0, 0, 0, time.UTC); t.Year() <= 2150; t = t.AddDate(0, 0, 7) {
		ch.Cases++
		date := t.Format("2006-01-02")
		var a, b struct {
			Results []struct {
				Date   string `json:"date"`
				Wareki string `json:"wareki"`
			} `json:"results"`
		}
		if err := c.call(ctx, "wareki_convert", map[string]any{"input": date}, &a); err != nil || len(a.Results) != 1 {
			ch.fail("%s: %v", date, err)
			continue
		}
		if err := c.call(ctx, "wareki_convert", map[string]any{"input": a.Results[0].Wareki}, &b); err != nil || len(b.Results) != 1 || b.Results[0].Date != date {
			ch.fail("%s → %s → %v (%v)", date, a.Results[0].Wareki, b.Results, err)
		}
	}
	return ch
}

// ───── データの取得 ─────

type file struct {
	path, url, sha string
}

type dataset struct {
	files        map[string]file
	cao          map[string]string
	validNumbers []string
	addresses    []string
}

func httpClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 3 * time.Minute}
}

func get(ctx context.Context, hc *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

func save(cache, name, u string, b []byte) (file, error) {
	p := filepath.Join(cache, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return file{}, err
	}
	s := sha256.Sum256(b)
	return file{path: p, url: u, sha: hex.EncodeToString(s[:])}, nil
}

func fetchAll(ctx context.Context, cache string) (*dataset, error) {
	d := &dataset{files: map[string]file{}, cao: map[string]string{}}
	hc := httpClient()

	// 内閣府の祝日 CSV
	const caoURL = "https://www8.cao.go.jp/chosei/shukujitsu/syukujitsu.csv"
	b, err := get(ctx, hc, caoURL)
	if err != nil {
		return nil, err
	}
	if d.files["cao_syukujitsu.csv"], err = save(cache, "cao_syukujitsu.csv", caoURL, b); err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(transform.NewReader(bytes.NewReader(b), japanese.ShiftJIS.NewDecoder())).ReadAll()
	if err != nil {
		return nil, err
	}
	for _, r := range rows[1:] {
		t, err := time.Parse("2006/1/2", r[0])
		if err != nil {
			return nil, err
		}
		d.cao[t.Format("2006-01-02")] = r[1]
	}

	// 法人番号公表サイト：鳥取県の全件（CSV・Shift_JIS）
	const houjinPage = "https://www.houjin-bangou.nta.go.jp/download/zenken/index.html"
	page, err := get(ctx, hc, houjinPage)
	if err != nil {
		return nil, err
	}
	tok := regexp.MustCompile(`CNSFWTokenProcessor\.request\.token" value="([^"]+)"`).FindSubmatch(page)
	idx := bytes.Index(page, []byte("鳥取県"))
	if tok == nil || idx < 0 {
		return nil, errors.New("法人番号公表サイトの画面が想定と違う")
	}
	fileNo := regexp.MustCompile(`doDownload\((\d+)\)`).FindSubmatch(page[idx:])
	if fileNo == nil {
		return nil, errors.New("鳥取県のファイル番号が見つからない")
	}
	form := url.Values{"jp.go.nta.houjin_bangou.framework.web.common.CNSFWTokenProcessor.request.token": {string(tok[1])}, "event": {"download"}, "selDlFileNo": {string(fileNo[1])}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, houjinPage, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	zb, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if d.files["houjin_tottori.zip"], err = save(cache, "houjin_tottori.zip", houjinPage+"（鳥取県 CSV）", zb); err != nil {
		return nil, err
	}
	csvb, err := firstCSV(zb)
	if err != nil {
		return nil, fmt.Errorf("法人番号: %w", err)
	}
	hr, err := csv.NewReader(transform.NewReader(bytes.NewReader(csvb), japanese.ShiftJIS.NewDecoder())).ReadAll()
	if err != nil {
		return nil, err
	}
	for _, r := range hr {
		d.validNumbers = append(d.validNumbers, r[1])
		d.addresses = append(d.addresses, r[9]+r[10]+r[11])
	}

	// 適格請求書発行事業者公表サイト：人格のない社団等・個人（分割の最後）
	const invPage = "https://www.invoice-kohyo.nta.go.jp/download/zenken"
	ip, err := get(ctx, hc, invPage)
	if err != nil {
		return nil, err
	}
	dl := regexp.MustCompile(`doDownload\('(\d+)','(\d)','01'\)`).FindAllSubmatch(ip, -1)
	picks := map[string]string{}
	for _, m := range dl {
		switch string(m[2]) {
		case "3":
			picks["invoice_shadan.zip"] = string(m[1])
		case "1":
			picks["invoice_kojin.zip"] = string(m[1]) // 最後の分割（いちばん小さい）
		}
	}
	if len(picks) != 2 {
		return nil, errors.New("適格請求書発行事業者公表サイトの画面が想定と違う")
	}
	names := []string{"invoice_shadan.zip", "invoice_kojin.zip"}
	for _, name := range names {
		jk := "3"
		if name == "invoice_kojin.zip" {
			jk = "1"
		}
		u := fmt.Sprintf("%s/dlfile?dlFilKanriNo=%s&jinkakukbn=%s&type=01", invPage, picks[name], jk)
		zb, err := get(ctx, hc, u)
		if err != nil {
			return nil, err
		}
		if d.files[name], err = save(cache, name, u, zb); err != nil {
			return nil, err
		}
		cb, err := firstCSV(zb)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		re := regexp.MustCompile(`"(T\d{13})"`)
		for _, m := range re.FindAllSubmatch(cb, -1) {
			d.validNumbers = append(d.validNumbers, string(m[1]))
		}
	}
	sort.Strings(d.validNumbers)
	d.validNumbers = dedupe(d.validNumbers)
	fmt.Printf("データ: 祝日 %d日・番号 %d件・所在地 %d件\n", len(d.cao), len(d.validNumbers), len(d.addresses))
	return d, nil
}

func dedupe(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func firstCSV(zb []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zb), int64(len(zb)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".csv") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 500<<20))
		}
	}
	return nil, errors.New("zip に CSV が無い")
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			n = n*10 + int(r-'0')
		}
	}
	return n
}

// ───── 遊び場に出す数字 ─────

func writeStats(path string, rep *report) error {
	tot := rep.Totals
	type stat struct {
		Value int    `json:"value"`
		Unit  string `json:"unit"`
		Label string `json:"label"`
		Cond  string `json:"cond"`
	}
	day := rep.When[:10]
	list := []stat{
		{tot["祝日（内閣府 CSV と1日ずつ）"], "日", "内閣府の祝日データと全日一致", "1955〜2027年。実行ファイルを stdio と HTTP の両方から呼んで照合（" + day + "）"},
		{tot["公開データの登録番号・法人番号（誤検出）"], "件", "国税庁の公開データの番号で誤検出 0", "法人番号（鳥取県の全件）と適格請求書発行事業者の登録番号（個人・人格のない社団等）"},
		{tot["1桁を変えた番号（見逃し）"], "件", "1桁違いの番号をすべて検出", "実在の番号の1桁を変えたもの（0と9の入れ替えは式の性質で区別できないため除く）"},
		{tot["法人の所在地の正規化（鳥取県の全法人）"], "件", "実在の住所の正規化で崩れ 0", "2回かけても同じ・全角が残らない・数字の並びが変わらない"},
		{tot["営業日の判定（2025〜2027年の全日）"] + tot["和暦⇔西暦の往復（1873〜2150年・7日おき）"], "回", "営業日と和暦の判定で誤答 0", "2025〜2027年の全日の営業日判定と、1873〜2150年の和暦の往復"},
	}
	b, _ := json.MarshalIndent(list, "", "  ")
	js := "// scripts/realworld が実機試験の結果から書き出す（手で直さない）。\nwindow.JBIZ_STATS = " + string(b) + ";\n"
	return os.WriteFile(path, []byte(js), 0o600)
}
