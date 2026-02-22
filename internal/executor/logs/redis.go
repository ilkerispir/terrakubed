package logs

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStreamer struct {
	client *redis.Client
	jobId  string
	stepId string
}

func NewRedisStreamer(addr, password, jobId, stepId string) *RedisStreamer {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password, // no password set
		DB:       0,        // use default DB
	})

	return &RedisStreamer{
		client: rdb,
		jobId:  jobId,
		stepId: stepId,
	}
}

func (r *RedisStreamer) Write(p []byte) (n int, err error) {
	// Write to Stdout as well for debugging
	os.Stdout.Write(p)

	ctx := context.Background()
	values := map[string]interface{}{
		"jobId":  r.jobId,
		"stepId": r.stepId,
		"output": string(p),
		"time":   time.Now().UnixMilli(),
	}

	err = r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: r.jobId,
		Values: values,
	}).Err()

	if err != nil {
		fmt.Printf("Failed to write to redis: %v\n", err)
		return len(p), nil // Don't fail the execution if logs fail
	}

	return len(p), nil
}

func (r *RedisStreamer) Close() error {
	return r.client.Close()
}
