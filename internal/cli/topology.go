package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/topology"
)

type topologyDirection string

const (
	directionTopology     topologyDirection = "topology"
	directionDependencies topologyDirection = "dependencies"
	directionBlastRadius  topologyDirection = "blast-radius"
)

func newTopologyCommand(a *App) *cobra.Command {
	return newTopologyQueryCommand(a, directionTopology, "topology KIND NAME [RUN]", nil, "Show a subject's containment and attachments")
}

func newDependenciesCommand(a *App) *cobra.Command {
	var depth int
	cmd := newTopologyQueryCommand(a, directionDependencies, "dependencies KIND NAME [RUN]", []string{"deps"}, "Show what a subject depends on")
	cmd.Flags().IntVar(&depth, "depth", 1, "dependency traversal depth (1-5)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runTopologyQuery(cmd, a, args, directionDependencies, depth)
	}
	return cmd
}

func newBlastRadiusCommand(a *App) *cobra.Command {
	var depth int
	cmd := newTopologyQueryCommand(a, directionBlastRadius, "blast-radius KIND NAME [RUN]", []string{"blast"}, "Show the objects affected by a subject")
	cmd.Flags().IntVar(&depth, "depth", 1, "blast-radius traversal depth (1-5)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runTopologyQuery(cmd, a, args, directionBlastRadius, depth)
	}
	return cmd
}

func newTopologyQueryCommand(a *App, direction topologyDirection, use string, aliases []string, short string) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   short,
		Args:    cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTopologyQuery(cmd, a, args, direction, 1)
		},
	}
}

