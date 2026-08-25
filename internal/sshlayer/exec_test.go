package sshlayer

import "testing"

// Pure unit coverage of BuildCommand's quoting; the integration half
// (verifying the remote process actually receives these argv elements
// unexpanded) is T-EXEC-QUOTING-SPECIAL-CHARS in internal/tools's
// integration tests.
func TestBuildCommand_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		args []string
		cwd  string
		want string
	}{
		{
			name: "no args, no cwd",
			cmd:  "ls",
			want: `'ls'`,
		},
		{
			name: "args joined with spaces",
			cmd:  "systemctl",
			args: []string{"status", "api"},
			want: `'systemctl' 'status' 'api'`,
		},
		{
			name: "cwd adds a cd prefix",
			cmd:  "ls",
			cwd:  "/srv/agents",
			want: `cd '/srv/agents' && 'ls'`,
		},
		{
			name: "single quote in an argument is escaped",
			cmd:  "echo",
			args: []string{"it's here"},
			want: `'echo' 'it'\''s here'`,
		},
		{
			name: "shell metacharacters are inert, not expanded",
			cmd:  "echo",
			args: []string{"$HOME; rm -rf /; `whoami`"},
			want: `'echo' '$HOME; rm -rf /; ` + "`whoami`" + `'`,
		},
		{
			name: "cwd containing a space is quoted",
			cmd:  "pwd",
			cwd:  "/srv/my agents",
			want: `cd '/srv/my agents' && 'pwd'`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildCommand(tc.cmd, tc.args, tc.cwd)
			if got != tc.want {
				t.Fatalf("BuildCommand(%q, %v, %q) = %q, want %q", tc.cmd, tc.args, tc.cwd, got, tc.want)
			}
		})
	}
}
