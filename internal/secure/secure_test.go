package secure

import (
	"bytes"
	"testing"
)

func TestVaultBindingAndAuthentication(t *testing.T) {
	v, e := New(bytes.Repeat([]byte{1}, 32))
	if e != nil {
		t.Fatal(e)
	}
	sealed := v.Seal("credential-1", []byte("secret"))
	if b, e := v.Open("credential-1", sealed); e != nil || string(b) != "secret" {
		t.Fatal("round trip", e)
	}
	if _, e := v.Open("credential-2", sealed); e == nil {
		t.Fatal("ciphertext accepted with wrong AAD")
	}
	h := Password("a sufficiently long password")
	if !CheckPassword("a sufficiently long password", h) || CheckPassword("wrong", h) {
		t.Fatal("password verification")
	}
}
