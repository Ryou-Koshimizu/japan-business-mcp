// Command japan-business-mcp は、日本の業務の道具を MCP サーバーとして動かす。
//
//	japan-business-mcp                          # 標準入出力（Claude Desktop / Claude Code / Cursor から起動される形）
//	japan-business-mcp -http 127.0.0.1:8105     # Streamable HTTP（http://127.0.0.1:8105/mcp）
//	japan-business-mcp -calendar company.json   # 会社の休みを既定として読む
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/server"
)

// version はリリース時に -ldflags "-X main.version=v1.2.3" で入る。
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "japan-business-mcp:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("japan-business-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	httpAddr := fs.String("http", "", "Streamable HTTP で待ち受けるアドレス（例 127.0.0.1:8105）。省略時は標準入出力")
	calendar := fs.String("calendar", os.Getenv("JBIZ_CALENDAR"), "会社の休みを書いた JSON ファイル（環境変数 JBIZ_CALENDAR でも可）")
	showVersion := fs.Bool("version", false, "版を表示して終わる")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "使い方: japan-business-mcp [-http ADDR] [-calendar FILE]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Fprintln(stdout, version)
		return nil
	}
	cfg := server.Config{Version: version}
	if *calendar != "" {
		c, err := server.LoadCalendarFile(*calendar)
		if err != nil {
			return err
		}
		cfg.DefaultCalendar = c
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *httpAddr == "" {
		t := &mcp.IOTransport{Reader: io.NopCloser(stdin), Writer: nopWriteCloser{stdout}}
		return server.New(cfg).Run(ctx, t)
	}
	return serveHTTP(ctx, *httpAddr, cfg, stderr)
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func serveHTTP(ctx context.Context, addr string, cfg server.Config, stderr io.Writer) error {
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv := server.New(cfg)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		SessionTimeout: 30 * time.Minute,
		Logger:         logger,
	})
	h := http.NewCrossOriginProtection().Handler(mcpHandler)
	if tok := os.Getenv("JBIZ_HTTP_TOKEN"); tok != "" {
		h = bearer(tok, h)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", h)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok", version)
	})

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	hs := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintf(stderr, "listening on http://%s/mcp\n", ln.Addr())
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	select {
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return hs.Shutdown(sctx)
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// bearer は Authorization: Bearer <token> を確かめる（外部に公開するときのため）。
func bearer(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(strings.TrimSpace(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="japan-business-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
