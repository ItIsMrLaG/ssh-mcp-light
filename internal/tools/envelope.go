package tools

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// success and failure are the only two ways a handler in this package
// produces a *mcp.CallToolResult; every RegisterX function's handler
// always returns (result, nil) from these, never a bare Go error. That's
// deliberate: the MCP SDK's ToolHandler treats a returned error as a
// protocol-level JSON-RPC error, and its own doc comment says so plainly.
// Building the result by hand here is what keeps a tool-level failure
// (unknown VM, bad key, path traversal, ...) a normal, structured
// CallToolResult with isError:true instead — visible to the calling LLM
// as data it can act on, not a transport-level fault.

// success builds the result for a successful call: structuredContent set
// to v, mirrored as JSON text content for clients that only read
// unstructured content, isError left unset.
func success(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(b)}},
		StructuredContent: json.RawMessage(b),
	}, nil
}

// failure builds the result for a tool-level error: isError:true,
// structuredContent is the {"error": {...}} envelope.
func failure(code, message string, target ResolvedTarget) (*mcp.CallToolResult, error) {
	env := ErrorEnvelope{Error: ErrorBody{Code: code, Message: message, ResolvedTarget: target}}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{
		IsError:           true,
		Content:           []mcp.Content{&mcp.TextContent{Text: string(b)}},
		StructuredContent: json.RawMessage(b),
	}, nil
}

// invalidArgument builds an E_INVALID_ARGUMENT failure for a semantic
// rule the JSON Schema itself can't express or enforce (the schema
// already rejects most shape/range problems before a handler even runs).
func invalidArgument(field, reason string) (*mcp.CallToolResult, error) {
	return failure(codeInvalidArgument, "invalid argument \""+field+"\": "+reason, noVMTarget())
}
