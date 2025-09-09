package errdefs

type ErrorType int

const (
	ErrTypeNotLinux ErrorType = iota
	ErrTypeInvalidArchitecture
	ErrTypeUnsupportedDistribution
	ErrTypeUnsupportedVersion
	ErrTypeUpdateCancelled
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

var ErrUpdateCancelled = NewCustomError(ErrTypeUpdateCancelled, "update cancelled by user")
