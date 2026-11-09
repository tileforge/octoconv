package metrics

import (
	"sync"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
)

var queueDepthDesc = prometheus.NewDesc(
	"octoconv_queue_depth",
	"Number of tasks per asynq queue and state.",
	[]string{"queue", "state"},
	nil,
)

type queueInspector interface {
	GetQueueInfo(queue string) (*asynq.QueueInfo, error)
}

// ImageBacklog is the protocol image pending, active, and retry counts.
type ImageBacklog struct {
	Pending int
	Active  int
	Retry   int
}

// QueueDepthCollector reports queue depth at scrape time.
type QueueDepthCollector struct {
	inspector queueInspector
	queues    []string

	mu      sync.Mutex
	backlog func() (ImageBacklog, error)
}

// NewQueueDepthCollector returns a collector for the given queues.
func NewQueueDepthCollector(inspector *asynq.Inspector, queues ...string) *QueueDepthCollector {
	var src queueInspector
	if inspector != nil {
		src = inspector
	}
	return &QueueDepthCollector{inspector: src, queues: queues}
}

// SetImageBacklog adds protocol image counts onto the image gauges.
func (c *QueueDepthCollector) SetImageBacklog(fn func() (ImageBacklog, error)) {
	c.mu.Lock()
	c.backlog = fn
	c.mu.Unlock()
}

func (c *QueueDepthCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- queueDepthDesc
}

func (c *QueueDepthCollector) Collect(ch chan<- prometheus.Metric) {
	extra, ok := c.imageBacklog()
	for _, q := range c.queues {
		info, err := c.inspector.GetQueueInfo(q)
		if err != nil {
			if q == "image" && ok {
				emitDepth(ch, q, "pending", extra.Pending)
				emitDepth(ch, q, "active", extra.Active)
				emitDepth(ch, q, "retry", extra.Retry)
			}
			continue
		}
		pending := info.Pending
		active := info.Active
		retry := info.Retry
		if q == "image" && ok {
			pending += extra.Pending
			active += extra.Active
			retry += extra.Retry
		}
		emitDepth(ch, q, "pending", pending)
		emitDepth(ch, q, "active", active)
		emitDepth(ch, q, "scheduled", info.Scheduled)
		emitDepth(ch, q, "retry", retry)
		emitDepth(ch, q, "archived", info.Archived)
	}
}

func (c *QueueDepthCollector) imageBacklog() (ImageBacklog, bool) {
	c.mu.Lock()
	fn := c.backlog
	c.mu.Unlock()
	if fn == nil {
		return ImageBacklog{}, false
	}
	counts, err := fn()
	if err != nil {
		return ImageBacklog{}, false
	}
	return counts, true
}

func emitDepth(ch chan<- prometheus.Metric, queue, state string, value int) {
	ch <- prometheus.MustNewConstMetric(queueDepthDesc, prometheus.GaugeValue, float64(value), queue, state)
}
