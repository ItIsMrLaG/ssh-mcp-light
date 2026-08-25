package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ssh-mcp-light/internal/config"
	"ssh-mcp-light/internal/mcpserver"
	"ssh-mcp-light/internal/sshlayer"
	"ssh-mcp-light/internal/sshtest"
	"ssh-mcp-light/internal/tools"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// testEnv wires an sshtest fixture (standing in for one VM), a real
// project-root temp directory, and a live in-memory MCP session, so
// integration tests exercise the real tool handlers end to end.
type testEnv struct {
	t               *testing.T
	server          *sshtest.Server
	projectRoot     string
	remoteLocalRoot string
	session         *mcp.ClientSession
	vm              config.VM
	Deps            *tools.Deps
}

func (e *testEnv) deps() *tools.Deps { return e.Deps }

func newEnv(t *testing.T, remoteLocalRoot string) *testEnv {
	t.Helper()
	s := sshtest.Start(t)
	projectRoot := t.TempDir()

	host, port := s.SplitHostPort()
	vm := config.VM{
		Name:         "testvm",
		Address:      host,
		Port:         port,
		User:         s.AuthorizedUser,
		IdentityFile: sshtest.WriteKeyFile(t, s),
		RemoteRoot:   s.Root,
	}
	vmConfig := &config.VMConfig{
		VMs:   map[string]config.VM{"testvm": vm},
		Order: []string{"testvm"},
	}

	canon, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	projectConfig := &config.ProjectConfig{
		ProjectRoot:          projectRoot,
		ProjectRootCanonical: canon,
		RemoteLocalRoot:      remoteLocalRoot,
	}

	deps := &tools.Deps{
		VMConfig:      vmConfig,
		ProjectConfig: projectConfig,
		Connector:     sshlayer.NewConnector(),
	}
	server := mcpserver.New(deps)

	c1, c2 := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = server.Run(ctx, c1) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, c2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return &testEnv{t: t, server: s, projectRoot: projectRoot, remoteLocalRoot: remoteLocalRoot, session: session, vm: vm, Deps: deps}
}

// writeLocal creates a file under the project root. A test can also mutate
// e.Deps.ProjectConfig (e.g. .Ignore) directly after newEnv — the handlers
// read it fresh on every call, no live re-registration needed.
func (e *testEnv) writeLocal(rel, content string) {
	e.t.Helper()
	p := filepath.Join(e.projectRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) remoteBase() string {
	return filepath.ToSlash(filepath.Join(e.server.Root, e.remoteLocalRoot))
}

func (e *testEnv) writeRemote(rel, content string) {
	e.t.Helper()
	p := filepath.Join(e.server.Root, e.remoteLocalRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) call(tool string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := e.session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		e.t.Fatalf("CallTool(%s): protocol-level error (a tool-level failure should never surface as one): %v", tool, err)
	}
	var out map[string]any
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, &out); err != nil {
			e.t.Fatalf("CallTool(%s): structuredContent did not decode as an object: %v", tool, err)
		}
	}
	return res, out
}

// --- host_list, path ---

func TestHostList(t *testing.T) {
	e := newEnv(t, "projects/api")
	res, out := e.call("host_list", map[string]any{})
	if res.IsError {
		t.Fatalf("unexpected error result: %v", out)
	}
	vms, _ := out["vms"].([]any)
	if len(vms) != 1 {
		t.Fatalf("expected 1 VM, got %v", out)
	}
	first := vms[0].(map[string]any)
	if first["name"] != "testvm" {
		t.Fatalf("unexpected vm entry: %v", first)
	}
}

func TestPath_NoVM(t *testing.T) {
	e := newEnv(t, "projects/api")
	res, out := e.call("path", map[string]any{})
	if res.IsError {
		t.Fatalf("unexpected error: %v", out)
	}
	if out["path"] != e.projectRoot {
		t.Fatalf("path = %v, want %v", out["path"], e.projectRoot)
	}
}

func TestPath_WithVM(t *testing.T) {
	e := newEnv(t, "projects/api")
	res, out := e.call("path", map[string]any{"vm": "testvm"})
	if res.IsError {
		t.Fatalf("unexpected error: %v", out)
	}
	want := e.remoteBase()
	if out["path"] != want {
		t.Fatalf("path = %v, want %v", out["path"], want)
	}
}

// T-UNKNOWN-VM across all four VM-taking tools.
func TestUnknownVM_AllTools(t *testing.T) {
	e := newEnv(t, "projects/api")
	e.writeLocal("a.txt", "x")

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"path", map[string]any{"vm": "prod"}},
		{"exec", map[string]any{"vm": "prod", "cmd": "true"}},
		{"push", map[string]any{"vm": "prod", "files": []string{"a.txt"}}},
		{"sync", map[string]any{"vm": "prod"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			res, out := e.call(tc.tool, tc.args)
			if !res.IsError {
				t.Fatalf("expected an error result for unknown vm")
			}
			errObj, _ := out["error"].(map[string]any)
			if errObj["code"] != "E_UNKNOWN_VM" {
				t.Fatalf("error code = %v, want E_UNKNOWN_VM", errObj["code"])
			}
		})
	}
}

// T-MCP-RESULT-ENVELOPE: success and failure both carry
// structuredContent and are never a JSON-RPC-level error.
func TestMCPResultEnvelope(t *testing.T) {
	e := newEnv(t, "projects/api")

	res, _ := e.call("host_list", map[string]any{})
	if res.IsError {
		t.Fatalf("success call must not set isError")
	}
	if res.StructuredContent == nil {
		t.Fatalf("success call must set structuredContent")
	}
	if len(res.Content) == 0 {
		t.Fatalf("success call must mirror structuredContent as text content")
	}

	res2, out2 := e.call("path", map[string]any{"vm": "does-not-exist"})
	if !res2.IsError {
		t.Fatalf("failure call must set isError")
	}
	if _, ok := out2["error"]; !ok {
		t.Fatalf("failure structuredContent must carry an 'error' object, got %v", out2)
	}
}
