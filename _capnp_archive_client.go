//go:build never

// Package zap provides Go bindings for ZAP (Zero-Copy App Proto).
//
// ZAP is a high-performance Cap'n Proto RPC protocol for AI agent communication.
package zap

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
)

// Client is a ZAP protocol client.
type Client struct {
	conn   *rpc.Conn
	zap    Zap
	mu     sync.RWMutex
	closed bool
}

// Connect establishes a connection to a ZAP server.
func Connect(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	transport := rpc.NewStreamTransport(conn)
	rpcConn := rpc.NewConn(transport, nil)

	return &Client{
		conn: rpcConn,
		zap:  Zap(rpcConn.Bootstrap(context.Background())),
	}, nil
}

// Close closes the connection to the server.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	return c.conn.Close()
}

// Init initializes the connection with client info.
func (c *Client) Init(ctx context.Context, name, version string) (*ServerInfoData, error) {
	future, release := c.zap.Init(ctx, func(p Zap_init_Params) error {
		client, err := p.NewClient()
		if err != nil {
			return err
		}
		if err := client.SetName(name); err != nil {
			return err
		}
		return client.SetVersion(version)
	})
	defer release()

	result, err := future.Struct()
	if err != nil {
		return nil, err
	}

	server, err := result.Server()
	if err != nil {
		return nil, err
	}

	return serverInfoToData(server)
}

// ListTools returns the list of available tools.
func (c *Client) ListTools(ctx context.Context) ([]ToolData, error) {
	future, release := c.zap.ListTools(ctx, nil)
	defer release()

	result, err := future.Struct()
	if err != nil {
		return nil, err
	}

	tools, err := result.Tools()
	if err != nil {
		return nil, err
	}

	list, err := tools.Tools()
	if err != nil {
		return nil, err
	}

	var data []ToolData
	for i := 0; i < list.Len(); i++ {
		tool := list.At(i)
		d, err := toolToData(tool)
		if err != nil {
			return nil, err
		}
		data = append(data, *d)
	}

	return data, nil
}

// CallTool calls a tool with the given arguments.
func (c *Client) CallTool(ctx context.Context, name string, args any) (*ToolResultData, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal args: %w", err)
	}

	future, release := c.zap.CallTool(ctx, func(p Zap_callTool_Params) error {
		call, err := p.NewCall()
		if err != nil {
			return err
		}
		if err := call.SetName(name); err != nil {
			return err
		}
		return call.SetArgs(argsJSON)
	})
	defer release()

	result, err := future.Struct()
	if err != nil {
		return nil, err
	}

	toolResult, err := result.Result()
	if err != nil {
		return nil, err
	}

	return toolResultToData(toolResult)
}

// ListResources returns the list of available resources.
func (c *Client) ListResources(ctx context.Context) ([]ResourceData, error) {
	future, release := c.zap.ListResources(ctx, nil)
	defer release()

	result, err := future.Struct()
	if err != nil {
		return nil, err
	}

	resources, err := result.Resources()
	if err != nil {
		return nil, err
	}

	list, err := resources.Resources()
	if err != nil {
		return nil, err
	}

	var data []ResourceData
	for i := 0; i < list.Len(); i++ {
		resource := list.At(i)
		d, err := resourceToData(resource)
		if err != nil {
			return nil, err
		}
		data = append(data, *d)
	}

	return data, nil
}

// ReadResource reads the content of a resource.
func (c *Client) ReadResource(ctx context.Context, uri string) (*ResourceContentData, error) {
	future, release := c.zap.ReadResource(ctx, func(p Zap_readResource_Params) error {
		return p.SetUri(uri)
	})
	defer release()

	result, err := future.Struct()
	if err != nil {
		return nil, err
	}

	content, err := result.Content()
	if err != nil {
		return nil, err
	}

	return resourceContentToData(content)
}

// ListPrompts returns the list of available prompts.
func (c *Client) ListPrompts(ctx context.Context) ([]PromptData, error) {
	future, release := c.zap.ListPrompts(ctx, nil)
	defer release()

	result, err := future.Struct()
	if err != nil {
		return nil, err
	}

	prompts, err := result.Prompts()
	if err != nil {
		return nil, err
	}

	list, err := prompts.Prompts()
	if err != nil {
		return nil, err
	}

	var data []PromptData
	for i := 0; i < list.Len(); i++ {
		prompt := list.At(i)
		d, err := promptToData(prompt)
		if err != nil {
			return nil, err
		}
		data = append(data, *d)
	}

	return data, nil
}

// GetPrompt retrieves a prompt with the given name and arguments.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) ([]PromptMessageData, error) {
	future, release := c.zap.GetPrompt(ctx, func(p Zap_getPrompt_Params) error {
		if err := p.SetName(name); err != nil {
			return err
		}

		meta, err := p.NewArgs()
		if err != nil {
			return err
		}

		entries, err := meta.NewEntries(int32(len(args)))
		if err != nil {
			return err
		}

		i := 0
		for k, v := range args {
			entry := entries.At(i)
			if err := entry.SetKey(k); err != nil {
				return err
			}
			if err := entry.SetValue(v); err != nil {
				return err
			}
			i++
		}

		return nil
	})
	defer release()

	result, err := future.Struct()
	if err != nil {
		return nil, err
	}

	messages, err := result.Messages()
	if err != nil {
		return nil, err
	}

	var data []PromptMessageData
	for i := 0; i < messages.Len(); i++ {
		msg := messages.At(i)
		d, err := promptMessageToData(msg)
		if err != nil {
			return nil, err
		}
		data = append(data, *d)
	}

	return data, nil
}

// Log sends a log message to the server.
func (c *Client) Log(ctx context.Context, level LogLevel, message string, data any) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	future, release := c.zap.Log(ctx, func(p Zap_log_Params) error {
		p.SetLevel(Zap_LogLevel(level))
		if err := p.SetMessage(message); err != nil {
			return err
		}
		return p.SetData(dataJSON)
	})
	defer release()

	_, err = future.Struct()
	return err
}

// LogLevel represents log severity levels.
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)
