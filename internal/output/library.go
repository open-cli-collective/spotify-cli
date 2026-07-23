package output

import (
	"strconv"
	"strings"
)

// SavedCheck is one user input and its normalized membership result.
type SavedCheck struct {
	Reference string
	ID        string
	Saved     bool
}

// RenderSavedChecks renders saved membership in input order.
func RenderSavedChecks(checks []SavedCheck) string {
	var rendered strings.Builder
	rendered.WriteString("REFERENCE | ID | SAVED\n")
	for _, check := range checks {
		rendered.WriteString(cell(check.Reference))
		rendered.WriteString(" | ")
		rendered.WriteString(cell(check.ID))
		rendered.WriteString(" | ")
		rendered.WriteString(strconv.FormatBool(check.Saved))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}
