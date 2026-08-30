package app

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrorValidation ErrorKind = "validation"
	ErrorNotFound   ErrorKind = "not_found"
	ErrorProcessing ErrorKind = "processing"
	ErrorCapacity   ErrorKind = "capacity"
	ErrorInternal   ErrorKind = "internal"
)

type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (err *Error) Error() string {
	if err.Message != "" {
		return err.Message
	}
	if err.Err != nil {
		return err.Err.Error()
	}
	return string(err.Kind)
}

func (err *Error) Unwrap() error {
	return err.Err
}

func failure(kind ErrorKind, message string, err error) error {
	return &Error{Kind: kind, Message: message, Err: err}
}

func ErrorKindOf(err error) ErrorKind {
	var appError *Error
	if errors.As(err, &appError) {
		return appError.Kind
	}
	return ErrorInternal
}

func internalFailure(operation string, err error) error {
	return failure(ErrorInternal, fmt.Sprintf("%s failed", operation), err)
}
