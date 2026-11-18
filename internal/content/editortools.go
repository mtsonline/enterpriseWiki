package content

import (
	"slices"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// EditorTools are the buttons of the formatting toolbar of the text editors
// (visual and simple editor share these names). The site
// (config ui.editor_tools) and categories (inherited downwards) choose which
// of them the toolbar shows; hiding a button does not forbid the formatting
// (shortcuts and Markdown keep working). The switch between visual editing
// and Markdown is always shown.
var EditorTools = []string{
	"block", "strong", "em", "underline", "strike", "code", "link",
	"bullet", "ordered", "task", "quote",
	"table", "table_edit", "image", "hr", "macro",
	"undo", "redo",
}

// AllEditorTools stands for all buttons (also those added later).
const AllEditorTools = "*"

// CheckEditorTools validates a choice of buttons: "*" alone, or known
// names without repetition.
func CheckEditorTools(list []string) error {
	if len(list) == 1 && list[0] == AllEditorTools {
		return nil
	}
	if len(list) > len(EditorTools) {
		return apperr.BadRequest("editor_tools: at most %d buttons", len(EditorTools))
	}
	seen := map[string]bool{}
	for _, t := range list {
		if !slices.Contains(EditorTools, t) {
			return apperr.BadRequest("editor_tools: unknown button %q (known: %v, or \"*\" for all)", t, EditorTools)
		}
		if seen[t] {
			return apperr.BadRequest("editor_tools: %q twice", t)
		}
		seen[t] = true
	}
	return nil
}
