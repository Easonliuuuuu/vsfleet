package assessment

import (
	"context"
	"fmt"
	"strings"
)

// ComparableBase picks what to compare target with: the newest capture before
// it that shares a context with it and recorded every one of contexts,
// preferring one of the same kind as ComparableBefore does. A pair sharing no
// context compares nothing, and would read as no changes. ok is false when no
// such capture exists.
func (s *Store) ComparableBase(ctx context.Context, target Run, contexts []string) (base Run, ok bool, err error) {
	runs, covers, err := s.comparisonCandidates(ctx, contexts)
	if err != nil {
		return Run{}, false, err
	}
	var earlier []Run
	for _, r := range runs {
		if r.ID != target.ID && r.StartedAt.Before(target.StartedAt) {
			earlier = append(earlier, r)
		}
	}
	base, ok = comparableAmong(earlier, target, covers)
	return base, ok, nil
}

// NewestComparablePair picks what "capture diff" compares when told nothing:
// the newest capture recording every one of contexts that has a ComparableBase,
// and that base. A newer capture with nothing to compare against, such as the
// first of a context, is passed over. ok is false when no such pair exists.
func (s *Store) NewestComparablePair(ctx context.Context, contexts []string) (base, target Run, ok bool, err error) {
	runs, covers, err := s.comparisonCandidates(ctx, contexts)
	if err != nil {
		return Run{}, Run{}, false, err
	}
	for i, r := range runs {
		if base, ok := comparableAmong(runs[i+1:], r, covers); ok {
			return base, r, true, nil
		}
	}
	return Run{}, Run{}, false, nil
}

func comparableAmong(earlier []Run, target Run, covers map[int64]map[string]bool) (Run, bool) {
	var sharing []Run
	for _, r := range earlier {
		if sharesKey(covers[r.ID], covers[target.ID]) {
			sharing = append(sharing, r)
		}
	}
	if len(sharing) == 0 {
		return Run{}, false
	}
	return ComparableBefore(sharing, target), true
}

func sharesKey(a, b map[string]bool) bool {
	for key := range a {
		if b[key] {
			return true
		}
	}
	return false
}

// comparisonCandidates returns the captures recording every one of contexts,
// newest first, and what each capture covers. A capture covers each context
// name it recorded and each vCenter it reached, so a context renamed between
// two captures of one vCenter still pairs them, as the diff itself does.
func (s *Store) comparisonCandidates(ctx context.Context, contexts []string) ([]Run, map[int64]map[string]bool, error) {
	contexts, err := s.validateHistoryContexts(ctx, contexts)
	if err != nil {
		return nil, nil, err
	}
	runs, err := s.runsRecording(ctx, contexts, true)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT run_id,name,vcenter_id FROM context_runs`)
	if err != nil {
		return nil, nil, fmt.Errorf("read captured contexts: %w", err)
	}
	defer rows.Close()
	covers := make(map[int64]map[string]bool)
	for rows.Next() {
		var id int64
		var name, vcenter string
		if err := rows.Scan(&id, &name, &vcenter); err != nil {
			return nil, nil, fmt.Errorf("read captured contexts: %w", err)
		}
		if covers[id] == nil {
			covers[id] = make(map[string]bool)
		}
		covers[id]["name:"+strings.ToLower(strings.TrimSpace(name))] = true
		if vcenter != "" {
			covers[id]["vcenter:"+vcenter] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read captured contexts: %w", err)
	}
	return runs, covers, nil
}
