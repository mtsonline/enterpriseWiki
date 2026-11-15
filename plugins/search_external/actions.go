package search_external

import (
	_ "embed"

	"github.com/mtsonline/enterprisewiki/internal/actions"
)

//go:embed actions.yaml
var actionsYAML []byte

// The plugin's entries of the action register.
func init() { actions.Add("search.external", actionsYAML) }
