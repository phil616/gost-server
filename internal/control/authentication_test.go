package control

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"github.com/altasci/altascitunnel/internal/model"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/altasci/altascitunnel/internal/store"
	"github.com/google/uuid"
	"sync"
	"testing"
	"time"
)

func TestEnrollmentProofExpiryAndConcurrentConsumption(t *testing.T) {
	ctx := context.Background()
	repo := store.NewMemory()
	vault, _ := secure.New(bytes.Repeat([]byte{8}, 32))
	server := New(repo, vault, nil, "")
	now := time.Now()
	server.Now = func() time.Time { return now }
	token := "single-use-test-token"
	repo.Update(ctx, func(st *model.State) error {
		st.Enrollments["enroll"] = model.Enrollment{ID: "enroll", Digest: model.Digest([]byte(token)), ExpiresAt: now.Add(time.Minute)}
		return nil
	})
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	request := authenticate{Token: token, RequestID: uuid.NewString(), PublicKey: base64.RawURLEncoding.EncodeToString(pub), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte("nonce-1")))}
	if _, e := server.authenticate(ctx, "register", "nonce-2", request); e == nil {
		t.Fatal("replayed signature accepted")
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pub, key, _ := ed25519.GenerateKey(rand.Reader)
			a := authenticate{Token: token, RequestID: uuid.NewString(), PublicKey: base64.RawURLEncoding.EncodeToString(pub), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte("concurrent")))}
			_, e := server.authenticate(ctx, "register", "concurrent", a)
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("single-use token enrolled %d devices", successes)
	}
	repo.Update(ctx, func(st *model.State) error {
		st.Enrollments["expired"] = model.Enrollment{ID: "expired", Digest: model.Digest([]byte("expired-token")), ExpiresAt: now.Add(-time.Second)}
		return nil
	})
	request.Token = "expired-token"
	if _, e := server.authenticate(ctx, "register", "nonce-1", request); e == nil {
		t.Fatal("expired enrollment accepted")
	}
}
