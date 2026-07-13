package grpcx

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"

	jerrors "github.com/eyesofblue/jgo/errors"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStatusErrorMapsServiceErrorAndBusinessCode(t *testing.T) {
	err := jerrors.New(120404, "user not found", jerrors.WithHTTPStatus(http.StatusNotFound))
	mapped := StatusError(err)
	grpcStatus := status.Convert(mapped)
	if grpcStatus.Code() != codes.NotFound || grpcStatus.Message() != "user not found" {
		t.Fatalf("status = %v %q", grpcStatus.Code(), grpcStatus.Message())
	}
	details := grpcStatus.Details()
	if len(details) != 1 {
		t.Fatalf("details = %v", details)
	}
	info, ok := details[0].(*errdetails.ErrorInfo)
	if !ok || info.Reason != "120404" || info.Domain != "jgo" {
		t.Fatalf("ErrorInfo = %#v", details[0])
	}
}

func TestStatusErrorDoesNotExposeUnknownError(t *testing.T) {
	mapped := StatusError(stderrors.New("database password"))
	grpcStatus := status.Convert(mapped)
	if grpcStatus.Code() != codes.Internal || grpcStatus.Message() != jerrors.MessageInternal {
		t.Fatalf("status = %v %q", grpcStatus.Code(), grpcStatus.Message())
	}
}

func TestStatusErrorPreservesGRPCAndContextErrors(t *testing.T) {
	original := status.Error(codes.AlreadyExists, "exists")
	if mapped := StatusError(original); mapped != original {
		t.Fatalf("existing status error was not preserved: %v", mapped)
	}
	if code := status.Code(StatusError(context.Canceled)); code != codes.Canceled {
		t.Fatalf("canceled code = %v", code)
	}
	if code := status.Code(StatusError(context.DeadlineExceeded)); code != codes.DeadlineExceeded {
		t.Fatalf("deadline code = %v", code)
	}
}
