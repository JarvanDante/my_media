package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/JarvanDante/my_media/internal/shared/transcode"
)

// Bus Kafka：投递转码 Job、消费 Result。
type Bus struct {
	enabled      bool
	brokers      []string
	topicJobs    string
	topicResults string
	group        string
	writer       *kafkago.Writer
}

func NewBus(ctx context.Context) *Bus {
	enabled := g.Cfg().MustGet(ctx, "kafka.enabled", true).Bool()
	brokers := g.Cfg().MustGet(ctx, "kafka.brokers").Strings()
	if len(brokers) == 0 {
		brokers = []string{"127.0.0.1:9092"}
	}
	jobs := g.Cfg().MustGet(ctx, "kafka.topic_jobs", transcode.TopicJobs).String()
	results := g.Cfg().MustGet(ctx, "kafka.topic_results", transcode.TopicResults).String()
	group := g.Cfg().MustGet(ctx, "kafka.group", "my_media").String()

	b := &Bus{
		enabled:      enabled,
		brokers:      brokers,
		topicJobs:    jobs,
		topicResults: results,
		group:        group,
	}
	if !enabled {
		return b
	}
	b.writer = &kafkago.Writer{
		Addr:                   kafkago.TCP(brokers...),
		Topic:                  jobs,
		Balancer:               &kafkago.Hash{},
		RequiredAcks:           kafkago.RequireOne,
		Async:                  false,
		AllowAutoTopicCreation: true,
	}
	return b
}

func (b *Bus) Enabled() bool { return b.enabled }

func (b *Bus) Close() error {
	if b.writer != nil {
		return b.writer.Close()
	}
	return nil
}

// PublishJob 投递转码任务。
func (b *Bus) PublishJob(ctx context.Context, msg transcode.JobMessage) error {
	if !b.enabled {
		return fmt.Errorf("kafka disabled")
	}
	if b.writer == nil {
		return fmt.Errorf("kafka writer not initialized")
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return b.writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte(msg.JobID),
		Value: body,
		Time:  time.Now(),
	})
}

// ConsumeResults 阻塞消费转码结果。
func (b *Bus) ConsumeResults(ctx context.Context, handler func(context.Context, transcode.ResultMessage) error) error {
	if !b.enabled {
		g.Log().Warning(ctx, "kafka disabled, skip result consumer")
		<-ctx.Done()
		return ctx.Err()
	}
	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        b.brokers,
		GroupID:        b.group,
		Topic:          b.topicResults,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		StartOffset:    kafkago.FirstOffset,
	})
	defer r.Close()

	g.Log().Infof(ctx, "kafka: consuming results topic=%s group=%s brokers=%s",
		b.topicResults, b.group, strings.Join(b.brokers, ","))

	for {
		m, err := r.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("kafka fetch: %w", err)
		}
		var res transcode.ResultMessage
		if err := json.Unmarshal(m.Value, &res); err != nil {
			g.Log().Warningf(ctx, "kafka: invalid result json: %v; skip", err)
			_ = r.CommitMessages(ctx, m)
			continue
		}
		if err := handler(ctx, res); err != nil {
			g.Log().Warningf(ctx, "kafka: result handle failed job_id=%s: %v", res.JobID, err)
		}
		if err := r.CommitMessages(ctx, m); err != nil {
			g.Log().Warningf(ctx, "kafka: commit failed: %v", err)
		}
	}
}
