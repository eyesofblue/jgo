package project

import "errors"

var (
	ErrInvalidName      = errors.New("project: invalid project name")
	ErrInvalidModule    = errors.New("project: invalid Go module path")
	ErrInvalidType      = errors.New("project: invalid project type")
	ErrInvalidVersion   = errors.New("project: invalid JGO version")
	ErrInvalidGoVersion = errors.New("project: invalid Go version")
	ErrInvalidReplace   = errors.New("project: invalid local JGO replacement")
	ErrInvalidTarget    = errors.New("project: invalid target directory")
	ErrTargetExists     = errors.New("project: target exists and is not a directory")
	ErrTargetNotEmpty   = errors.New("project: target directory is not empty")
	ErrTargetIsSymlink  = errors.New("project: target directory is a symbolic link")
	ErrUnsafeTemplate   = errors.New("project: template contains an unsafe path")
	ErrGeneratedInvalid = errors.New("project: generated project is incomplete")
	ErrTidyFailed       = errors.New("project: go mod tidy failed")
)
