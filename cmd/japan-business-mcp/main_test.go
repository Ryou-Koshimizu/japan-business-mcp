package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/server"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/testcases"
)

// buildBinary は実際に配る形の実行ファイルを作る。
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "japan-business-mcp")
	if os.PathSeparator == '\\' {
		bin += ".exe"
	}
	cmd := exec.CommandContext(context.Background(), "go", "build", "-ldflags", "-X main.version=v0.0.0-test", "-o", bin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	return bin
}

// exercise は、つないだセッションで全道具・資料・依頼文を呼び、ゴールデンと突き合わせる。
func exercise(t *testing.T, cs *mcp.ClientSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	init := cs.InitializeResult()
	if init.ServerInfo.Name != server.Name || init.ServerInfo.Version != "v0.0.0-test" {
		t.Fatalf("serverInfo: %+v", init.ServerInfo)
	}
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != len(server.ToolNames) {
		t.Fatalf("道具の数 %d", len(tools.Tools))
	}
	for _, c := range testcases.Cases {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: c.Tool, Arguments: c.Args})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if res.IsError {
			t.Fatalf("%s: %v", c.Name, res.Content[0].(*mcp.TextContent).Text)
		}
		want, err := os.ReadFile(testcases.Path(c.Name))
		if err != nil {
			t.Fatal(err)
		}
		if got := testcases.Canonical(t, res.StructuredContent); !bytes.Equal(got, want) {
			t.Fatalf("%s: ゴールデンと違う\n%s", c.Name, got)
		}
	}
	// 誤った入力はエラーとして返り、セッションは続く
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "business_day_check", Arguments: map[string]any{"date": "2026-02-30"}})
	if err != nil || !res.IsError {
		t.Fatalf("誤った日付がエラーにならない: %v", err)
	}
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "jbiz://holidays/2027"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "invoice_review", Arguments: map[string]string{"invoice_text": "x"}}); err != nil {
		t.Fatal(err)
	}
}

func TestBinaryStdio(t *testing.T) {
	bin := buildBinary(t)
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-stdio", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.CommandContext(ctx, bin)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	exercise(t, cs)
}

// startHTTP は実行ファイルを HTTP で起動し、待ち受けの URL を返す。
func startHTTP(t *testing.T, bin string, env ...string) string {
	t.Helper()
	addr := os.Getenv("JBIZ_TEST_HTTP_ADDR") // 決まったポートで試すとき（例 127.0.0.1:8105）
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	cmd := exec.CommandContext(context.Background(), bin, "-http", addr)
	cmd.Env = append(os.Environ(), env...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	})
	sc := bufio.NewScanner(stderr)
	urlc := make(chan string, 1)
	go func() {
		for sc.Scan() {
			if u, ok := strings.CutPrefix(sc.Text(), "listening on "); ok {
				urlc <- u
			}
		}
	}()
	select {
	case u := <-urlc:
		return u
	case <-time.After(20 * time.Second):
		t.Fatal("HTTP の待ち受けが始まらない")
	}
	return ""
}

func TestBinaryHTTP(t *testing.T) {
	bin := buildBinary(t)
	url := startHTTP(t, bin)
	hreq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, strings.TrimSuffix(url, "/mcp")+"/healthz", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(b), "ok v0.0.0-test") {
		t.Fatalf("healthz: %d %s", resp.StatusCode, b)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "e2e-http", Version: "0"}, nil).
		Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: url}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	exercise(t, cs)
}

type authRT struct{ token string }

func (a authRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestBinaryHTTPToken(t *testing.T) {
	bin := buildBinary(t)
	url := startHTTP(t, bin, "JBIZ_HTTP_TOKEN=s3cret")
	// 鍵なしは 401
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("鍵なしが通った: %d", resp.StatusCode)
	}
	// 鍵ありは通る
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "e2e-token", Version: "0"}, nil).Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: authRT{"s3cret"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if _, err := cs.ListTools(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunFlags(t *testing.T) {
	var out, errb bytes.Buffer
	if err := run([]string{"-version"}, strings.NewReader(""), &out, &errb); err != nil || strings.TrimSpace(out.String()) != version {
		t.Fatalf("%v %q", err, out.String())
	}
	if err := run([]string{"-h"}, strings.NewReader(""), &out, &errb); err != nil || !strings.Contains(errb.String(), "使い方") {
		t.Fatalf("%v %q", err, errb.String())
	}
	if err := run([]string{"-nope"}, strings.NewReader(""), &out, &errb); err == nil {
		t.Fatal("知らない旗はエラー")
	}
	if err := run([]string{"-calendar", "/nonexistent.json"}, strings.NewReader(""), &out, &errb); err == nil {
		t.Fatal("無いカレンダーはエラー")
	}
}

// 標準入出力の生の JSON-RPC（改行区切り）で、初期化から道具の呼び出しまで通す。
func TestRunStdioRawJSON(t *testing.T) {
	pr, pw := io.Pipe()
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() { done <- run(nil, pr, out, io.Discard) }()
	lines := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"raw","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"wareki_convert","arguments":{"input":"R8.10.20"}}}`,
	}
	for _, l := range lines {
		fmt.Fprintln(pw, l)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), `"id":2`) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatalf("標準入力が閉じたら正常に終わるはず: %v", err)
	}
	var got struct {
		Result struct {
			StructuredContent struct {
				Results []struct{ Gregorian string } `json:"results"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.Contains(l, `"id":2`) {
			if err := json.Unmarshal([]byte(l), &got); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(got.Result.StructuredContent.Results) != 1 || got.Result.StructuredContent.Results[0].Gregorian != "2026年10月20日" {
		t.Fatalf("応答: %s", out.String())
	}
}

// syncBuf は書き込みと読み出しが別のゴルーチンから来てもよいバッファ。
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
