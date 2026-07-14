package grpcx

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"

	jerrors "github.com/eyesofblue/jgo/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStatusErrorMapsEscapedServiceErrorWithoutBusinessDetails(t *testing.T) {
	err := jerrors.New(120404, "user not found", jerrors.WithHTTPStatus(http.StatusNotFound))
	mapped := StatusError(err)
	grpcStatus := status.Convert(mapped)
	if grpcStatus.Code() != codes.NotFound || grpcStatus.Message() != "user not found" {
		t.Fatalf("status = %v %q", grpcStatus.Code(), grpcStatus.Message())
	}
	if details := grpcStatus.Details(); len(details) != 0 {
		t.Fatalf("business details leaked into transport status: %v", details)
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
