package errors

// Option configures a service Error.
type Option func(*Error)

// WithHTTPStatus associates an HTTP status with a service error.
func WithHTTPStatus(status int) Option {
	return func(e *Error) {
		e.httpStatus = status
	}
}
