package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/mcp"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// GatewayConfig configures the gateway.
type GatewayConfig struct {
	ToolDispatchUseRegistry bool
}

// Gateway is the main gateway server that handles tool dispatch.
type Gateway struct {
	registry *mcp.ToolRegistry
	logger   *zap.Logger

	toolDispatchUseRegistry bool
	legacyMu                sync.RWMutex
	legacyHandlers          map[string]func(ctx context.Context, args map[string]any) (map[string]any, error)
}

func NewGateway(cfg *GatewayConfig, logger *slog.Logger) *Gateway {
	var zapLogger *zap.Logger
	if logger != nil {
		// Convert slog to zap - for now use default
		zapLogger, _ = zap.NewProduction()
	} else {
		zapLogger, _ = zap.NewProduction()
	}

	g := &Gateway{
		registry:                mcp.NewToolRegistry(),
		logger:                  zapLogger,
		toolDispatchUseRegistry: cfg.ToolDispatchUseRegistry,
		legacyHandlers:          make(map[string]func(ctx context.Context, args map[string]any) (map[string]any, error)),
	}

	// Register built-in tools
	g.registerBuiltinTools()

	return g
}

// RegisterTool registers a tool with the gateway.
func (g *Gateway) RegisterTool(def mcp.ToolDefinition, handler mcp.ToolHandler) error {
	return g.registry.Register(def, handler)
}

// Registry returns the tool registry.
func (g *Gateway) Registry() *mcp.ToolRegistry {
	return g.registry
}

// ExecuteTool executes a tool by name.
func (g *Gateway) ExecuteTool(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	if g.toolDispatchUseRegistry {
		return g.registry.Execute(ctx, name, args)
	}
	return g.executeToolLegacy(ctx, name, args)
}

// executeToolLegacy executes a tool using the legacy handler map.
func (g *Gateway) executeToolLegacy(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	g.legacyMu.RLock()
	handler, ok := g.legacyHandlers[name]
	g.legacyMu.RUnlock()
	if !ok {
		return nil, &mcp.ToolNotFoundError{Name: name}
	}
	return handler(ctx, args)
}

// ListTools returns all registered tool definitions.
func (g *Gateway) ListTools() []mcp.ToolDefinition {
	return g.registry.ListDefinitions()
}

// registerBuiltinTools registers the built-in gateway tools.
func (g *Gateway) registerBuiltinTools() {
	// Health check tool
	g.RegisterTool(mcp.ToolDefinition{
		Name:        "health_check",
		Description: "Check gateway health status",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, g.handleHealthCheck)

	// List tools tool
	g.RegisterTool(mcp.ToolDefinition{
		Name:        "list_tools",
		Description: "List all available tools",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, g.handleListTools)
}

func (g *Gateway) handleHealthCheck(ctx context.Context, args map[string]any) (map[string]any, error) {
	return map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (g *Gateway) handleListTools(ctx context.Context, args map[string]any) (map[string]any, error) {
	return map[string]any{
		"tools": g.registry.ListDefinitions(),
	}, nil
}

// WSConfig configures the WebSocket server.
type WSConfig struct{}

// WSServer handles WebSocket connections for tool execution.
type WSServer struct {
	gateway *Gateway
	logger  *zap.Logger
	config  WSConfig

	mu      sync.RWMutex
	clients map[*WSClient]bool
}

type WSClient struct {
	conn   *websocket.Conn
	send   chan []byte
	server *WSServer
}

func NewWSServer(gateway *Gateway, logger *slog.Logger, config WSConfig) *WSServer {
	var zapLogger *zap.Logger
	if logger != nil {
		zapLogger, _ = zap.NewProduction()
	} else {
		zapLogger, _ = zap.NewProduction()
	}

	return &WSServer{
		gateway: gateway,
		logger:  zapLogger,
		config:  config,
		clients: make(map[*WSClient]bool),
	}
}

func (s *WSServer) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade failed", zap.Error(err))
		return
	}

	client := &WSClient{
		conn:   conn,
		send:   make(chan []byte, 256),
		server: s,
	}

	s.mu.Lock()
	s.clients[client] = true
	s.mu.Unlock()

	go client.writePump()
	go client.readPump()
}

func (c *WSClient) readPump() {
	defer func() {
		c.server.mu.Lock()
		delete(c.server.clients, c)
		c.server.mu.Unlock()
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512 * 1024)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.server.logger.Error("websocket read error", zap.Error(err))
			}
			break
		}

		c.handleMessage(message)
	}
}

func (c *WSClient) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

type wsRequest struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

type wsResponse struct {
	ID     string         `json:"id,omitempty"`
	Result map[string]any `json:"result,omitempty"`
	Error  *wsError       `json:"error,omitempty"`
}

type wsError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *WSClient) handleMessage(data []byte) {
	var req wsRequest
	if err := json.Unmarshal(data, &req); err != nil {
		c.sendError(req.ID, -32700, "Parse error: "+err.Error())
		return
	}

	switch req.Method {
	case "tools/call":
		c.handleToolCall(req)
	case "tools/list":
		c.handleListTools(req)
	default:
		c.sendError(req.ID, -32601, "Method not found: "+req.Method)
	}
}

func (c *WSClient) handleToolCall(req wsRequest) {
	toolName, ok := req.Params["tool"].(string)
	if !ok || toolName == "" {
		c.sendError(req.ID, -32602, "Invalid params: tool name required")
		return
	}

	args, _ := req.Params["arguments"].(map[string]any)
	if args == nil {
		args = make(map[string]any)
	}

	result, err := c.server.gateway.ExecuteTool(context.Background(), toolName, args)
	if err != nil {
		code := -32000
		if _, ok := err.(*mcp.ToolNotFoundError); ok {
			code = -32601
		}
		c.sendError(req.ID, code, err.Error())
		return
	}

	c.sendResponse(req.ID, result)
}

func (c *WSClient) handleListTools(req wsRequest) {
	tools := c.server.gateway.ListTools()
	c.sendResponse(req.ID, map[string]any{"tools": tools})
}

func (c *WSClient) sendResponse(id string, result map[string]any) {
	resp := wsResponse{
		ID:     id,
		Result: result,
	}
	data, _ := json.Marshal(resp)
	select {
	case c.send <- data:
	default:
		c.server.logger.Warn("websocket send buffer full, dropping response")
	}
}

func (c *WSClient) sendError(id string, code int, message string) {
	resp := wsResponse{
		ID: id,
		Error: &wsError{
			Code:    code,
			Message: message,
		},
	}
	data, _ := json.Marshal(resp)
	select {
	case c.send <- data:
	default:
		c.server.logger.Warn("websocket send buffer full, dropping error")
	}
}

// ConnectedClients returns the number of connected clients.
func (s *WSServer) ConnectedClients() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// HandleWS returns an HTTP handler for WebSocket connections.
func (g *Gateway) HandleWS() http.HandlerFunc {
	wsServer := NewWSServer(g, nil, WSConfig{})
	return wsServer.ServeWS
}

// Broadcast sends a message to all connected clients.
func (s *WSServer) Broadcast(message []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for client := range s.clients {
		select {
		case client.send <- message:
		default:
			s.logger.Warn("client send buffer full, dropping broadcast")
		}
	}
}