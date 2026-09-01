package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// PermissionLevel defines the authorization level required to run a tool.
type PermissionLevel string

const (
	// PermissionEveryone allows any chat participant to invoke the tool.
	PermissionEveryone PermissionLevel = "everyone"
	// PermissionAdminOnly restricts tool invocation to bot administrators.
	PermissionAdminOnly PermissionLevel = "admin"
)

// ExecutionContext provides caller and chat environment context for tool execution.
type ExecutionContext struct {
	SenderID   string                 `json:"sender_id"`
	SenderName string                 `json:"sender_name"`
	ChatID     string                 `json:"chat_id"`
	ChatType   string                 `json:"chat_type"`
	IsAdmin    bool                   `json:"is_admin"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Tool defines the interface for all agentic tools.
type Tool interface {
	// Name returns the unique tool identifier used by LLMs (e.g. "web_search", "calculator").
	Name() string
	// Description returns a concise description of what the tool does and when to call it.
	Description() string
	// Parameters returns the JSON Schema for tool parameters expected in function calling.
	Parameters() map[string]interface{}
	// Permission returns the required permission level to execute this tool.
	Permission() PermissionLevel
	// Execute executes the tool with the provided raw JSON arguments and context.
	Execute(ctx context.Context, args json.RawMessage, execCtx ExecutionContext) (string, error)
}

// ToolDefinition matches OpenAI / OpenRouter function calling specification.
type ToolDefinition struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef represents the function metadata within ToolDefinition.
type FunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Registry manages tool registration, discovery, permission filtering, and execution.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry initializes an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// NewDefaultRegistry creates a registry pre-loaded with default tools (Calculator, Weather, WebReader).
func NewDefaultRegistry(openRouterAPIKey string) *Registry {
	r := NewRegistry()
	_ = r.Register(NewCalculatorTool())
	_ = r.Register(NewWeatherTool())
	_ = r.Register(NewWebReaderTool())
	return r
}

// Register registers a tool in the registry. Returns error if tool is nil or name is empty.
func (r *Registry) Register(tool Tool) error {
	if tool == nil {
		return errors.New("cannot register nil tool")
	}
	name := tool.Name()
	if name == "" {
		return errors.New("tool name cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = tool
	return nil
}

// Unregister removes a tool from the registry by name.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// Count returns the total number of registered tools.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// List returns all tools accessible to the caller based on their admin status.
func (r *Registry) List(isAdmin bool) []Tool {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		if tool.Permission() == PermissionAdminOnly && !isAdmin {
			continue
		}
		result = append(result, tool)
	}
	return result
}

// ToToolDefinitions returns standard JSON schema tool definitions for LLM tool calling.
func (r *Registry) ToToolDefinitions(isAdmin bool) []ToolDefinition {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		if tool.Permission() == PermissionAdminOnly && !isAdmin {
			continue
		}
		defs = append(defs, ToolDefinition{
			Type: "function",
			Function: FunctionDef{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.Parameters(),
			},
		})
	}
	return defs
}

// Execute looks up a tool, enforces permissions, and executes it with context.
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage, execCtx ExecutionContext) (string, error) {
	if r == nil {
		return "", fmt.Errorf("tool registry is nil")
	}
	r.mu.RLock()
	tool, exists := r.tools[name]
	r.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("tool %q not found", name)
	}

	if tool.Permission() == PermissionAdminOnly && !execCtx.IsAdmin {
		return "", fmt.Errorf("permission denied: tool %q requires admin privileges", name)
	}

	return tool.Execute(ctx, args, execCtx)
}
