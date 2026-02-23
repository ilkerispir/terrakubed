package logs

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStreamer writes log lines to a Redis Stream so the API can serve them
// in real-time via the /tfoutput/v1/... endpoint.
// Matches the Java LogsServiceRedis + LogsConsumer pattern.
type RedisStreamer struct {
	client     *redis.Client
	jobId      string
	stepId     string
	lineNumber atomic.Int32
	buf        strings.Builder
}

func NewRedisStreamer(addr, password, jobId, stepId string) (*RedisStreamer, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})

	// Verify connection
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis at %s: %w", addr, err)
	}

	rs := &RedisStreamer{
		client: rdb,
		jobId:  jobId,
		stepId: stepId,
	}

	// Setup consumer groups (matching Java setupConsumerGroups)
	ctx := context.Background()
	_ = rdb.XGroupCreateMkStream(ctx, jobId, "CLI", "0").Err()
	_ = rdb.XGroupCreateMkStream(ctx, jobId, "UI", "0").Err()

	return rs, nil
}

func (r *RedisStreamer) Write(p []byte) (n int, err error) {
	// Write to stdout for pod logs
	os.Stdout.Write(p)

	text := string(p)
	r.buf.WriteString(text)

	// Split by newlines and send each complete line to Redis
	for {
		content := r.buf.String()
		idx := strings.IndexByte(content, '\n')
		if idx == -1 {
			break
		}
		line := content[:idx]
		remaining := content[idx+1:]
		r.buf.Reset()
		r.buf.WriteString(remaining)

		lineNum := r.lineNumber.Add(1)

		// XADD to Redis Stream (matching Java LogsServiceRedis.sendLogs)
		err := r.client.XAdd(context.Background(), &redis.XAddArgs{
			Stream: r.jobId,
			Values: map[string]interface{}{
				"jobId":      r.jobId,
				"stepId":     r.stepId,
				"lineNumber": fmt.Sprintf("%d", lineNum),
				"output":     line,
			},
		}).Err()
		if err != nil {
			log.Printf("Warning: failed to send log line to Redis: %v", err)
		} else if lineNum == 1 {
			log.Printf("First log line sent to Redis stream (jobId=%s)", r.jobId)
		}
	}

	return len(p), nil
}

func (r *RedisStreamer) Close() error {
	ctx := context.Background()

	// Flush any remaining content in buffer
	if r.buf.Len() > 0 {
		lineNum := r.lineNumber.Add(1)
		_ = r.client.XAdd(ctx, &redis.XAddArgs{
			Stream: r.jobId,
			Values: map[string]interface{}{
				"jobId":      r.jobId,
				"stepId":     r.stepId,
				"lineNumber": fmt.Sprintf("%d", lineNum),
				"output":     r.buf.String(),
			},
		}).Err()
		r.buf.Reset()
	}

	// Send a sentinel message so consumers know the stream is complete
	_ = r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: r.jobId,
		Values: map[string]interface{}{
			"jobId":  r.jobId,
			"stepId": r.stepId,
			"done":   "true",
		},
	}).Err()

	// Set TTL on the stream instead of deleting immediately,
	// so the UI has time to read remaining logs
	r.client.Expire(ctx, r.jobId, 5*time.Minute)

	return r.client.Close()
}
