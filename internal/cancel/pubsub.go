package cancel

import (
	"context"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Publisher publishes a job id to the Channel, waking any worker process
// whose Subscriber is currently running that job (D-03). It wraps a
// *redis.Client constructed from the SAME Redis instance/address the
// existing asynq queue uses (queue.RedisOpt()) -- no new infrastructure, no
// new Go dependency.
type Publisher struct {
	rdb *redis.Client
}

// NewPublisher wraps an existing *redis.Client for publishing cancel
// signals. The caller owns the client's lifecycle (Close).
func NewPublisher(rdb *redis.Client) *Publisher {
	return &Publisher{rdb: rdb}
}

// Publish announces that job id should be canceled. This is a best-effort,
// fire-and-forget speed layer (D-05/D-12): delivery is NOT guaranteed (a
// disconnected worker misses it), and correctness never depends on this
// call succeeding -- the guarded-transition allow-list already committed
// the authoritative canceled status before Publish is ever invoked. The
// signature satisfies internal/api's CancelPublisher interface so Plan 03
// can inject *Publisher directly.
func (p *Publisher) Publish(ctx context.Context, id uuid.UUID) error {
	return p.rdb.Publish(ctx, Channel, id.String()).Err()
}

// Subscriber listens on Channel and, for each received job id, calls
// reg.Cancel(id) -- a no-op if this process is not currently running that
// job.
type Subscriber struct {
	rdb *redis.Client
	reg *Registry
}

// NewSubscriber wraps an existing *redis.Client and the Registry to cancel
// against. The caller owns the client's lifecycle (Close).
func NewSubscriber(rdb *redis.Client, reg *Registry) *Subscriber {
	return &Subscriber{rdb: rdb, reg: reg}
}

// Run subscribes to Channel and cancels the matching registry entry for
// each received job id until ctx is done. An unparseable payload (T-39-05:
// a stray/malformed publish must never crash a worker) is silently
// skipped. Run closes the underlying pub/sub subscription and returns
// promptly on ctx.Done() -- no leaked goroutine or connection (T-39-04).
func (s *Subscriber) Run(ctx context.Context) {
	sub := s.rdb.Subscribe(ctx, Channel)
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			id, err := uuid.Parse(msg.Payload)
			if err != nil {
				continue
			}
			s.reg.Cancel(id)
		}
	}
}
