// Package diag accumulates validation messages and renders them as errors.
package diag

import (
	"fmt"
	"strings"
)

// Collector accumulates messages that are attributed to a file or directory.
type Collector struct {
	errs []string
}

// Addf records a message about path. An empty path records the message alone.
func (c *Collector) Addf(path string, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if path == "" {
		c.errs = append(c.errs, message)
		return
	}
	c.errs = append(c.errs, path+": "+message)
}

// ExpectExactlyOne records a message unless exactly one definition was found.
func (c *Collector) ExpectExactlyOne(path string, count int, format string, args ...any) {
	if count == 1 {
		return
	}
	c.Addf(path, format+": expected exactly one definition, got %d", append(args, count)...)
}

// Err returns the accumulated messages, or nil when none were recorded.
func (c *Collector) Err() error {
	if len(c.errs) == 0 {
		return nil
	}
	return ValidationErrors(c.errs)
}

// ValidationErrors reports one or more extension validation failures.
type ValidationErrors []string

func (e ValidationErrors) Error() string {
	return join("extension validation failed:", e)
}

func join(heading string, items []string) string {
	if len(items) == 1 {
		return items[0]
	}
	var builder strings.Builder
	builder.WriteString(heading)
	for _, item := range items {
		builder.WriteString("\n- ")
		builder.WriteString(item)
	}
	return builder.String()
}

// Append folds err into errs, flattening messages that were already accumulated.
func Append(errs []string, err error) []string {
	switch typed := err.(type) {
	case ValidationErrors:
		return append(errs, typed...)
	default:
		return append(errs, err.Error())
	}
}
