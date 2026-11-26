package service

import (
	"context"
	"errors"
	"sync"
	"time"
)

var errOpenAIPassthroughStreamIdle = errors.New("upstream response stream idle timeout")

// The watchdog starts after the first complete output frame. It cancels the
// upstream request, not the downstream context, so a blocked read wakes up
// and the client can still receive a retryable terminal event.
type openAIPassthroughStreamIdle struct {
	ctx      context.Context
	cancel   context.CancelCauseFunc
	interval time.Duration
	mu       sync.Mutex
	timer    *time.Timer
	deadline time.Time
	stopped  bool
}

func newOpenAIPassthroughStreamIdle(ctx context.Context, cancel context.CancelCauseFunc, interval time.Duration) *openAIPassthroughStreamIdle {
	return &openAIPassthroughStreamIdle{ctx: ctx, cancel: cancel, interval: interval}
}

func (g *openAIPassthroughStreamIdle) progress() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return
	}
	g.deadline = time.Now().Add(g.interval)
	if g.timer == nil {
		g.timer = time.AfterFunc(g.interval, g.checkDeadline)
	}
}

func (g *openAIPassthroughStreamIdle) checkDeadline() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return
	}
	if remaining := time.Until(g.deadline); remaining > 0 {
		g.timer.Reset(remaining)
		return
	}
	g.stopped = true
	g.cancel(errOpenAIPassthroughStreamIdle)
}

func (g *openAIPassthroughStreamIdle) stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopped = true
	if g.timer != nil {
		g.timer.Stop()
	}
}

func (g *openAIPassthroughStreamIdle) expired() bool {
	return errors.Is(context.Cause(g.ctx), errOpenAIPassthroughStreamIdle)
}
