package topology

import (
	"sort"
	"strings"
)

// Resolve finds every distinct subject whose observed name or managed-object
// ID matches query. A context filter is applied to members before ambiguity is
// decided; a name collision is therefore data for the caller, not an error.
func (g Graph) Resolve(kind Kind, query string, contexts []string) []Subject {
	query = strings.TrimSpace(query)
	allowed := make(map[string]bool, len(contexts))
	for _, context := range contexts {
		allowed[strings.ToLower(strings.TrimSpace(context))] = true
	}
	var out []Subject
	for _, subject := range g.subjects {
		if !strings.EqualFold(subject.subject.Kind, string(kind)) || !subjectMatches(subject.subject, query) {
			continue
		}
		if len(allowed) > 0 && !subjectInContexts(subject.subject, allowed) {
			continue
		}
		out = append(out, cloneSubject(subject.subject))
	}
	sort.SliceStable(out, func(i, j int) bool { return subjectSortKey(out[i]) < subjectSortKey(out[j]) })
	return out
}

// Unknown creates an explicit unknown result for a subject that was not found
// in the graph. The blindness list is calculated only for the requested
// context scope, so an absent object never falls back to a same-named subject
// from another vCenter.
func (g Graph) Unknown(kind Kind, name string, contexts []string) SubjectResult {
	checked := append([]string(nil), contexts...)
	if len(checked) == 0 {
		checked = append(checked, g.contexts...)
	}
	checked = sortedContexts(checked)
	return SubjectResult{
		Subject:    Subject{Kind: string(kind), Name: name, Basis: BasisName},
		Confidence: ConfidenceUnknown,
		Blind:      g.blindness(checked, kind),
	}
}

func subjectMatches(subject Subject, query string) bool {
	if strings.EqualFold(subject.Name, query) {
		return true
	}
	for _, identity := range subject.Identity {
		if strings.EqualFold(identity, query) {
			return true
		}
		if colon := strings.IndexByte(identity, ':'); colon >= 0 && strings.EqualFold(identity[colon+1:], query) {
			return true
		}
	}
	for _, member := range subject.Members {
		if strings.EqualFold(member.Name, query) || (member.ID != "" && strings.EqualFold(member.ID, query)) {
			return true
		}
	}
	return false
}

func subjectInContexts(subject Subject, contexts map[string]bool) bool {
	for _, member := range subject.Members {
		if contexts[strings.ToLower(member.Context)] {
			return true
		}
	}
	return false
}

func cloneSubject(subject Subject) Subject {
	clone := subject
	clone.Members = append([]Node(nil), subject.Members...)
	clone.Identity = append([]string(nil), subject.Identity...)
	return clone
}

// Topology returns the subject's containment ancestry and all depth-one
// attachments. Edges retain their natural direction: dependent objects point
// at the infrastructure they use, while contains edges point parent-to-child.
func (g Graph) Topology(subject Subject) SubjectResult {
	result := g.newSubjectResult(subject)
	index, ok := g.subjectIndex(subject)
	if !ok {
		return result
	}
	checked := g.checkedContexts(g.subjects[index])
	for _, edge := range g.edges {
		if edge.relation == RelationContains {
			continue
		}
		if g.edgeTouchesSubject(edge, g.subjects[index].nodes) {
			result.Edges = append(result.Edges, g.edgeValue(edge))
			checked = appendContext(checked, g.records[edge.from].node.Context, g.records[edge.to].node.Context)
		}
	}
	// The chain was collected child-to-parent; wire output is root-to-subject.
	for _, node := range g.subjects[index].nodes {
		var chain []Edge
		current := node
		for {
			parentEdge := -1
			for _, edgeIndex := range g.incoming[current] {
				edge := g.edges[edgeIndex]
				if edge.relation == RelationContains {
					parentEdge = edgeIndex
					break
				}
			}
			if parentEdge < 0 {
				break
			}
			chain = append([]Edge{g.edgeValue(g.edges[parentEdge])}, chain...)
			current = g.edges[parentEdge].from
		}
		result.Ancestors = appendUniqueEdges(result.Ancestors, chain...)
		for _, unresolved := range g.unresolved[node] {
			result.Unresolved = appendUniqueString(result.Unresolved, unresolved)
		}
	}
	checked = sortedContexts(checked)
	sort.Strings(result.Unresolved)
	result.Blind = g.blindness(checked, Kind(subject.Kind))
	result.Blind = uniqueBlind(result.Blind)
	result.Confidence = g.verdict(result, index, checked)
	return result
}

// Dependencies follows the dependency direction from a subject to the
// objects it uses. depth is capped at five; non-positive values mean one.
func (g Graph) Dependencies(subject Subject, depth int) SubjectResult {
	return g.traverse(subject, depth, false)
}

// BlastRadius follows dependencies backwards, returning objects that rely on
// the subject. depth is capped at five; non-positive values mean one.
func (g Graph) BlastRadius(subject Subject, depth int) SubjectResult {
	return g.traverse(subject, depth, true)
}

