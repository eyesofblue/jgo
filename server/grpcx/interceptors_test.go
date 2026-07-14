package grpcx

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	jerrors "github.com/eyesofblue/jgo/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryRecoveryAndErrorMapping(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	recovery := UnaryRecovery(logger)
	mapper := UnaryErrorMapper()

	_, err := mapper(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Panic"},
		func(ctx context.Context, req any) (any, error) {
			return recovery(ctx, req, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Panic"},
				func(context.Context, any) (any, error) { panic("boom") })
		})
	if status.Code(err) != codes.Internal || !strings.Contains(logs.String(), "boom") {
		t.Fatalf("error = %v, logs = %s", err, logs.String())
	}
}

func TestStreamInterceptors(t *testing.T) {
	stream := &testServerStream{ctx: context.Background()}

	errorInterceptor := StreamErrorMapper()
	recoveryInterceptor := StreamRecovery(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	err := errorInterceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/test.Service/Watch"},
		func(srv any, mappedStream grpc.ServerStream) error {
			return recoveryInterceptor(srv, mappedStream, &grpc.StreamServerInfo{FullMethod: "/test.Service/Watch"},
				func(any, grpc.ServerStream) error {
					return jerrors.New(100400, "bad stream", jerrors.WithHTTPStatus(http.StatusBadRequest))
				})
		})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("stream error = %v", err)
	}
}

type testServerStream struct {
	ctx context.Context
}

func (s *testServerStream) SetHeader(metadata.MD) error  { return nil }
func (s *testServerStream) SendHeader(metadata.MD) error { return nil }
func (s *testServerStream) SetTrailer(metadata.MD)       {}
func (s *testServerStream) Context() context.Context     { return s.ctx }
func (s *testServerStream) SendMsg(any) error            { return nil }
func (s *testServerStream) RecvMsg(any) error            { return nil }
