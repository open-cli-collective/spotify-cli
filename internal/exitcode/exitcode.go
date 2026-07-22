// Package exitcode classifies command failures for the process boundary.
package exitcode

import "errors"

// Success and the remaining constants are stable process exit codes.
const (
	Success  = 0
	Generic  = 1
	Usage    = 2
	Config   = 3
	NotFound = 4
	Upstream = 5
)

type codedError struct {
	code  int
	err   error
	quiet bool
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }

// New attaches a process exit code to err.
func New(code int, err error) error {
	if err == nil {
		return nil
	}
	return &codedError{code: code, err: err}
}

// NewQuiet attaches an exit code to an error already rendered to stdout.
func NewQuiet(code int, err error) error {
	if err == nil {
		return nil
	}
	return &codedError{code: code, err: err, quiet: true}
}

// Code returns the process exit code for err.
func Code(err error) int {
	if err == nil {
		return Success
	}
	var typed *codedError
	if errors.As(err, &typed) {
		return typed.code
	}
	return Generic
}

// Quiet reports whether err should not be rendered again.
func Quiet(err error) bool {
	var typed *codedError
	return errors.As(err, &typed) && typed.quiet
}
