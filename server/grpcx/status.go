package grpcx

import (
	"context"
	"errors"
	"net/http"

	jerrors "github.com/eyesofblue/jgo/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StatusError converts errors that escaped the generated transport into gRPC
// status errors. Generated unary transports convert explicit JGO business
// errors into response code/msg before this mapper runs.
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

	_, message, httpStatus := jerrors.PublicValues(err)
	return status.Error(codeFromHTTPStatus(httpStatus), message)
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
