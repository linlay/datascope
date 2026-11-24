package store

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// OperationError retains cancellation identity while adding the operation that
// failed. No SQL, bind values or credentials are included in its message.
func OperationError(ctx context.Context, stage string, limit time.Duration, err error) error {
	if err == nil {
		return nil
	}
	var contextual *OperationTimeoutError
	if errors.As(err, &contextual) {
		return err
	}
	cause := ctx.Err()
	if cause == nil {
		cause = err
	}
	if errors.Is(cause, context.Canceled) {
		return fmt.Errorf("%s已取消: %w", stage, context.Canceled)
	}
	var network net.Error
	if errors.Is(cause, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		return &OperationTimeoutError{Stage: stage, Limit: limit, Cause: context.DeadlineExceeded}
	}
	return err
}

type OperationTimeoutError struct {
	Stage string
	Limit time.Duration
	Cause error
}

func (e *OperationTimeoutError) Error() string {
	return fmt.Sprintf("%s超时（配置上限 %s；请求或客户端时限可能更短），请检查该阶段耗时或调整数据库超时设置", e.Stage, e.Limit)
}
func (e *OperationTimeoutError) Unwrap() error { return e.Cause }

func connectLimit(values []time.Duration) time.Duration {
	if len(values) > 0 && values[0] > 0 {
		return values[0]
	}
	return DefaultConnectTimeout
}
