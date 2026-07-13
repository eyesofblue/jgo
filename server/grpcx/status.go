package grpcx

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	jerrors "github.com/eyesofblue/jgo/errors"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StatusError converts an application error into a gRPC status error. Existing
// gRPC status errors are preserved. JGO business codes are attached through a
// standard ErrorInfo detail with domain "jgo".
func StatusError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, context.Canceled.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, context.DeadlineExceeded.Error())
	}

	businessCode, message, httpStatus := jerrors.PublicValues(err)
	grpcStatus := status.New(codeFromHTTPStatus(httpStatus), message)
	withDetails, detailErr := grpcStatus.WithDetails(&errdetails.ErrorInfo{
		Reason: strconv.Itoa(businessCode),
		Domain: "jgo",
	})
	if detailErr != nil {
		return grpcStatus.Err()
	}
	return withDetails.Err()
}

func codeFromHTTPStatus(httpStatus int) codes.Code {
	switch httpStatus {
	case http.StatusBadRequest:
		return codes.InvalidArgument
	case http.StatusUnauthorized:
		return codes.Unauthenticated
	case http.StatusForbidden:
		return codes.PermissionDenied
	case http.StatusNotFound:
		return codes.NotFound
	case http.StatusConflict:
		return codes.Aborted
	case http.StatusTooManyRequests:
		return codes.ResourceExhausted
	case http.StatusNotImplemented:
		return codes.Unimplemented
	case http.StatusServiceUnavailable:
		return codes.Unavailable
	case http.StatusGatewayTimeout:
		return codes.DeadlineExceeded
	default:
		return codes.Internal
	}
}