func (g Graph) traverse(subject Subject, depth int, reverse bool) SubjectResult {
	result := g.newSubjectResult(subject)
	index, ok := g.subjectIndex(subject)
	if !ok {
		return result
	}
	if depth <= 0 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	frontier := append([]int(nil), g.subjects[index].nodes...)
	visited := make(map[int]bool)
	for _, node := range frontier {
		visited[node] = true
	}
	checked := g.checkedContexts(g.subjects[index])
	for level := 0; level < depth && len(frontier) > 0; level++ {
		next := make([]int, 0)
		for _, node := range frontier {
			for _, edgeIndex := range g.adjacency(node, reverse) {
				edge := g.edges[edgeIndex]
				if edge.relation == RelationContains {
					continue
				}
				result.Edges = appendUniqueEdges(result.Edges, g.edgeValue(edge))
				checked = appendContext(checked, g.records[edge.from].node.Context, g.records[edge.to].node.Context)
				neighbor := edge.to
				if reverse {
					neighbor = edge.from
				}
				if !visited[neighbor] {
					visited[neighbor] = true
					next = append(next, neighbor)
				}
			}
			for _, unresolved := range g.unresolved[node] {
				result.Unresolved = appendUniqueString(result.Unresolved, unresolved)
			}
		}
		frontier = next
	}
	for node := range visited {
		for _, unresolved := range g.unresolved[node] {
			result.Unresolved = appendUniqueString(result.Unresolved, unresolved)
		}
	}
	sort.Strings(result.Unresolved)
	checked = sortedContexts(checked)
	result.Blind = g.blindness(checked, Kind(subject.Kind))
	result.Confidence = g.verdict(result, index, checked)
	return result
}

func (g Graph) newSubjectResult(subject Subject) SubjectResult {
	return SubjectResult{Subject: cloneSubject(subject), Confidence: ConfidenceUnknown}
}

func (g Graph) subjectIndex(subject Subject) (int, bool) {
	for index, candidate := range g.subjects {
		if !strings.EqualFold(candidate.subject.Kind, subject.Kind) {
			continue
		}
		if subject.Name != "" && !strings.EqualFold(candidate.subject.Name, subject.Name) && !subjectMembersOverlap(candidate.subject.Members, subject.Members) {
			continue
		}
		if subjectMembersOverlap(candidate.subject.Members, subject.Members) {
			return index, true
		}
	}
	return -1, false
}

func subjectMembersOverlap(a, b []Node) bool {
	for _, left := range a {
		for _, right := range b {
			if strings.EqualFold(left.Context, right.Context) && ((left.ID != "" && strings.EqualFold(left.ID, right.ID)) || strings.EqualFold(left.Name, right.Name)) {
				return true
			}
		}
	}
	return false
}

func (g Graph) edgeTouchesSubject(edge graphEdge, nodes []int) bool {
	for _, node := range nodes {
		if edge.from == node || edge.to == node {
			return true
		}
	}
	return false
}

func (g Graph) adjacency(node int, reverse bool) []int {
	if reverse {
		return g.incoming[node]
	}
	return g.outgoing[node]
}

func appendUniqueEdges(values []Edge, additions ...Edge) []Edge {
	for _, addition := range additions {
		found := false
		for _, value := range values {
			if nodeSortKey(value.From) == nodeSortKey(addition.From) && nodeSortKey(value.To) == nodeSortKey(addition.To) && value.Relation == addition.Relation && value.Basis == addition.Basis && value.Detail == addition.Detail {
				found = true
				break
			}
		}
		if !found {
			values = append(values, addition)
		}
	}
	sort.SliceStable(values, func(i, j int) bool { return outputEdgeKey(values[i]) < outputEdgeKey(values[j]) })
	return values
}

func outputEdgeKey(edge Edge) string {
	return strings.Join([]string{nodeSortKey(edge.From), nodeSortKey(edge.To), string(edge.Relation), string(edge.Basis), string(edge.Confidence), edge.Detail}, "\x00")
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendContext(values []string, additions ...string) []string {
	for _, addition := range additions {
		if addition != "" && !contains(values, addition) {
			values = append(values, addition)
		}
	}
	return values
}

func sortedContexts(values []string) []string {
	seen := make(map[string]bool)
	for _, value := range values {
		if value != "" {
			seen[value] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (g Graph) verdict(result SubjectResult, index int, checked []string) ResultConfidence {
	if len(g.subjects[index].nodes) == 0 {
		return ConfidenceUnknown
	}
	coverageBlind := 0
	for _, blind := range result.Blind {
		if !strings.Contains(blind.Reason, "predates network inventory") {
			coverageBlind++
		}
	}
	if len(checked) > 0 && coverageBlind >= len(checked) {
		return ConfidenceUnknown
	}
	if Kind(result.Subject.Kind) == KindDatastore && g.schema >= 11 && len(result.Subject.Identity) == 0 && !g.subjectLocal(index) {
		return ConfidenceUnknown
	}
	if len(result.Blind) > 0 || len(result.Unresolved) > 0 || g.subjectUsesReconstructedNetwork(index) {
		return ConfidencePartial
	}
	return ConfidenceComplete
}

func (g Graph) subjectLocal(index int) bool {
	for _, node := range g.subjects[index].nodes {
		if node < len(g.records) && g.records[node].local {
			return true
		}
	}
	return false
}

func (g Graph) subjectUsesReconstructedNetwork(index int) bool {
	for _, node := range g.subjects[index].nodes {
		if g.records[node].reconstructed {
			return true
		}
		for _, edge := range g.outgoing[node] {
			other := g.edges[edge].to
			if other < len(g.records) && g.records[other].reconstructed {
				return true
			}
		}
	}
	return false
}
