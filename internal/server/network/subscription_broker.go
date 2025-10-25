package network

import (
	"context"
	"fmt"
	"sync"

	"github.com/AvengeMedia/danklinux/internal/errdefs"
	"github.com/AvengeMedia/danklinux/internal/log"
)

type SubscriptionBroker struct {
	mu              sync.RWMutex
	pending         map[string]chan PromptReply
	requests        map[string]PromptRequest
	broadcastPrompt func(CredentialPrompt)
}

func NewSubscriptionBroker(broadcastPrompt func(CredentialPrompt)) PromptBroker {
	return &SubscriptionBroker{
		pending:         make(map[string]chan PromptReply),
		requests:        make(map[string]PromptRequest),
		broadcastPrompt: broadcastPrompt,
	}
}

func (b *SubscriptionBroker) Ask(ctx context.Context, req PromptRequest) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}

	replyChan := make(chan PromptReply, 1)
	b.mu.Lock()
	b.pending[token] = replyChan
	b.requests[token] = req
	b.mu.Unlock()

	if b.broadcastPrompt != nil {
		prompt := CredentialPrompt{
			Token:   token,
			SSID:    req.SSID,
			Setting: req.SettingName,
			Fields:  req.Fields,
			Hints:   req.Hints,
			Reason:  req.Reason,
		}
		b.broadcastPrompt(prompt)
	}

	return token, nil
}

func (b *SubscriptionBroker) Wait(ctx context.Context, token string) (PromptReply, error) {
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

func (b *SubscriptionBroker) Resolve(token string, reply PromptReply) error {
	b.mu.RLock()
	replyChan, exists := b.pending[token]
	b.mu.RUnlock()

	if !exists {
		log.Warnf("[SubscriptionBroker] Resolve: unknown or expired token: %s", token)
		return fmt.Errorf("unknown or expired token: %s", token)
	}

	select {
	case replyChan <- reply:
		return nil
	default:
		log.Warnf("[SubscriptionBroker] Resolve: failed to deliver reply for token %s (channel full or closed)", token)
		return fmt.Errorf("failed to deliver reply for token: %s", token)
	}
}

func (b *SubscriptionBroker) cleanup(token string) {
	b.mu.Lock()
	delete(b.pending, token)
	delete(b.requests, token)
	b.mu.Unlock()
}
