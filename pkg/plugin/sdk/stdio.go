package sdk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
)

// serveIO reads newline-delimited JSON-RPC from r and writes responses to w.
// It is the internal entry point used by Serve and tests. Both sides act as
// client and server: a plugin's Check/Apply may call back to the host for
// handle ops (RunCommand/PutFile/GetFile) over the same channel.
func serveIO(m Module, r io.Reader, w io.Writer) {
	srv := newServer(m, r, w, nil)
	srv.run()
}

// server is the plugin-side JSON-RPC endpoint.
type server struct {
	mod   Module
	codec *codec
	info  TargetInfo // set at initialize
}

func newServer(m Module, r io.Reader, w io.Writer, closeFn func() error) *server {
	s := &server{mod: m}
	s.codec = newCodec(r, w, s.handleRequest, s.handleNotification, closeFn)
	return s
}

func (s *server) run() {
	s.codec.start()
	// Block until the read loop ends (peer closed or transport error), then
	// wait for any in-flight request handlers so their responses are written.
	<-s.codec.ctx.Done()
	s.codec.handlerWG.Wait()
}

// handleRequest dispatches an incoming host→plugin request.
func (s *server) handleRequest(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var p InitializeParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &rpcError{Code: -32602, Message: "initialize params: " + err.Error()}
			}
		}
		s.info = p.Target
		return InitializeResult{
			Name:            s.mod.Name(),
			Version:         s.mod.Version(),
			ProtocolVersion: ProtocolVersion,
		}, nil

	case "check":
		h := &serverHandle{info: s.info, codec: s.codec}
		res, err := s.mod.Check(argsFromParams(params), h)
		if err != nil {
			res.Error = err.Error()
		}
		return res, nil

	case "apply":
		h := &serverHandle{info: s.info, codec: s.codec}
		res, err := s.mod.Apply(argsFromParams(params), h)
		if err != nil {
			res.Error = err.Error()
		}
		return res, nil

	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

func (s *server) handleNotification(_ json.RawMessage) {
	// The host does not send notifications to the plugin in v1.
}

// serverHandle is the Handle given to a plugin's Check/Apply. Info returns the
// TargetInfo delivered at initialize; Output emits an output notification;
// RunCommand/PutFile/GetFile send requests to the host over the same codec.
type serverHandle struct {
	info  TargetInfo
	codec *codec
}

func (h *serverHandle) RunCommand(ctx context.Context, script string) (CommandResult, error) {
	var res CommandResult
	if err := h.codec.call(ctx, "run_command", map[string]any{"script": script}, &res); err != nil {
		return CommandResult{}, err
	}
	return res, nil
}

func (h *serverHandle) PutFile(ctx context.Context, path string, data []byte) error {
	return h.codec.call(ctx, "put_file", map[string]any{
		"path": path,
		"data": encodeBase64(data),
	}, nil)
}

func (h *serverHandle) GetFile(ctx context.Context, path string) ([]byte, error) {
	var res struct {
		Data string `json:"data"`
	}
	if err := h.codec.call(ctx, "get_file", map[string]any{"path": path}, &res); err != nil {
		return nil, err
	}
	return decodeBase64(res.Data)
}

func (h *serverHandle) Info() TargetInfo { return h.info }

func (h *serverHandle) Output(line string) {
	_ = h.codec.notify("output", map[string]any{"line": line})
}

// argsFromParams extracts the "args" key from JSON-RPC params, returning an
// empty map when absent.
func argsFromParams(params json.RawMessage) map[string]any {
	if len(params) == 0 {
		return map[string]any{}
	}
	var p struct {
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return map[string]any{}
	}
	if p.Args == nil {
		return map[string]any{}
	}
	return p.Args
}

// encodeBase64 / decodeBase64 carry file bytes over JSON. PutFile/GetFile use
// them so the host can chunk across high-latency transports without the plugin
// caring about framing.
func encodeBase64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

func decodeBase64(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(s)
}
