//go:build integration

package tests

import "testing"

type vcsimFixture struct {
	Name      string
	Endpoints map[string]*simEndpoint
}

func newVCSimFixture(t *testing.T, name string, specs []vcsimSpec) *vcsimFixture {
	t.Helper()
	fixture := &vcsimFixture{Name: name, Endpoints: make(map[string]*simEndpoint, len(specs))}
	for _, spec := range specs {
		if spec.Fixture == "" {
			spec.Fixture = name
		}
		fixture.Endpoints[spec.Name] = startEndpoint(t, spec)
	}
	return fixture
}

// fixtureBasicMultivcenter is vc-prod with two datacenters, one cluster per
// datacenter, two hosts per cluster, three VMs per pool, three datastores,
// three distributed port groups and one vApp per cluster; vc-edge has one
// datacenter, one cluster, two hosts, one VM, one datastore, one port group
// and one vApp. The generated names intentionally remain deterministic while
// the endpoint-local object identities remain independent.
func fixtureBasicMultivcenter(t *testing.T) *vcsimFixture {
	t.Helper()
	return newVCSimFixture(t, "basic-multivcenter", []vcsimSpec{
		{Fixture: "basic-multivcenter", Name: "vc-prod", Flags: []string{"-dc", "2", "-cluster", "1", "-host", "2", "-vm", "3", "-ds", "3", "-pg", "3", "-app", "1"}},
		{Fixture: "basic-multivcenter", Name: "vc-edge", Flags: []string{"-dc", "1", "-cluster", "1", "-host", "2", "-vm", "1", "-ds", "1", "-pg", "1", "-app", "1"}},
	})
}

// fixtureDuplicateNames starts two identical one-datacenter estates. Their
// generated DC0, DC0_C0, DC0_C0_RP0_VM0 and LocalDS_0 names collide by design, so
// identity and context provenance must keep the subjects separate.
func fixtureDuplicateNames(t *testing.T) *vcsimFixture {
	t.Helper()
	flags := []string{"-dc", "1", "-cluster", "1", "-host", "2", "-vm", "2", "-ds", "2", "-pg", "2", "-app", "1"}
	return newVCSimFixture(t, "duplicate-names", []vcsimSpec{
		{Fixture: "duplicate-names", Name: "vc-a", Flags: append([]string(nil), flags...)},
		{Fixture: "duplicate-names", Name: "vc-b", Flags: append([]string(nil), flags...)},
	})
}

// fixturePartialFailure starts one healthy endpoint and one endpoint that the
// scenario can terminate mid-suite; the third dead-port context is registered
// through addUnreachableContext to exercise both process loss and connection
// setup failure in one partial run.
func fixturePartialFailure(t *testing.T) *vcsimFixture {
	t.Helper()
	flags := []string{"-dc", "1", "-cluster", "1", "-host", "2", "-vm", "2", "-ds", "2", "-pg", "2", "-app", "1"}
	return newVCSimFixture(t, "partial-failure", []vcsimSpec{
		{Fixture: "partial-failure", Name: "healthy", Flags: append([]string(nil), flags...)},
		{Fixture: "partial-failure", Name: "killable", Flags: append([]string(nil), flags...)},
	})
}

// fixtureTopology is a one-datacenter, one-cluster graph with two hosts, two
// VMs, two datastores, two distributed port groups and one vApp. Its generated
// hierarchy is small enough for exact VM-to-host-to-cluster-to-datacenter and
// attachment assertions.
func fixtureTopology(t *testing.T) *vcsimFixture {
	t.Helper()
	return newVCSimFixture(t, "topology", []vcsimSpec{{
		Fixture: "topology", Name: "topology", Flags: []string{"-dc", "1", "-cluster", "1", "-host", "2", "-vm", "2", "-ds", "2", "-pg", "2", "-app", "1"},
	}})
}

// fixtureHistory is the basic deterministic shape used for three captures;
// the test mutates VM power and name through govmomi between captures.
func fixtureHistory(t *testing.T) *vcsimFixture {
	t.Helper()
	return newVCSimFixture(t, "history", []vcsimSpec{{
		Fixture: "history", Name: "history", Flags: []string{"-dc", "1", "-cluster", "1", "-host", "2", "-vm", "2", "-ds", "2", "-pg", "2", "-app", "1"},
	}})
}
