package streaming

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RedisStreamReader reads job logs from Redis streams.
// This is a placeholder implementation — will use go-redis when integrated.
type RedisStreamReader struct {
	pool *pgxpool.Pool
	// redisClient *redis.Client — to be added
}

// NewRedisStreamReader creates a new reader.
func NewRedisStreamReader(pool *pgxpool.Pool) *RedisStreamReader {
	return &RedisStreamReader{pool: pool}
}

// GetCurrentLogs reads logs from Redis stream for a given step.
// Matches the Java StreamingService.getCurrentLogs() logic:
// 1. Look up the step to get the job ID (Redis stream key)
// 2. Read all entries from the Redis stream using StreamOffset.fromStart()
// 3. Extract "output" field from each entry
// 4. Return concatenated output
func (r *RedisStreamReader) GetCurrentLogs(ctx context.Context, stepID string) (string, error) {
	// Get the job ID for this step (used as Redis stream key)
	var jobID int
	err := r.pool.QueryRow(ctx,
		"SELECT job_id FROM step WHERE id = $1", stepID,
	).Scan(&jobID)
	if err != nil {
		return "", fmt.Errorf("step not found: %w", err)
	}

	streamKey := fmt.Sprintf("%d", jobID)
	log.Printf("Reading Redis stream: %s (for step %s)", streamKey, stepID)

	// TODO: Use go-redis to read from stream
	// Example:
	// msgs, err := r.redisClient.XRange(ctx, streamKey, "-", "+").Result()
	// for _, msg := range msgs {
	//     output := msg.Values["output"].(string)
	//     logs.WriteString(output + "\n")
	// }

	_ = streamKey
	return "", nil // No Redis connection yet
}

// AppendLog writes a log entry to the Redis stream.
func (r *RedisStreamReader) AppendLog(ctx context.Context, jobID string, output string) error {
	log.Printf("Append log to Redis stream: %s (%d bytes)", jobID, len(output))

	// TODO: Use go-redis to write to stream
	// r.redisClient.XAdd(ctx, &redis.XAddArgs{
	//     Stream: jobID,
	//     Values: map[string]interface{}{"output": output},
	// })

	return nil
}

// SetupConsumerGroup creates a Redis consumer group for a job stream.
func (r *RedisStreamReader) SetupConsumerGroup(ctx context.Context, jobID string) error {
	log.Printf("Setup consumer group for stream: %s", jobID)

	// TODO: Use go-redis
	// r.redisClient.XGroupCreateMkStream(ctx, jobID, "terrakube-group", "0")

	return nil
}

// StreamLog provides SSE-like streaming of logs for a step.
func (r *RedisStreamReader) StreamLog(ctx context.Context, stepID string, output chan<- string) error {
	var jobID int
	err := r.pool.QueryRow(ctx,
		"SELECT job_id FROM step WHERE id = $1", stepID,
	).Scan(&jobID)
	if err != nil {
		return fmt.Errorf("step not found: %w", err)
	}

	streamKey := fmt.Sprintf("%d", jobID)
	log.Printf("Streaming logs from: %s", streamKey)

	// Poll Redis for new entries
	// TODO: Use go-redis XREAD with BLOCK
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Check if step is still running
			var status string
			err := r.pool.QueryRow(ctx,
				"SELECT status FROM step WHERE id = $1", stepID,
			).Scan(&status)
			if err != nil {
				return err
			}

			if status == "completed" || status == "failed" {
				return nil
			}
		}
	}
}

// GetStepOutput tries Redis first, falls back to storage.
// This matches TerraformOutputController.getFile():
// 1. Try Redis (always, regardless of step status)
// 2. Fall back to storage (S3/Azure/GCP)
func (r *RedisStreamReader) GetStepOutput(ctx context.Context, orgID, jobID, stepID string) ([]byte, error) {
	// Try Redis first
	logs, err := r.GetCurrentLogs(ctx, stepID)
	if err == nil && len(strings.TrimSpace(logs)) > 0 {
		log.Printf("Reading output from Redis stream for step %s", stepID)
		return []byte(logs), nil
	}

	// Fall back to storage
	log.Printf("Reading output from storage for step %s", stepID)
	// TODO: Read from storage backend
	return nil, nil
}
