package service

import (
	"context"
	"time"

	demoprotov1 "example.com/demo-proto/gen/pb/demo_proto/v1"
	"github.com/eyesofblue/jgo/logx"
	"go.opentelemetry.io/otel/trace"
)

func (s *Service) DemoProtoServiceGetUser(ctx context.Context, request *demoprotov1.GetUserRequest) (*demoprotov1.GetUserResponse, error) {
	if request.GetUid() == 999 {
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	traceID := trace.SpanContextFromContext(ctx).TraceID().String()
	logx.InfoCtx(ctx, "e2e grpc get user", "uid", request.GetUid())
	return &demoprotov1.GetUserResponse{Code: 0, Msg: "", TraceId: traceID}, nil
}
