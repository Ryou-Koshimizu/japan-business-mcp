//go:build js && wasm

// Command wasm は、MCP サーバー（stdio・HTTP 版と同じ Go のコード）をブラウザの中で動かす。
//
// ブラウザ側からは、JSON-RPC の文字列をそのまま送り、サーバーの返事を文字列で受け取る：
//
//	const res = await jbizMCP.send('{"jsonrpc":"2.0","id":1,"method":"tools/list"}')
//
// 道具の計算はすべてこの WebAssembly の中で行い、通信はしない。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"syscall/js"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/server"
)

var version = "dev"

type bridge struct {
	conn    mcp.Connection
	mu      sync.Mutex
	pending map[string]js.Value // id → resolve
}

func main() {
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.New(server.Config{Version: version}).Connect(ctx, st, nil); err != nil {
		panic(err)
	}
	conn, err := ct.Connect(ctx)
	if err != nil {
		panic(err)
	}
	b := &bridge{conn: conn, pending: map[string]js.Value{}}
	go b.readLoop(ctx)

	api := js.Global().Get("Object").New()
	api.Set("version", version)
	api.Set("send", js.FuncOf(b.send))
	tools := js.Global().Get("Array").New()
	for _, n := range server.ToolNames {
		tools.Call("push", n)
	}
	api.Set("tools", tools)
	js.Global().Set("jbizMCP", api)
	if cb := js.Global().Get("onJbizReady"); cb.Type() == js.TypeFunction {
		cb.Invoke()
	}
	select {}
}

func idKey(id jsonrpc.ID) string {
	b, _ := json.Marshal(id.Raw())
	return string(b)
}

// send は JSON-RPC の文字列を受け取り、返事の文字列で解決する Promise を返す（通知なら null）。
func (b *bridge) send(_ js.Value, args []js.Value) any {
	raw := ""
	if len(args) > 0 {
		raw = args[0].String()
	}
	handler := js.FuncOf(func(_ js.Value, pargs []js.Value) any {
		resolve, reject := pargs[0], pargs[1]
		go func() {
			msg, err := jsonrpc.DecodeMessage([]byte(raw))
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("JSON-RPC として読めません: %v", err)))
				return
			}
			req, isReq := msg.(*jsonrpc.Request)
			if isReq && req.ID.IsValid() {
				b.mu.Lock()
				b.pending[idKey(req.ID)] = resolve
				b.mu.Unlock()
			}
			if err := b.conn.Write(context.Background(), msg); err != nil {
				reject.Invoke(js.Global().Get("Error").New(err.Error()))
				return
			}
			if !isReq || !req.ID.IsValid() {
				resolve.Invoke(js.Null())
			}
		}()
		return nil
	})
	defer handler.Release()
	return js.Global().Get("Promise").New(handler)
}

func (b *bridge) readLoop(ctx context.Context) {
	for {
		msg, err := b.conn.Read(ctx)
		if err != nil {
			return
		}
		data, err := jsonrpc.EncodeMessage(msg)
		if err != nil {
			continue
		}
		resp, ok := msg.(*jsonrpc.Response)
		if !ok {
			if cb := js.Global().Get("onJbizNotification"); cb.Type() == js.TypeFunction {
				cb.Invoke(string(data))
			}
			continue
		}
		key := idKey(resp.ID)
		b.mu.Lock()
		resolve, found := b.pending[key]
		delete(b.pending, key)
		b.mu.Unlock()
		if found {
			resolve.Invoke(string(data))
		}
	}
}
