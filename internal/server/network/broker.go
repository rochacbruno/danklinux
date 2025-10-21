package network

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/AvengeMedia/danklinux/internal/errdefs"
)

type PromptBroker interface {
	Ask(ctx context.Context, req PromptRequest) (token string, err error)
	Wait(ctx context.Context, token string) (PromptReply, error)
	Resolve(token string, reply PromptReply) error
}

type defaultBroker struct {
	mu       sync.RWMutex
	pending  map[string]chan PromptReply
	requests map[string]PromptRequest
}

func NewDefaultBroker() PromptBroker {
	return &defaultBroker{
		pending:  make(map[string]chan PromptReply),
		requests: make(map[string]PromptRequest),
	}
}

func (b *defaultBroker) Ask(ctx context.Context, req PromptRequest) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}

	replyChan := make(chan PromptReply, 1)
	b.mu.Lock()
	b.pending[token] = replyChan
	b.requests[token] = req
	b.mu.Unlock()

	return token, nil
}

func (b *defaultBroker) Wait(ctx context.Context, token string) (PromptReply, error) {
	b.mu.RLock()
	replyChan, exists := b.pending[token]
	b.mu.RUnlock()

	if !exists {
		return PromptReply{}, fmt.Errorf("unknown token: %s", token)
	}

	select {
	case <-ctx.Done():
		b.cleanup(token)
		return PromptReply{}, errdefs.ErrSecretPromptTimeout
	case reply := <-replyChan:
		b.cleanup(token)
		if reply.Cancel {
			return reply, errdefs.ErrSecretPromptCancelled
		}
		return reply, nil
	}
}

func (b *defaultBroker) Resolve(token string, reply PromptReply) error {
	b.mu.RLock()
	replyChan, exists := b.pending[token]
	b.mu.RUnlock()

	if !exists {
		return fmt.Errorf("unknown or expired token: %s", token)
	}

	select {
	case replyChan <- reply:
		return nil
	default:
		return fmt.Errorf("failed to deliver reply for token: %s", token)
	}
}

func (b *defaultBroker) cleanup(token string) {
	b.mu.Lock()
	delete(b.pending, token)
	delete(b.requests, token)
	b.mu.Unlock()
}

func generateToken() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
