package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
)

func newAPIResourcesCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "api-resources",
		Short: "List the object kinds vsfleet knows, with their short names",
		Long: strings.TrimSpace(`
List every object kind vsfleet accepts, the way "kubectl api-resources" does.

NAME is the plural a listing asks for; SHORTNAMES are abbreviations accepted
anywhere a kind is, including --kind flags and topology arguments. SOURCE says
whether the kind is read from a live vCenter, recorded in stored captures, or
both; "history" marks a kind the history database keeps as an object of its
own, such as a capture. Never contacts a vCenter.`),
		Example: `  # Every kind and its short names
  vsfleet api-resources

  # As JSON, for a script
  vsfleet api-resources -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			all := apiresource.All()
			if a.json() {
				return writeJSON(a.out(), all)
			}
			t := newTable(a.out(), "NAME", "SHORTNAMES", "SOURCE", "DESCRIPTION")
			for _, k := range all {
				t.row(k.Plural, dash(strings.Join(k.ShortNames, ",")), kindSource(k), k.Description)
			}
			t.flush()
			return nil
		},
	}
}

func kindSource(k apiresource.Kind) string {
	var sources []string
	if k.Live {
		sources = append(sources, "live")
	}
	if k.Stored {
		sources = append(sources, "stored")
	}
	if k.History {
		sources = append(sources, "history")
	}
	return strings.Join(sources, ",")
}
