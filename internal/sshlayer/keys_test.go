package sshlayer_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ssh-mcp-light/internal/sshlayer"
	"ssh-mcp-light/internal/sshtest"

	"golang.org/x/crypto/ssh"
)

// T-KEY-MISSING.
func TestConnect_KeyMissing(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)
	vm.IdentityFile = filepath.Join(t.TempDir(), "does-not-exist")

	c := sshlayer.NewConnector()
	_, _, _, err := c.Connect(context.Background(), vm)
	if err == nil {
		t.Fatal("expected an error")
	}
	var target *sshlayer.KeyMissingError
	if !errors.As(err, &target) {
		t.Fatalf("expected *KeyMissingError, got %T: %v", err, err)
	}
}

// T-KEY-UNREADABLE: both an OS-level unreadable file
// and a file that reads fine but isn't a supported key format.
func TestConnect_KeyUnreadable(t *testing.T) {
	s := sshtest.Start(t)

	t.Run("corrupt format", func(t *testing.T) {
		vm := testVM(t, s)
		path := filepath.Join(t.TempDir(), "id_bad")
		if err := os.WriteFile(path, []byte("not a key"), 0o600); err != nil {
			t.Fatal(err)
		}
		vm.IdentityFile = path

		c := sshlayer.NewConnector()
		_, _, _, err := c.Connect(context.Background(), vm)
		var target *sshlayer.KeyUnreadableError
		if !errors.As(err, &target) {
			t.Fatalf("expected *KeyUnreadableError, got %T: %v", err, err)
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: permission bits are not enforced")
		}
		vm := testVM(t, s)
		path := filepath.Join(t.TempDir(), "id_noperm")
		if err := os.WriteFile(path, []byte("x"), 0o000); err != nil {
			t.Fatal(err)
		}
		vm.IdentityFile = path

		c := sshlayer.NewConnector()
		_, _, _, err := c.Connect(context.Background(), vm)
		if err == nil {
			t.Fatal("expected an error")
		}
	})
}

// T-KEY-ENCRYPTED: a passphrase-protected key yields
// E_KEY_ENCRYPTED, with no prompt (there is nothing to prompt on stdio,
// and Connect never blocks waiting for one).
func TestConnect_KeyEncrypted(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)
	vm.IdentityFile = writeEncryptedKey(t)

	c := sshlayer.NewConnector()
	_, _, _, err := c.Connect(context.Background(), vm)
	var target *sshlayer.KeyEncryptedError
	if !errors.As(err, &target) {
		t.Fatalf("expected *KeyEncryptedError, got %T: %v", err, err)
	}
}

// writeEncryptedKey generates a fresh RSA key, encrypts it with a
// passphrase, and returns the path to the written PEM file.
func writeEncryptedKey(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_encrypted")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
