package app

import (
	"errors"
	"io"

	"wowdata/internal/resource"
)

type Response struct {
	OK       bool           `json:"ok"`
	Command  string         `json:"command"`
	Data     any            `json:"data"`
	Warnings []string       `json:"warnings"`
	Error    *ErrorResponse `json:"error,omitempty"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CommandError marks a structured command failure that has already been
// written to stdout. CLI entry points should return a non-zero status without
// printing this error again.
type CommandError struct {
	Code string
}

func (e *CommandError) Error() string {
	return e.Code
}

func IsCommandError(err error) bool {
	var commandErr *CommandError
	return errors.As(err, &commandErr)
}

func NewSuccessResponse(command string, data any) Response {
	return Response{
		OK:       true,
		Command:  command,
		Data:     data,
		Warnings: []string{},
	}
}

func NewErrorResponse(command string, code string, message string) Response {
	return Response{
		OK:       false,
		Command:  command,
		Data:     nil,
		Warnings: []string{},
		Error: &ErrorResponse{
			Code:    code,
			Message: message,
		},
	}
}

type ResponseWriter struct {
	w io.Writer
}

func NewResponseWriter(w io.Writer) *ResponseWriter {
	return &ResponseWriter{w: w}
}

func (rw *ResponseWriter) Success(command string, data any) error {
	return rw.write(NewSuccessResponse(command, data))
}

func (rw *ResponseWriter) Error(command, code, message string) error {
	return rw.write(NewErrorResponse(command, code, message))
}

func (rw *ResponseWriter) write(resp Response) error {
	return writeJSON(rw.w, resp)
}

func writeJSON(w io.Writer, resp Response) error {
	if err := resource.EncodeJSON(w, resp, "", "  "); err != nil {
		return err
	}
	if !resp.OK {
		code := "command_failed"
		if resp.Error != nil && resp.Error.Code != "" {
			code = resp.Error.Code
		}
		return &CommandError{Code: code}
	}
	return nil
}
