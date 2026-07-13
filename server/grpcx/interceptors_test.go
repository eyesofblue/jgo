package grpcx

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	jerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/middleware/requestid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryRequestID(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(RequestIDMetadataKey, "client-123"))
	var responseHeader metadata.MD
	ctx = grpc.NewContextWithServerTransportStream(ctx, &headerTransportStream{header: &responseHeader})

	_, err := UnaryRequestID(nil)(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Echo"},
		func(handlerCtx context.Context, _ any) (any, error) {
			if got := requestid.FromContext(handlerCtx); got != "client-123" {
				t.Fatalf("request ID = %q", got)
			}
			return nil, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := responseHeader.Get(RequestIDMetadataKey); len(got) != 1 || got[0] != "client-123" {
		t.Fatalf("response request ID = %v", got)
	}
}

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
	stream := &testServerStream{
		ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs(RequestIDMetadataKey, "stream-7")),
	}

	requestIDInterceptor := StreamRequestID(nil)
	errorInterceptor := StreamErrorMapper()
	recoveryInterceptor := StreamRecovery(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	err := requestIDInterceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/test.Service/Watch"},
		func(srv any, requestIDStream grpc.ServerStream) error {
			if got := requestid.FromContext(requestIDStream.Context()); got != "stream-7" {
				t.Fatalf("request ID = %q", got)
			}
			return errorInterceptor(srv, requestIDStream, &grpc.StreamServerInfo{FullMethod: "/test.Service/Watch"},
				func(srv any, mappedStream grpc.ServerStream) error {
					return recoveryInterceptor(srv, mappedStream, &grpc.StreamServerInfo{FullMethod: "/test.Service/Watch"},
						func(any, grpc.ServerStream) error {
							return jerrors.New(100400, "bad stream", jerrors.WithHTTPStatus(http.StatusBadRequest))
						})
				})
		})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("stream error = %v", err)
	}
	if got := stream.header.Get(RequestIDMetadataKey); len(got) != 1 || got[0] != "stream-7" {
		t.Fatalf("stream response header = %v", got)
	}
}

type headerTransportStream struct {
	header *metadata.MD
}

func (s *headerTransportStream) Method() string { return "/test.Service/Echo" }
func (s *headerTransportStream) SetHeader(md metadata.MD) error {
	*s.header = metadata.Join(*s.header, md)
	return nil
}
func (s *headerTransportStream) SendHeader(md metadata.MD) error { return s.SetHeader(md) }
func (s *headerTransportStream) SetTrailer(metadata.MD) error    { return nil }

type testServerStream struct {
	ctx     context.Context
	header  metadata.MD
	trailer metadata.MD
}

func (s *testServerStream) SetHeader(md metadata.MD) error {
	s.header = metadata.Join(s.header, md)
	return nil
}
func (s *testServerStream) SendHeader(md metadata.MD) error { return s.SetHeader(md) }
func (s *testServerStream) SetTrailer(md metadata.MD)       { s.trailer = metadata.Join(s.trailer, md) }
func (s *testServerStream) Context() context.Context        { return s.ctx }
func (s *testServerStream) SendMsg(any) error               { return nil }
func (s *testServerStream) RecvMsg(any) error               { return nil }
