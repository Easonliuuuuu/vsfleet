package assessment

import (
	"encoding/json"
	"sort"
)

// BlindContexts returns contexts where at least one required collection did
// not answer successfully. Empty collections are successful evidence.
func BlindContexts(contexts []ContextRun, needs []string) []string {
	if len(needs) == 0 {
		return nil
	}
	out := make([]string, 0)
	for _, context := range contexts {
		statuses := make(map[string]string, len(context.Collections))
		for _, collection := range context.Collections {
			statuses[collection.Kind] = collection.Status
		}
		blind := false
		for _, kind := range needs {
			status := statuses[kind]
			if kind == "vm" && context.VMStatus != "" {
				status = context.VMStatus
			}
			if !Successful(status) {
				blind = true
				break
			}
		}
		if blind {
			out = append(out, context.Name)
		}
	}
	sort.Strings(out)
	return out
}

// CoverageReason explains the first missing or failed collection for a
// context.
func CoverageReason(contexts []ContextRun, name string, needs []string) string {
	for _, context := range contexts {
		if context.Name != name {
			continue
		}
		statuses := make(map[string]CollectionRun, len(context.Collections))
		for _, collection := range context.Collections {
			statuses[collection.Kind] = collection
		}
		for _, kind := range needs {
			status, ok := statuses[kind]
			value := status.Status
			errorText := status.Error
			if kind == "vm" && context.VMStatus != "" {
				value, errorText = context.VMStatus, context.Error
				ok = true
			}
			if !ok || !Successful(value) {
				if !ok {
					return kind + " collection was not recorded"
				}
				return kind + " collection: " + nonempty(errorText, value)
			}
		}
	}
	return "required collection was not recorded"
}

// ContextComplete reports whether all standard persisted collections are
// complete. Callers choose the collection set so adding a new query-specific
// kind does not rewrite health coverage for historical runs.
func ContextComplete(c ContextRun, needs []string) bool {
	statuses := make(map[string]string, len(c.Collections))
	for _, collection := range c.Collections {
		statuses[collection.Kind] = collection.Status
	}
	for _, kind := range needs {
		status := statuses[kind]
		if kind == "vm" && c.VMStatus != "" {
			status = c.VMStatus
		}
		if !Successful(status) {
			return false
		}
	}
	return true
}

// DecodeResource decodes a persisted resource payload into target.
func DecodeResource(r ResourceObservation, target any) bool {
	return json.Unmarshal(r.Payload, target) == nil
}