func runTopologyQuery(cmd *cobra.Command, a *App, args []string, direction topologyDirection, depth int) error {
	kind, err := topology.ParseKind(args[0])
	if err != nil {
		return err
	}
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	data, err := loadRunExportData(cmd, a, args[2:])
	if err != nil {
		return err
	}
	graph := topology.Build(data)
	subjects := graph.Resolve(kind, args[1], a.ContextNames)
	result := topology.Result{
		SchemaVersion: 1,
		RunID:         data.Run.ID,
		Query:         args[1],
		Kind:          string(kind),
		Direction:     string(direction),
		Ambiguous:     len(subjects) > 1,
		Subjects:      make([]topology.SubjectResult, 0, len(subjects)),
	}
	for _, subject := range subjects {
		var subjectResult topology.SubjectResult
		switch direction {
		case directionTopology:
			subjectResult = graph.Topology(subject)
		case directionDependencies:
			subjectResult = graph.Dependencies(subject, depth)
		case directionBlastRadius:
			subjectResult = graph.BlastRadius(subject, depth)
		}
		result.Subjects = append(result.Subjects, subjectResult)
	}
	if len(result.Subjects) == 0 {
		// Keep a machine-readable unknown result rather than turning an absent
		// object into a successful empty answer.
		result.Subjects = append(result.Subjects, graph.Unknown(kind, args[1], a.ContextNames))
	}
	result.CheckedContexts, result.Blind = topologyResultCoverage(result.Subjects)
	if len(result.CheckedContexts) == 0 {
		if len(a.ContextNames) > 0 {
			result.CheckedContexts = append([]string(nil), a.ContextNames...)
		} else {
			for _, contextRun := range data.Contexts {
				result.CheckedContexts = append(result.CheckedContexts, contextRun.Name)
			}
		}
		result.CheckedContexts = uniqueSortedStrings(result.CheckedContexts)
	}
	if a.json() {
		return writeJSON(a.out(), result)
	}
	return printTopologyResult(a.out(), result, direction)
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			seen[value] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if strings.ToLower(out[j]) < strings.ToLower(out[i]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func topologyResultCoverage(subjects []topology.SubjectResult) ([]string, []topology.Blindness) {
	contexts := make(map[string]bool)
	blind := make(map[string]topology.Blindness)
	for _, subject := range subjects {
		for _, member := range subject.Subject.Members {
			if member.Context != "" {
				contexts[member.Context] = true
			}
		}
		for _, edge := range append(append([]topology.Edge(nil), subject.Ancestors...), subject.Edges...) {
			if edge.From.Context != "" {
				contexts[edge.From.Context] = true
			}
			if edge.To.Context != "" {
				contexts[edge.To.Context] = true
			}
		}
		for _, item := range subject.Blind {
			blind[item.Context+"\x00"+item.Reason] = item
		}
	}
	checked := make([]string, 0, len(contexts))
	for context := range contexts {
		checked = append(checked, context)
	}
	// Keep the final JSON and table order independent of map iteration.
	for i := 0; i < len(checked); i++ {
		for j := i + 1; j < len(checked); j++ {
			if strings.ToLower(checked[j]) < strings.ToLower(checked[i]) {
				checked[i], checked[j] = checked[j], checked[i]
			}
		}
	}
	blindness := make([]topology.Blindness, 0, len(blind))
	for _, item := range blind {
		blindness = append(blindness, item)
	}
	for i := 0; i < len(blindness); i++ {
		for j := i + 1; j < len(blindness); j++ {
			if blindness[j].Context < blindness[i].Context || (blindness[j].Context == blindness[i].Context && blindness[j].Reason < blindness[i].Reason) {
				blindness[i], blindness[j] = blindness[j], blindness[i]
			}
		}
	}
	return checked, blindness
}

func printTopologyResult(out io.Writer, result topology.Result, direction topologyDirection) error {
	if result.Ambiguous {
		fmt.Fprintf(out, "[AMBIGUOUS] %d distinct %ss are named %q\n", len(result.Subjects), result.Kind, result.Query)
		fmt.Fprintln(out, "Use --context to narrow the search.")
		fmt.Fprintln(out)
	}
	for i, subject := range result.Subjects {
		printTopologySubject(out, subject, direction)
		if i+1 < len(result.Subjects) {
			fmt.Fprintln(out)
		}
	}
	return nil
}

func printTopologySubject(out io.Writer, result topology.SubjectResult, direction topologyDirection) {
	label := strings.ToUpper(string(result.Confidence))
	fmt.Fprintf(out, "[%s] %s %s\n", label, result.Subject.Kind, result.Subject.Name)
	fields := newFields(out)
	if len(result.Subject.Identity) > 0 {
		fields.add("identity", strings.Join(result.Subject.Identity, ", "))
	}
	observed := make([]string, 0, len(result.Subject.Members))
	for _, member := range result.Subject.Members {
		observed = append(observed, member.Context)
	}
	fields.add("observed in", strings.Join(observed, ", "))
	checked := make([]string, 0)
	for _, member := range result.Subject.Members {
		checked = appendTopologyContext(checked, member.Context)
	}
	for _, edge := range append(append([]topology.Edge(nil), result.Ancestors...), result.Edges...) {
		checked = appendTopologyContext(checked, edge.From.Context)
		checked = appendTopologyContext(checked, edge.To.Context)
	}
	fields.add("checked contexts", strings.Join(checked, ", "))
	if len(result.Blind) > 0 {
		messages := make([]string, 0, len(result.Blind))
		for _, blind := range result.Blind {
			messages = append(messages, blind.Context+" — "+blind.Reason)
		}
		fields.add("could not verify", strings.Join(messages, "; "))
	}
	fields.flush()
	if len(result.Ancestors) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  ANCESTOR\tRELATION\tCONFIDENCE\tBASIS")
		t := newTable(out)
		for _, edge := range result.Ancestors {
			t.row(objectLabel(edge.From), string(edge.Relation), string(edge.Confidence), string(edge.Basis))
		}
		t.flush()
	}
	if len(result.Edges) > 0 {
		fmt.Fprintln(out)
		header := "RELATED"
		if direction == directionDependencies {
			header = "DEPENDENCY"
		}
		if direction == directionBlastRadius {
			header = "DEPENDENT"
		}
		fmt.Fprintf(out, "  %s\tRELATION\tCONFIDENCE\tBASIS\n", header)
		t := newTable(out)
		for _, edge := range result.Edges {
			other := edge.To
			if direction == directionBlastRadius || (direction == directionTopology && edge.To.Kind == result.Subject.Kind && edge.To.Name == result.Subject.Name) {
				other = edge.From
			}
			t.row(objectLabel(other), string(edge.Relation), string(edge.Confidence), string(edge.Basis))
		}
		t.flush()
	}
	if len(result.Unresolved) > 0 {
		fmt.Fprintf(out, "\n  unresolved\t%s\n", strings.Join(result.Unresolved, ", "))
	}
}

func appendTopologyContext(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func objectLabel(node topology.Node) string {
	label := node.Kind + " " + node.Name
	if node.Context != "" {
		label += " @ " + node.Context
	}
	return label
}
