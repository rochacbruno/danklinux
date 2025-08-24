package errdefs

import "fmt"

type ErrorType int

const (
	ErrTypeNotLinux ErrorType = iota
	ErrTypeInvalidArchitecture
	ErrTypeUnsupportedDistribution
	ErrTypeUnsupportedVersion
	ErrTypeGeneric
)

type CustomError struct {
	Type    ErrorType
	Message string
}

func (e *CustomError) Error() string {
	return e.Message
}

func NewCustomError(errType ErrorType, message string) error {
	return &CustomError{
		Type:    errType,
		Message: message,
	}
}

func NewGenericError(message string, args ...interface{}) error {
	return NewCustomError(ErrTypeGeneric, fmt.Sprintf(message, args...))
}