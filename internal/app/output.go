package app

import "github.com/spf13/cobra"

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

// PlaceholderHandler returns a handler that writes a success placeholder for a command group.
func PlaceholderHandler(label string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		resp := NewSuccessResponse(label, map[string]interface{}{
			"note": label + " handler ready — full implementation requires complete CASC context",
		})
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}

// Ensure imports
var _ = (*cobra.Command)(nil)
