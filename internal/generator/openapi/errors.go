package openapi

import "errors"

var (
	ErrInvalidProject       = errors.New("openapi: invalid JGO project")
	ErrInvalidOperation     = errors.New("openapi: invalid operation")
	ErrInvalidMethod        = errors.New("openapi: method must be GET or POST")
	ErrInvalidPath          = errors.New("openapi: invalid HTTP path")
	ErrInvalidField         = errors.New("openapi: invalid request field")
	ErrDuplicateOperation   = errors.New("openapi: operation already exists")
	ErrDuplicateRoute       = errors.New("openapi: route already exists")
	ErrModelNotFound        = errors.New("openapi: Go model was not found")
	ErrUnsupportedModelType = errors.New("openapi: unsupported Go model field type")
	ErrServiceFileExists    = errors.New("openapi: service implementation already exists")
)
