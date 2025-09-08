package treesitter

import "fmt"

// Error represents a TreeSitter-related error
type Error struct {
	Type    ErrorType
	Message string
	Cause   error
}

// ErrorType represents the type of error
type ErrorType string

const (
	ErrorTypeParse      ErrorType = "parse"
	ErrorTypeLanguage   ErrorType = "language"
	ErrorTypeSymbol     ErrorType = "symbol"
	ErrorTypeRegistry   ErrorType = "registry"
	ErrorTypeValidation ErrorType = "validation"
)

// Error implements the error interface
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// Unwrap returns the underlying error
func (e *Error) Unwrap() error {
	return e.Cause
}

// NewParseError creates a new parse error
func NewParseError(message string, cause error) *Error {
	return &Error{
		Type:    ErrorTypeParse,
		Message: message,
		Cause:   cause,
	}
}

// NewLanguageError creates a new language error
func NewLanguageError(message string, cause error) *Error {
	return &Error{
		Type:    ErrorTypeLanguage,
		Message: message,
		Cause:   cause,
	}
}

// NewSymbolError creates a new symbol extraction error
func NewSymbolError(message string, cause error) *Error {
	return &Error{
		Type:    ErrorTypeSymbol,
		Message: message,
		Cause:   cause,
	}
}

// NewRegistryError creates a new registry error
func NewRegistryError(message string, cause error) *Error {
	return &Error{
		Type:    ErrorTypeRegistry,
		Message: message,
		Cause:   cause,
	}
}

// NewValidationError creates a new validation error
func NewValidationError(message string) *Error {
	return &Error{
		Type:    ErrorTypeValidation,
		Message: message,
	}
}
