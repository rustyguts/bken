package queue

import (
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// dragonflyOpt implements asynq.RedisConnOpt and forces go-redis to send
// EVAL instead of EVALSHA. DragonflyDB doesn't persist Lua script caches
// across snapshots/restarts the same way Redis does, so EVALSHA fails with
// -NOSCRIPT. NoScriptCache makes go-redis fall back transparently.
type dragonflyOpt struct {
	Addr string
}

func (o dragonflyOpt) MakeRedisClient() interface{} {
	return redis.NewClient(&redis.Options{
		Addr: o.Addr,
	})
}

// NewClient builds an asynq client against redis (or DragonflyDB).
func NewClient(redisAddr string) *asynq.Client {
	return asynq.NewClient(dragonflyOpt{Addr: redisAddr})
}

// NewInspector is used for listing queued/active tasks in the UI.
func NewInspector(redisAddr string) *asynq.Inspector {
	return asynq.NewInspector(dragonflyOpt{Addr: redisAddr})
}
