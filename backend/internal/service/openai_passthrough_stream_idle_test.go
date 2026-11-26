package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIPassthroughIdleStream(t *testing.T) {
	for _, tc := range []struct {
		name     string
		progress bool
		prelude  bool
	}{
		{name: "comments_dont_keep_stalled_output_alive"},
		{name: "complete_events_extend_deadline", progress: true},
		{name: "unarmed_during_initial_reasoning", prelude: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const interval = 240 * time.Millisecond
			const first = "data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_idle\",\"delta\":\"hello\",\"sequence_number\":2}\n\n"
			const completed = "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_idle\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
			upstreamCanceled := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				flush := w.(http.Flusher).Flush
				flush()
				if tc.prelude {
					time.Sleep(interval * 2)
				}
				_, _ = fmt.Fprint(w, first)
				flush()
				if tc.prelude {
					_, _ = fmt.Fprint(w, completed)
					return
				}
				ticker := time.NewTicker(interval / 8)
				defer ticker.Stop()
				for i := 0; ; i++ {
					select {
					case <-r.Context().Done():
						close(upstreamCanceled)
						return
					case <-ticker.C:
						if tc.progress {
							if i == 12 {
								_, _ = fmt.Fprint(w, completed)
								return
							}
							_, _ = fmt.Fprint(w, first)
						} else {
							_, _ = fmt.Fprint(w, ": keepalive\n\n")
						}
						flush()
					}
				}
			}))
			defer upstream.Close()
			base, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			ctx, cancel := context.WithCancelCause(base)
			defer cancel(nil)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
			require.NoError(t, err)
			resp, err := upstream.Client().Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			c, rec := newPassthroughKeepaliveTestContext(t)
			svc := &OpenAIGatewayService{}
			_, err = svc.handleStreamingResponsePassthrough(base, resp, c, &Account{ID: 3, Platform: PlatformOpenAI}, time.Now(), "gpt-6.2-sol", "gpt-6.2-sol", newOpenAIPassthroughStreamIdle(ctx, cancel, interval))
			body := rec.Body.String()
			require.Contains(t, body, first)
			if tc.progress || tc.prelude {
				require.NoError(t, err)
				require.Contains(t, body, completed)
				require.NotContains(t, body, "response.failed")
				return
			}
			require.ErrorIs(t, err, errOpenAIPassthroughStreamIdle)
			require.Contains(t, body, ": keepalive")
			require.Equal(t, 1, strings.Count(body, "event: response.failed\n"))
			require.Contains(t, body, "\"code\":\"server_error\"")
			require.NotContains(t, body, "response.completed")
			require.True(t, IsResponseCommitted(c))
			select {
			case <-upstreamCanceled:
			case <-base.Done():
				t.Fatal("upstream did not observe cancellation")
			}
		})
	}
}
