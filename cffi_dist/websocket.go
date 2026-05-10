package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/bogdanfinn/websocket"
	"github.com/google/uuid"
)

// wsConn wraps a *websocket.Conn with per-direction locks. Gorilla allows one
// concurrent reader and one concurrent writer, so we serialize each direction.
type wsConn struct {
	conn     *websocket.Conn
	sendLock sync.Mutex
	recvLock sync.Mutex
}

var (
	wsConnections    = make(map[string]*wsConn)
	wsConnectionsLck sync.Mutex
)

type wsOpenInput struct {
	URL                          string            `json:"url"`
	Headers                      map[string]string `json:"headers"`
	HeaderOrder                  []string          `json:"headerOrder"`
	TLSClientIdentifier          string            `json:"tlsClientIdentifier"`
	WithRandomTLSExtensionOrder  bool              `json:"withRandomTLSExtensionOrder"`
	HandshakeTimeoutMilliseconds int               `json:"handshakeTimeoutMilliseconds"`
	InsecureSkipVerify           bool              `json:"insecureSkipVerify"`
	ProxyURL                     string            `json:"proxyUrl"`
}

//export wsOpen
func wsOpen(payloadJson *C.char) *C.char {
	var input wsOpenInput
	if err := json.Unmarshal([]byte(C.GoString(payloadJson)), &input); err != nil {
		return wsResultJSON(map[string]any{"ok": false, "error": "invalid payload: " + err.Error()})
	}
	if input.URL == "" {
		return wsResultJSON(map[string]any{"ok": false, "error": "url is required"})
	}

	// Resolve TLS profile (default Chrome 133, same default as the rest of the lib).
	profile := profiles.Chrome_133
	if input.TLSClientIdentifier != "" {
		if p, ok := profiles.MappedTLSClients[input.TLSClientIdentifier]; ok {
			profile = p
		}
	}

	opts := []tls_client.HttpClientOption{
		tls_client.WithClientProfile(profile),
		tls_client.WithForceHttp1(), // WebSocket requires HTTP/1.1
	}
	if input.WithRandomTLSExtensionOrder {
		opts = append(opts, tls_client.WithRandomTLSExtensionOrder())
	}
	if input.InsecureSkipVerify {
		opts = append(opts, tls_client.WithInsecureSkipVerify())
	}
	if input.ProxyURL != "" {
		opts = append(opts, tls_client.WithProxyUrl(input.ProxyURL))
	}

	httpClient, err := tls_client.NewHttpClient(nil, opts...)
	if err != nil {
		return wsResultJSON(map[string]any{"ok": false, "error": "create http client: " + err.Error()})
	}

	headers := http.Header{}
	for k, v := range input.Headers {
		headers.Set(k, v)
	}
	if len(input.HeaderOrder) > 0 {
		headers[http.HeaderOrderKey] = input.HeaderOrder
	}

	timeoutMs := input.HandshakeTimeoutMilliseconds
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}

	ws, err := tls_client.NewWebsocket(nil,
		tls_client.WithUrl(input.URL),
		tls_client.WithTlsClient(httpClient),
		tls_client.WithHeaders(headers),
		tls_client.WithHandshakeTimeoutMilliseconds(timeoutMs),
	)
	if err != nil {
		return wsResultJSON(map[string]any{"ok": false, "error": "ws config: " + err.Error()})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	conn, err := ws.Connect(ctx)
	if err != nil {
		return wsResultJSON(map[string]any{"ok": false, "error": "connect: " + err.Error()})
	}

	connID := uuid.NewString()
	wsConnectionsLck.Lock()
	wsConnections[connID] = &wsConn{conn: conn}
	wsConnectionsLck.Unlock()

	return wsResultJSON(map[string]any{"ok": true, "connId": connID})
}

//export wsSend
func wsSend(connIdC *C.char, messageC *C.char, isBinary C.int) *C.char {
	connID := C.GoString(connIdC)

	wsConnectionsLck.Lock()
	wc := wsConnections[connID]
	wsConnectionsLck.Unlock()
	if wc == nil {
		return wsResultJSON(map[string]any{"ok": false, "error": "unknown connId: " + connID})
	}

	msgType := websocket.TextMessage
	var data []byte
	message := C.GoString(messageC)
	if isBinary != 0 {
		msgType = websocket.BinaryMessage
		decoded, err := base64.StdEncoding.DecodeString(message)
		if err != nil {
			return wsResultJSON(map[string]any{"ok": false, "error": "base64 decode: " + err.Error()})
		}
		data = decoded
	} else {
		data = []byte(message)
	}

	wc.sendLock.Lock()
	err := wc.conn.WriteMessage(msgType, data)
	wc.sendLock.Unlock()
	if err != nil {
		return wsResultJSON(map[string]any{"ok": false, "error": "write: " + err.Error()})
	}
	return wsResultJSON(map[string]any{"ok": true})
}

//export wsRecv
func wsRecv(connIdC *C.char, timeoutMs C.int) *C.char {
	connID := C.GoString(connIdC)

	wsConnectionsLck.Lock()
	wc := wsConnections[connID]
	wsConnectionsLck.Unlock()
	if wc == nil {
		return wsResultJSON(map[string]any{"type": "error", "error": "unknown connId: " + connID})
	}

	wc.recvLock.Lock()
	defer wc.recvLock.Unlock()

	if timeoutMs > 0 {
		_ = wc.conn.SetReadDeadline(time.Now().Add(time.Duration(timeoutMs) * time.Millisecond))
	} else {
		_ = wc.conn.SetReadDeadline(time.Time{})
	}

	msgType, data, err := wc.conn.ReadMessage()
	if err != nil {
		if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
			return wsResultJSON(map[string]any{"type": "timeout"})
		}
		if closeErr, ok := err.(*websocket.CloseError); ok {
			return wsResultJSON(map[string]any{
				"type":   "close",
				"code":   closeErr.Code,
				"reason": closeErr.Text,
			})
		}
		return wsResultJSON(map[string]any{"type": "error", "error": err.Error()})
	}

	switch msgType {
	case websocket.TextMessage:
		return wsResultJSON(map[string]any{"type": "text", "data": string(data)})
	case websocket.BinaryMessage:
		return wsResultJSON(map[string]any{
			"type": "binary",
			"data": base64.StdEncoding.EncodeToString(data),
		})
	default:
		return wsResultJSON(map[string]any{"type": "unknown", "code": msgType})
	}
}

//export wsClose
func wsClose(connIdC *C.char, code C.int, reasonC *C.char) *C.char {
	connID := C.GoString(connIdC)
	reason := C.GoString(reasonC)

	wsConnectionsLck.Lock()
	wc := wsConnections[connID]
	delete(wsConnections, connID)
	wsConnectionsLck.Unlock()
	if wc == nil {
		return wsResultJSON(map[string]any{"ok": false, "error": "unknown connId: " + connID})
	}

	if code > 0 {
		wc.sendLock.Lock()
		_ = wc.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(int(code), reason))
		wc.sendLock.Unlock()
	}
	_ = wc.conn.Close()
	return wsResultJSON(map[string]any{"ok": true})
}

// wsResultJSON encodes a payload, allocates a C string, and tracks it in the
// shared unsafePointers registry under an "id" so callers can free it via
// freeMemory if they want to. Same convention as the existing request export.
func wsResultJSON(payload map[string]any) *C.char {
	id := uuid.NewString()
	payload["id"] = id
	out, _ := json.Marshal(payload)
	s := C.CString(string(out))
	unsafePointersLck.Lock()
	unsafePointers[id] = s
	unsafePointersLck.Unlock()
	return s
}
