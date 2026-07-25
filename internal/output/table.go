package output

import (
	"fmt"
	"slices"
	"strings"
)

func renderTable[T any, F ~string](items []T, fields []F, cellFn func(T, F) string) string {
	var rendered strings.Builder
	headers := make([]string, len(fields))
	for index, field := range fields {
		headers[index] = string(field)
	}
	rendered.WriteString(strings.Join(headers, " | "))
	rendered.WriteByte('\n')
	for _, item := range items {
		cells := make([]string, len(fields))
		for index, field := range fields {
			cells[index] = cellFn(item, field)
		}
		rendered.WriteString(strings.Join(cells, " | "))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

func renderIDs[T any](items []T, id func(T) string) string {
	var rendered strings.Builder
	for _, item := range items {
		rendered.WriteString(cell(id(item)))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

func selectFields[F ~string](csv string, defaults, allowed []F, noun string) ([]F, error) {
	if strings.TrimSpace(csv) == "" {
		return defaults, nil
	}
	fields := make([]F, 0, len(defaults))
	seen := map[F]bool{}
	for _, raw := range strings.Split(csv, ",") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		field := F(strings.ToUpper(trimmed))
		if !slices.Contains(allowed, field) {
			return nil, fmt.Errorf("unknown %s field %q; valid fields: %s", noun, trimmed, fieldNames(allowed))
		}
		if !seen[field] {
			fields = append(fields, field)
			seen[field] = true
		}
	}
	if len(fields) == 0 {
		return defaults, nil
	}
	return fields, nil
}

func fieldNames[F ~string](fields []F) string {
	values := make([]string, len(fields))
	for index, field := range fields {
		values[index] = string(field)
	}
	return strings.Join(values, ", ")
}
