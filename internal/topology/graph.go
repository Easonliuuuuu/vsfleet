package topology

import (
	"sort"
	"strconv"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

type nodeRecord struct {
	node          Node
	kind          Kind
	keys          []string
	weak          string
	network       networkIdentity
	local         bool
	eligible      bool
	reconstructed bool
}

type graphEdge struct {
	from       int
	to         int
	relation   Relation
	basis      Basis
	confidence EdgeConfidence
	detail     string
}

type subjectRecord struct {
	subject Subject
	nodes   []int
}

// Graph is the immutable result of Build. Its methods do not touch external
// state, so the same graph can safely serve table and JSON renderers.
type Graph struct {
	data        assessment.ExportData
	records     []nodeRecord
	edges       []graphEdge
	unresolved  map[int][]string
	subjects    []subjectRecord
	nodeSubject []int
	incoming    map[int][]int
	outgoing    map[int][]int
	contexts    []string
	schema      int
	legacy      map[string]bool
}

type disjointSet struct{ parent, rank []int }

func newDisjointSet(n int) *disjointSet {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	return &disjointSet{parent: p, rank: make([]int, n)}
}

func (d *disjointSet) find(x int) int {
	if d.parent[x] != x {
		d.parent[x] = d.find(d.parent[x])
	}
	return d.parent[x]
}

func (d *disjointSet) union(a, b int) {
	a, b = d.find(a), d.find(b)
	if a == b {
		return
	}
	if d.rank[a] < d.rank[b] {
		a, b = b, a
	}
	d.parent[b] = a
	if d.rank[a] == d.rank[b] {
		d.rank[a]++
	}
}

// Build constructs one estate-wide graph from a stored assessment.
func Build(data assessment.ExportData) Graph {
	g := Graph{
		data: data, unresolved: make(map[int][]string), incoming: make(map[int][]int),
		outgoing: make(map[int][]int), nodeSubject: nil, schema: inventorySchema(data.Run.InventorySchemaVersion),
		legacy: make(map[string]bool),
	}
	for _, c := range data.Contexts {
		g.contexts = append(g.contexts, c.Name)
	}
	sort.Strings(g.contexts)

	// Resources and VM observations are intentionally flattened before any
	// relationship is considered. A cross-vCenter join must see the entire
	// estate, not one context at a time.
	for _, resource := range data.Resources {
		g.addResource(resource)
	}
	for _, item := range data.VMs {
		g.addVM(item.Observation)
	}

	// Distributed port groups and standard host port groups are relationship
	// evidence even when the persisted network collection predates schema 12.
	for i := range g.records {
		if !g.records[i].eligible {
			continue
		}
		g.addDerivedNetworks(i)
	}
	if g.schema < 12 {
		for i := range g.records {
			if g.records[i].kind != KindVM || !g.records[i].eligible {
				continue
			}
			g.addLegacyVMNetworks(i)
		}
	}

	g.makeSubjects()
	g.addRelationships()
	g.finalizeEdges()
	return g
}

func inventorySchema(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return n
}

func (g *Graph) addResource(resource assessment.ResourceObservation) {
	contextName, vcenter, datacenter, objectPath := resource.Context, resource.VCenterID, "", ""
	if contextName == "" {
		contextName = resource.Context
	}
	base := Node{Kind: resource.Kind, Name: resource.Name, ID: resource.ID, Context: contextName, VCenterID: vcenter}
	add := func(kind Kind, node Node, keys []string) int {
		node.Kind = string(kind)
		if node.Name == "" {
			node.Name = resource.Name
		}
		if node.ID == "" {
			node.ID = resource.ID
		}
		if node.Context == "" {
			node.Context = resource.Context
		}
		if node.VCenterID == "" {
			node.VCenterID = resource.VCenterID
		}
		idx := g.addRecord(nodeRecord{node: node, kind: kind, keys: uniqueLower(keys), weak: weakKey(kind, node.Context, node.Name, ""), eligible: true})
		g.addContainment(idx)
		return idx
	}

	switch resource.Kind {
	case string(KindHost):
		var value vsphere.Host
		if !assessment.DecodeResource(resource, &value) {
			return
		}
		contextName, vcenter, datacenter, objectPath = fillLocation(resource, value.Location)
		base = Node{Kind: string(KindHost), Name: value.Name, ID: value.ID, Context: contextName, VCenterID: vcenter, Datacenter: datacenter, Path: objectPath}
		add(KindHost, base, nil)
	case string(KindCluster):
		var value vsphere.Cluster
		if !assessment.DecodeResource(resource, &value) {
			return
		}
		contextName, vcenter, datacenter, objectPath = fillLocation(resource, value.Location)
		base = Node{Kind: string(KindCluster), Name: value.Name, ID: value.ID, Context: contextName, VCenterID: vcenter, Datacenter: datacenter, Path: objectPath}
		add(KindCluster, base, nil)
	case string(KindDatastore):
		var value vsphere.Datastore
		if !assessment.DecodeResource(resource, &value) {
			return
		}
		contextName, vcenter, datacenter, objectPath = fillLocation(resource, value.Location)
		base = Node{Kind: string(KindDatastore), Name: value.Name, ID: value.ID, Context: contextName, VCenterID: vcenter, Datacenter: datacenter, Path: objectPath}
		idx := add(KindDatastore, base, value.IdentityKeys())
		g.records[idx].local = value.Backing.Local
	case string(KindResourcePool):
		var value vsphere.ResourcePool
		if !assessment.DecodeResource(resource, &value) {
			return
		}
		contextName, vcenter, datacenter, objectPath = fillLocation(resource, value.Location)
		base = Node{Kind: string(KindResourcePool), Name: value.Name, ID: value.ID, Context: contextName, VCenterID: vcenter, Datacenter: datacenter, Path: objectPath}
		add(KindResourcePool, base, nil)
	case string(KindDVSwitch):
		var value vsphere.DVSwitch
		if !assessment.DecodeResource(resource, &value) {
			return
		}
		contextName, vcenter, datacenter, objectPath = fillLocation(resource, value.Location)
		base = Node{Kind: string(KindDVSwitch), Name: value.Name, ID: value.ID, Context: contextName, VCenterID: vcenter, Datacenter: datacenter, Path: objectPath}
		keys := []string{}
		if value.UUID != "" {
			keys = append(keys, "dvswitch:"+strings.ToLower(strings.TrimSpace(value.UUID)))
		}
		idx := add(KindDVSwitch, base, keys)
		g.records[idx].network.switchUUID = value.UUID
	case string(KindNetwork):
		var value vsphere.Network
		if !assessment.DecodeResource(resource, &value) {
			return
		}
		contextName, vcenter, datacenter, objectPath = fillLocation(resource, value.Location)
		base = Node{Kind: string(KindNetwork), Name: value.Name, ID: value.ID, Context: contextName, VCenterID: vcenter, Datacenter: datacenter, Path: objectPath}
		idx := add(KindNetwork, base, nil)
		g.records[idx].network.vlan = value.VLAN
		g.records[idx].weak = weakKey(KindNetwork, base.Context, base.Name, value.VLAN)
	}
}

func fillLocation(resource assessment.ResourceObservation, location vsphere.Location) (string, string, string, string) {
	contextName := location.Context
	if contextName == "" {
		contextName = resource.Context
	}
	vcenter := resource.VCenterID
	datacenter := location.Datacenter
	objectPath := location.Path
	return contextName, vcenter, datacenter, objectPath
}

func (g *Graph) addVM(observation assessment.Observation) {
	vm := observation.VM
	contextName := observation.Context
	if contextName == "" {
		contextName = vm.Context
	}
	nodeKind := KindVM
	if vm.IsTemplate {
		nodeKind = KindTemplate
	}
	keys := make([]string, 0, 2)
	if value := strings.TrimSpace(vm.InstanceUUID); value != "" {
		keys = append(keys, "instance-uuid:"+strings.ToLower(value))
	}
	if value := strings.TrimSpace(vm.BIOSUUID); value != "" {
		keys = append(keys, "bios-uuid:"+strings.ToLower(value))
	}
	node := Node{Kind: string(nodeKind), Name: vm.Name, ID: vm.ID, Context: contextName, VCenterID: observation.VCenterID, Datacenter: vm.Datacenter, Path: vm.Path}
	idx := g.addRecord(nodeRecord{node: node, kind: nodeKind, keys: uniqueLower(keys), weak: weakKey(nodeKind, contextName, vm.Name, ""), eligible: true})
	g.addContainment(idx)
}

func (g *Graph) addRecord(record nodeRecord) int {
	if record.node.Datacenter == "" {
		for _, c := range g.data.Contexts {
			if c.Name == record.node.Context {
				record.node.Datacenter = c.Datacenter
				break
			}
		}
	}
	if record.node.VCenterID == "" {
		for _, c := range g.data.Contexts {
			if c.Name == record.node.Context {
				record.node.VCenterID = c.VCenterID
				break
			}
		}
	}
	if record.node.Context == "" {
		for _, c := range g.data.Contexts {
			if c.VCenterID == record.node.VCenterID && record.node.VCenterID != "" {
				record.node.Context = c.Name
				break
			}
		}
	}
	if record.weak == "" {
		record.weak = weakKey(record.kind, record.node.Context, record.node.Name, record.network.vlan)
	}
	idx := len(g.records)
	g.records = append(g.records, record)
	return idx
}

func (g *Graph) addContainment(child int) {
	n := g.records[child].node
	if n.Context == "" {
		return
	}
	parent := g.ensureVCenter(n.Context, n.VCenterID)
	if n.Datacenter != "" {
		parent = g.ensureDatacenter(parent, n.Context, n.VCenterID, n.Datacenter)
	}
	pathValue := strings.Trim(strings.ReplaceAll(n.Path, "\\", "/"), "/")
	parts := strings.Split(pathValue, "/")
	if len(parts) > 1 && n.Datacenter != "" && strings.EqualFold(parts[0], n.Datacenter) {
		currentPath := parts[0]
		for i, part := range parts[1:] {
			if i == len(parts[1:])-1 && strings.EqualFold(part, n.Name) {
				break
			}
			if part == "" {
				continue
			}
			currentPath += "/" + part
			parent = g.ensureFolder(parent, n.Context, n.VCenterID, part, "/"+currentPath)
		}
	}
	if parent >= 0 {
		g.edges = append(g.edges, graphEdge{from: parent, to: child, relation: RelationContains, basis: BasisMoref, confidence: EdgeConfirmed})
	}
}

func (g *Graph) ensureVCenter(contextName, vcenter string) int {
	key := "vcenter\x00" + strings.ToLower(contextName) + "\x00" + vcenter
	for i, r := range g.records {
		if r.node.Kind == "vcenter" && nodeIdentity(r.node) == key {
			return i
		}
	}
	return g.addSynthetic(Node{Kind: "vcenter", Name: contextName, ID: vcenter, Context: contextName, VCenterID: vcenter}, key)
}

func (g *Graph) ensureDatacenter(parent int, contextName, vcenter, datacenter string) int {
	key := "datacenter\x00" + strings.ToLower(contextName) + "\x00" + strings.ToLower(datacenter)
	for i, r := range g.records {
		if r.node.Kind == "datacenter" && nodeIdentity(r.node) == key {
			return i
		}
	}
	idx := g.addSynthetic(Node{Kind: "datacenter", Name: datacenter, ID: vcenter + ":datacenter:" + datacenter, Context: contextName, VCenterID: vcenter, Datacenter: datacenter, Path: "/" + datacenter}, key)
	g.edges = append(g.edges, graphEdge{from: parent, to: idx, relation: RelationContains, basis: BasisMoref, confidence: EdgeConfirmed})
	return idx
}

func (g *Graph) ensureFolder(parent int, contextName, vcenter, name, pathValue string) int {
	key := "folder\x00" + strings.ToLower(contextName) + "\x00" + strings.ToLower(pathValue)
	for i, r := range g.records {
		if r.node.Kind == "folder" && nodeIdentity(r.node) == key {
			return i
		}
	}
	idx := g.addSynthetic(Node{Kind: "folder", Name: name, ID: vcenter + ":folder:" + pathValue, Context: contextName, VCenterID: vcenter, Path: pathValue}, key)
	g.edges = append(g.edges, graphEdge{from: parent, to: idx, relation: RelationContains, basis: BasisMoref, confidence: EdgeConfirmed})
	return idx
}

func (g *Graph) addSynthetic(node Node, key string) int {
	idx := len(g.records)
	g.records = append(g.records, nodeRecord{node: node, kind: Kind(node.Kind), eligible: false, weak: key})
	return idx
}

func nodeIdentity(n Node) string {
	return strings.ToLower(n.Kind) + "\x00" + strings.ToLower(n.Context) + "\x00" + n.ID
}

func (g *Graph) addDerivedNetworks(parent int) {
	r := g.records[parent]
	switch r.kind {
	case KindDVSwitch:
		value, ok := g.resourceValue(parent).(vsphere.DVSwitch)
		if !ok {
			return
		}
		for _, pg := range value.PortGroups {
			identity := networkIdentity{portGroupKey: pg.Key, switchUUID: value.UUID, vlan: pg.VLAN}
			if pg.LogicalSwitchUUID != "" {
				identity.logicalSwitch = pg.LogicalSwitchUUID
			}
			if pg.SegmentID != "" {
				identity.segmentID = pg.SegmentID
			}
			identity.keys = networkKeys(identity)
			node := Node{Kind: string(KindNetwork), Name: pg.Name, ID: pg.ID, Context: r.node.Context, VCenterID: r.node.VCenterID, Datacenter: r.node.Datacenter, Path: r.node.Path + "/" + pg.Name}
			idx := g.addNetworkRecord(node, identity, g.schema < 12)
			g.edges = append(g.edges, graphEdge{from: parent, to: idx, relation: RelationContains, basis: BasisMoref, confidence: EdgeConfirmed, detail: pg.Key})
		}
	case KindHost:
		value, ok := g.resourceValue(parent).(vsphere.Host)
		if !ok {
			return
		}
		for _, pg := range value.PortGroups {
			node := Node{Kind: string(KindNetwork), Name: pg.Name, ID: pg.Key, Context: r.node.Context, VCenterID: r.node.VCenterID, Datacenter: r.node.Datacenter, Path: r.node.Path + "/" + pg.Name}
			identity := networkIdentity{portGroupKey: pg.Key, vlan: strconv.FormatInt(int64(pg.VLAN), 10)}
			idx := g.addNetworkRecord(node, identity, g.schema < 12)
			g.edges = append(g.edges, graphEdge{from: parent, to: idx, relation: RelationPresents, basis: BasisName, confidence: EdgeInferred, detail: identity.vlan})
		}
	}
}

func (g *Graph) addLegacyVMNetworks(parent int) {
	value, ok := g.resourceValue(parent).(vsphere.VM)
	if !ok {
		return
	}
	for _, nic := range value.NICs {
		name := nic.Network
		if name == "" {
			name = nic.NetworkID
		}
		if name == "" {
			continue
		}
		identity := networkIdentity{portGroupKey: nic.NetworkID, switchUUID: nic.SwitchID}
		identity.keys = networkKeys(identity)
		node := Node{Kind: string(KindNetwork), Name: name, Context: g.records[parent].node.Context, VCenterID: g.records[parent].node.VCenterID, Datacenter: g.records[parent].node.Datacenter}
		g.addNetworkRecord(node, identity, true)
	}
}

func (g *Graph) addNetworkRecord(node Node, identity networkIdentity, reconstructed bool) int {
	idx := len(g.records)
	g.records = append(g.records, nodeRecord{node: node, kind: KindNetwork, keys: uniqueLower(identity.keys), weak: weakKey(KindNetwork, node.Context, node.Name, identity.vlan), network: identity, eligible: true, reconstructed: reconstructed})
	if reconstructed {
		g.legacy[node.Context] = true
	}
	return idx
}

func (g *Graph) resourceValue(index int) any {
	r := g.records[index]
	for _, resource := range g.data.Resources {
		if resource.Kind != string(r.kind) || !strings.EqualFold(resource.Context, r.node.Context) {
			continue
		}
		if resource.ID != "" && r.node.ID != "" && !strings.EqualFold(resource.ID, r.node.ID) {
			continue
		}
		if resource.Name != "" && r.node.Name != "" && !strings.EqualFold(resource.Name, r.node.Name) {
			continue
		}
		switch r.kind {
		case KindHost:
			var v vsphere.Host
			if assessment.DecodeResource(resource, &v) {
				return v
			}
		case KindCluster:
			var v vsphere.Cluster
			if assessment.DecodeResource(resource, &v) {
				return v
			}
		case KindDatastore:
			var v vsphere.Datastore
			if assessment.DecodeResource(resource, &v) {
				return v
			}
		case KindResourcePool:
			var v vsphere.ResourcePool
			if assessment.DecodeResource(resource, &v) {
				return v
			}
		case KindDVSwitch:
			var v vsphere.DVSwitch
			if assessment.DecodeResource(resource, &v) {
				return v
			}
		case KindNetwork:
			var v vsphere.Network
			if assessment.DecodeResource(resource, &v) {
				return v
			}
		}
	}
	for _, item := range g.data.VMs {
		contextName := item.Observation.Context
		if contextName == "" {
			contextName = item.Observation.VM.Context
		}
		if contextName == r.node.Context && item.Observation.VM.ID == r.node.ID && item.Observation.VM.Name == r.node.Name {
			return item.Observation.VM
		}
	}
	return nil
}

func (g *Graph) makeSubjects() {
	dsu := newDisjointSet(len(g.records))
	byStrong := make(map[string][]int)
	byWeak := make(map[string][]int)
	for i, r := range g.records {
		if !r.eligible {
			continue
		}
		for _, key := range r.keys {
			byStrong[strings.ToLower(string(r.kind))+"\x00"+key] = append(byStrong[strings.ToLower(string(r.kind))+"\x00"+key], i)
		}
		byWeak[strings.ToLower(string(r.kind))+"\x00"+r.weak] = append(byWeak[strings.ToLower(string(r.kind))+"\x00"+r.weak], i)
	}
	for _, values := range byStrong {
		for i := 1; i < len(values); i++ {
			dsu.union(values[0], values[i])
		}
	}
	for _, values := range byWeak {
		var unknown []int
		var keyed []int
		for _, index := range values {
			if len(g.records[index].keys) == 0 {
				unknown = append(unknown, index)
			} else {
				keyed = append(keyed, index)
			}
		}
		for i := 1; i < len(unknown); i++ {
			dsu.union(unknown[0], unknown[i])
		}
		roots := make(map[int]bool)
		for _, index := range keyed {
			roots[dsu.find(index)] = true
		}
		if len(roots) == 1 && len(unknown) > 0 {
			for _, index := range unknown {
				dsu.union(index, keyed[0])
			}
		}
	}

	groups := make(map[int][]int)
	for i, r := range g.records {
		if r.eligible {
			groups[dsu.find(i)] = append(groups[dsu.find(i)], i)
		}
	}
	for _, values := range groups {
		g.subjects = append(g.subjects, makeSubject(values, g.records))
	}
	sort.SliceStable(g.subjects, func(i, j int) bool {
		return subjectSortKey(g.subjects[i].subject) < subjectSortKey(g.subjects[j].subject)
	})
	g.nodeSubject = make([]int, len(g.records))
	for i := range g.nodeSubject {
		g.nodeSubject[i] = -1
	}
	for si, subject := range g.subjects {
		for _, node := range subject.nodes {
			g.nodeSubject[node] = si
		}
	}
}

func makeSubject(values []int, records []nodeRecord) subjectRecord {
	sort.SliceStable(values, func(i, j int) bool {
		return memberRecordSortKey(records[values[i]]) < memberRecordSortKey(records[values[j]])
	})
	members := make([]Node, 0)
	memberByContext := make(map[string]int)
	identity := make([]string, 0)
	identitySeen := make(map[string]bool)
	name := ""
	basis := BasisName
	for _, index := range values {
		r := records[index]
		if name == "" || strings.ToLower(r.node.Name) < strings.ToLower(name) {
			name = r.node.Name
		}
		for _, key := range r.keys {
			if !identitySeen[key] {
				identitySeen[key] = true
				identity = append(identity, key)
			}
		}
		if len(r.keys) > 0 {
			basis = basisForKeys(r.keys)
		}
		ctx := strings.ToLower(r.node.Context)
		if _, ok := memberByContext[ctx]; !ok {
			memberByContext[ctx] = len(members)
			members = append(members, r.node)
		}
	}
	sort.SliceStable(members, func(i, j int) bool { return nodeSortKey(members[i]) < nodeSortKey(members[j]) })
	sort.Strings(identity)
	return subjectRecord{subject: Subject{Kind: records[values[0]].node.Kind, Name: name, Members: members, Identity: identity, Basis: basis}, nodes: append([]int(nil), values...)}
}

func basisForKeys(keys []string) Basis {
	for _, key := range keys {
		switch {
		case strings.HasPrefix(key, "instance-uuid:"), strings.HasPrefix(key, "bios-uuid:"):
			return BasisInstanceUUID
		case strings.HasPrefix(key, "portgroup:"):
			return BasisPortGroupKey
		case strings.HasPrefix(key, "dvswitch:") || strings.HasPrefix(key, "logical-switch:"):
			return BasisSwitchUUID
		case strings.HasPrefix(key, "vmfs:") || strings.HasPrefix(key, "extent:") || strings.HasPrefix(key, "nas:") || strings.HasPrefix(key, "vvol:") || strings.HasPrefix(key, "url:"):
			return BasisBackingIdentity
		}
	}
	return BasisMoref
}

func (g *Graph) addRelationships() {
	// First pass: references that are directly reported on VMs.
	for i, r := range g.records {
		if !r.eligible {
			continue
		}
		switch r.kind {
		case KindVM, KindTemplate:
			g.addVMRelationships(i)
		case KindHost:
			g.addHostRelationships(i)
		case KindDVSwitch:
			g.addDVSwitchRelationships(i)
		case KindResourcePool:
			g.addResourcePoolRelationships(i)
		}
	}
	// A host's multipath view is authoritative storage presentation evidence;
	// VM placement supplies a weaker, explicitly inferred host/datastore edge.
	for i, r := range g.records {
		if r.kind != KindVM && r.kind != KindTemplate {
			continue
		}
		for _, edge := range g.edges {
			if edge.from != i || edge.relation != RelationStoredOn {
				continue
			}
			for _, host := range g.edges {
				if host.from == i && host.relation == RelationRunsOn {
					g.addEdge(host.from, edge.to, RelationPresents, BasisName, EdgeInferred, r.node.Name)
				}
			}
		}
	}
}

func (g *Graph) addVMRelationships(index int) {
	vm, ok := g.resourceValue(index).(vsphere.VM)
	if !ok {
		return
	}
	r := g.records[index]
	if vm.Host != "" {
		g.addNamedReference(index, KindHost, vm.Host, RelationRunsOn, BasisName, EdgeInferred, "host "+vm.Host)
	}
	if vm.Cluster != "" {
		g.addNamedReference(index, KindCluster, vm.Cluster, RelationRunsOn, BasisName, EdgeInferred, "cluster "+vm.Cluster)
	}
	for _, disk := range vm.Disks {
		name, relative, valid := vsphere.SplitDatastorePath(disk.BackingPath)
		if !valid || name == "" || relative == "" {
			continue
		}
		candidates := g.namedCandidates(KindDatastore, r.node.Context, name, "")
		if len(candidates) == 0 {
			g.addUnresolvedEdge(index, KindDatastore, name, RelationStoredOn, BasisDiskPath, EdgeUnresolved, disk.BackingPath)
			continue
		}
		g.addCandidates(index, candidates, RelationStoredOn, BasisDiskPath, EdgeConfirmed, disk.BackingPath, "datastore "+name)
	}
	for _, name := range vm.Datastores {
		candidates := g.namedCandidates(KindDatastore, r.node.Context, name, "")
		if len(candidates) == 0 {
			g.addUnresolvedEdge(index, KindDatastore, name, RelationStoredOn, BasisName, EdgeUnresolved, name)
			continue
		}
		g.addCandidates(index, candidates, RelationStoredOn, BasisName, EdgeInferred, name, "datastore "+name)
	}
	for _, nic := range vm.NICs {
		candidates, basis, confidence := g.networkCandidates(r.node.Context, nic)
		if len(candidates) == 0 {
			label := nic.Network
			if label == "" {
				label = nic.NetworkID
			}
			if label != "" {
				g.addUnresolvedEdge(index, KindNetwork, label, RelationAttachedTo, basis, EdgeUnresolved, nic.NetworkID)
			}
			continue
		}
		g.addCandidates(index, candidates, RelationAttachedTo, basis, confidence, nic.NetworkID, "network")
	}
}

func (g *Graph) addHostRelationships(index int) {
	host, ok := g.resourceValue(index).(vsphere.Host)
	if !ok {
		return
	}
	if host.Cluster != "" {
		g.addNamedReference(index, KindCluster, host.Cluster, RelationMemberOf, BasisName, EdgeInferred, "cluster "+host.Cluster)
	}
	for _, multipath := range host.Multipaths {
		lun := strings.ToLower(strings.TrimSpace(multipath.LUN))
		if lun == "" {
			continue
		}
		var candidates []int
		for j, r := range g.records {
			if r.kind != KindDatastore || r.node.Context != g.records[index].node.Context {
				continue
			}
			for _, extent := range datastoreExtents(j, g) {
				if strings.ToLower(strings.TrimSpace(extent)) == lun {
					candidates = append(candidates, j)
					break
				}
			}
		}
		if len(candidates) == 0 {
			g.addUnresolvedEdge(index, KindDatastore, multipath.LUN, RelationPresents, BasisBackingIdentity, EdgeUnresolved, multipath.LUN)
			continue
		}
		g.addCandidates(index, candidates, RelationPresents, BasisBackingIdentity, EdgeConfirmed, multipath.LUN, "datastore")
	}
	for _, pg := range host.PortGroups {
		candidates := g.namedCandidates(KindNetwork, g.records[index].node.Context, pg.Name, strconv.FormatInt(int64(pg.VLAN), 10))
		if len(candidates) == 0 {
			candidates = g.namedCandidates(KindNetwork, g.records[index].node.Context, pg.Name, "")
		}
		if len(candidates) == 0 {
			g.addUnresolvedEdge(index, KindNetwork, pg.Name, RelationPresents, BasisName, EdgeUnresolved, strconv.FormatInt(int64(pg.VLAN), 10))
			continue
		}
		g.addCandidates(index, candidates, RelationPresents, BasisName, EdgeInferred, strconv.FormatInt(int64(pg.VLAN), 10), "network "+pg.Name)
	}
}

func (g *Graph) addDVSwitchRelationships(index int) {
	switchValue, ok := g.resourceValue(index).(vsphere.DVSwitch)
	if !ok {
		return
	}
	for _, name := range switchValue.Hosts {
		g.addNamedReference(index, KindHost, name, RelationAttachedTo, BasisName, EdgeInferred, "host "+name)
	}
}

func (g *Graph) addResourcePoolRelationships(index int) {
	pool, ok := g.resourceValue(index).(vsphere.ResourcePool)
	if !ok {
		return
	}
	for _, reference := range pool.VMRefs {
		id := strings.TrimSpace(reference)
		if colon := strings.LastIndexByte(id, ':'); colon >= 0 {
			id = id[colon+1:]
		}
		candidates := g.morefCandidates(KindVM, g.records[index].node.VCenterID, id)
		if len(candidates) == 0 {
			candidates = g.morefCandidates(KindTemplate, g.records[index].node.VCenterID, id)
		}
		if len(candidates) == 0 {
			g.addUnresolvedEdge(index, KindVM, id, RelationContains, BasisVMRef, EdgeUnresolved, reference)
			continue
		}
		g.addCandidates(index, candidates, RelationContains, BasisVMRef, EdgeConfirmed, reference, "VM reference "+reference)
	}
}

func datastoreExtents(index int, g *Graph) []string {
	value, ok := g.resourceValue(index).(vsphere.Datastore)
	if !ok {
		return nil
	}
	return value.Backing.Extents
}

func (g *Graph) networkCandidates(contextName string, nic vsphere.VMNIC) ([]int, Basis, EdgeConfidence) {
	var candidates []int
	if nic.NetworkID != "" {
		for i, r := range g.records {
			if r.kind != KindNetwork || !strings.EqualFold(r.node.Context, contextName) {
				continue
			}
			if nic.SwitchID != "" && r.network.switchUUID != "" && strings.EqualFold(nic.SwitchID, r.network.switchUUID) && strings.EqualFold(nic.NetworkID, r.network.portGroupKey) {
				candidates = append(candidates, i)
			} else if nic.SwitchID == "" && strings.EqualFold(nic.NetworkID, r.network.portGroupKey) {
				candidates = append(candidates, i)
			}
		}
		if len(candidates) > 0 {
			return uniqueNodeCandidates(candidates, g), BasisPortGroupKey, EdgeConfirmed
		}
	}
	if nic.Network != "" {
		candidates = g.namedCandidates(KindNetwork, contextName, nic.Network, "")
		return candidates, BasisName, EdgeInferred
	}
	return nil, BasisPortGroupKey, EdgeUnresolved
}

func (g *Graph) addNamedReference(from int, kind Kind, name string, relation Relation, basis Basis, confidence EdgeConfidence, unresolved string) {
	candidates := g.namedCandidates(kind, g.records[from].node.Context, name, "")
	if len(candidates) == 0 {
		g.addUnresolvedEdge(from, kind, name, relation, basis, EdgeUnresolved, name)
		return
	}
	g.addCandidates(from, candidates, relation, basis, confidence, name, unresolved)
}

func (g *Graph) namedCandidates(kind Kind, contextName, name, vlan string) []int {
	key := weakKey(kind, contextName, name, vlan)
	var out []int
	for i, r := range g.records {
		if !r.eligible || r.kind != kind || !strings.EqualFold(r.node.Context, contextName) || !strings.EqualFold(r.weak, key) {
			continue
		}
		out = append(out, i)
	}
	if len(out) == 0 {
		for i, r := range g.records {
			if r.eligible && r.kind == kind && strings.EqualFold(r.node.Context, contextName) && strings.EqualFold(r.node.Name, name) {
				out = append(out, i)
			}
		}
	}
	return uniqueNodeCandidates(out, g)
}

func (g *Graph) morefCandidates(kind Kind, vcenter, id string) []int {
	var out []int
	for i, r := range g.records {
		if r.eligible && r.kind == kind && r.node.VCenterID == vcenter && strings.EqualFold(r.node.ID, id) {
			out = append(out, i)
		}
	}
	return uniqueNodeCandidates(out, g)
}

func uniqueNodeCandidates(values []int, g *Graph) []int {
	seen := make(map[int]bool)
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value < 0 || value >= len(g.records) || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func (g *Graph) addCandidates(from int, candidates []int, relation Relation, basis Basis, confidence EdgeConfidence, detail, unresolved string) {
	candidates = uniqueNodeCandidates(candidates, g)
	if len(candidates) == 0 {
		if unresolved != "" {
			g.addUnresolved(from, unresolved)
		}
		return
	}
	// Multiple observations of one logical subject are one edge. Distinct
	// same-name subjects remain ambiguous and are deliberately unresolved.
	subjects := make(map[int]bool)
	for _, candidate := range candidates {
		if candidate < len(g.nodeSubject) && g.nodeSubject[candidate] >= 0 {
			subjects[g.nodeSubject[candidate]] = true
		}
	}
	if len(subjects) > 1 {
		if unresolved != "" {
			g.addUnresolved(from, unresolved)
		}
		return
	}
	canonical := candidates[0]
	for _, candidate := range candidates[1:] {
		if nodeSortKey(g.records[candidate].node) < nodeSortKey(g.records[canonical].node) {
			canonical = candidate
		}
	}
	g.addEdge(from, canonical, relation, basis, confidence, detail)
}

func (g *Graph) addEdge(from, to int, relation Relation, basis Basis, confidence EdgeConfidence, detail string) {
	if from == to {
		return
	}
	g.edges = append(g.edges, graphEdge{from: from, to: to, relation: relation, basis: basis, confidence: confidence, detail: detail})
}

func (g *Graph) addUnresolved(index int, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, existing := range g.unresolved[index] {
		if existing == value {
			return
		}
	}
	g.unresolved[index] = append(g.unresolved[index], value)
}

func (g *Graph) addUnresolvedEdge(from int, kind Kind, name string, relation Relation, basis Basis, confidence EdgeConfidence, detail string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	node := Node{Kind: string(kind), Name: name, Context: g.records[from].node.Context, VCenterID: g.records[from].node.VCenterID, Datacenter: g.records[from].node.Datacenter}
	target := g.addSynthetic(node, "unresolved\x00"+nodeIdentity(node))
	g.addEdge(from, target, relation, basis, confidence, detail)
	g.addUnresolved(from, name)
}

func (g *Graph) finalizeEdges() {
	// Collapse duplicate edges and retain the strongest evidence when a disk
	// path and a display-name fallback describe the same relationship.
	type edgeKey struct {
		from, to int
		relation Relation
	}
	best := make(map[edgeKey]graphEdge)
	for _, edge := range g.edges {
		key := edgeKey{edge.from, edge.to, edge.relation}
		old, ok := best[key]
		if !ok || edgeScore(edge) > edgeScore(old) || (edgeScore(edge) == edgeScore(old) && edge.detail < old.detail) {
			best[key] = edge
		}
	}
	g.edges = g.edges[:0]
	for _, edge := range best {
		g.edges = append(g.edges, edge)
	}
	sort.SliceStable(g.edges, func(i, j int) bool { return edgeSortKey(g.edges[i], g.records) < edgeSortKey(g.edges[j], g.records) })
	for i, edge := range g.edges {
		g.outgoing[edge.from] = append(g.outgoing[edge.from], i)
		g.incoming[edge.to] = append(g.incoming[edge.to], i)
	}
	for index := range g.unresolved {
		sort.Strings(g.unresolved[index])
	}
}

func edgeScore(edge graphEdge) int {
	score := 0
	if edge.confidence == EdgeConfirmed {
		score += 20
	}
	if edge.basis == BasisDiskPath || edge.basis == BasisBackingIdentity || edge.basis == BasisPortGroupKey || edge.basis == BasisSwitchUUID {
		score += 5
	}
	return score
}

func weakKey(kind Kind, contextName, name, vlan string) string {
	return strings.ToLower(string(kind)) + "\x00" + strings.ToLower(strings.TrimSpace(contextName)) + "\x00" + strings.ToLower(strings.TrimSpace(name)) + "\x00" + strings.ToLower(strings.TrimSpace(vlan))
}

func nodeSortKey(node Node) string {
	return strings.ToLower(strings.Join([]string{node.Kind, node.Context, node.Name, node.ID, node.Path}, "\x00"))
}

func memberRecordSortKey(record nodeRecord) string {
	reconstructed := "0"
	if record.reconstructed {
		reconstructed = "1"
	}
	idMissing := "0"
	if record.node.ID == "" {
		idMissing = "1"
	}
	return strings.Join([]string{reconstructed, idMissing, nodeSortKey(record.node)}, "\x00")
}

func subjectSortKey(subject Subject) string {
	return strings.ToLower(strings.Join([]string{subject.Kind, subject.Name, firstContext(subject.Members)}, "\x00"))
}

func firstContext(nodes []Node) string {
	if len(nodes) == 0 {
		return ""
	}
	return nodes[0].Context
}

func edgeSortKey(edge graphEdge, records []nodeRecord) string {
	return strings.Join([]string{nodeSortKey(records[edge.from].node), nodeSortKey(records[edge.to].node), string(edge.relation), string(edge.basis), string(edge.confidence), edge.detail}, "\x00")
}

func (g Graph) edgeValue(edge graphEdge) Edge {
	return Edge{From: g.records[edge.from].node, To: g.records[edge.to].node, Relation: edge.relation, Basis: edge.basis, Confidence: edge.confidence, Detail: edge.detail}
}

func (g Graph) checkedContexts(subject subjectRecord) []string {
	seen := make(map[string]bool)
	for _, index := range subject.nodes {
		seen[g.records[index].node.Context] = true
	}
	for _, index := range subject.nodes {
		r := g.records[index]
		for _, other := range g.records {
			if !other.eligible || other.kind != r.kind || !strings.EqualFold(other.node.Name, r.node.Name) || other.node.Context == r.node.Context {
				continue
			}
			if len(r.keys) == 0 || len(other.keys) == 0 || intersects(r.keys, other.keys) {
				seen[other.node.Context] = true
			}
		}
	}
	// A failed required collection has no observed object to match, but it is
	// still part of the estate-wide search scope. Naming it prevents a query
	// from presenting "not found" in an unreachable context as evidence of
	// absence.
	failed := assessment.BlindContexts(g.data.Contexts, g.needs(subject.subjectKind()))
	for _, contextName := range failed {
		seen[contextName] = true
	}
	seen[""] = false
	out := make([]string, 0, len(seen))
	for name, ok := range seen {
		if ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (s subjectRecord) subjectKind() Kind { return Kind(s.subject.Kind) }

func intersects(a, b []string) bool {
	seen := make(map[string]bool, len(a))
	for _, value := range a {
		seen[value] = true
	}
	for _, value := range b {
		if seen[value] {
			return true
		}
	}
	return false
}

func (g Graph) needs(kind Kind) []string {
	switch kind {
	case KindVM, KindTemplate:
		needs := []string{"vm", "host", "cluster", "datastore", "resourcepool", "dvswitch"}
		if g.schema >= 12 {
			needs = append(needs, "network")
		}
		return needs
	case KindDatastore:
		return []string{"datastore", "vm", "host"}
	case KindNetwork:
		needs := []string{"vm", "host", "dvswitch"}
		if g.schema >= 12 {
			needs = append(needs, "network")
		}
		return needs
	case KindHost:
		needs := []string{"host", "cluster", "datastore", "dvswitch", "vm"}
		if g.schema >= 12 {
			needs = append(needs, "network")
		}
		return needs
	case KindCluster:
		return []string{"cluster", "host", "vm"}
	case KindResourcePool:
		return []string{"resourcepool", "vm"}
	case KindDVSwitch:
		needs := []string{"dvswitch", "host", "vm"}
		if g.schema >= 12 {
			needs = append(needs, "network")
		}
		return needs
	default:
		return nil
	}
}

func (g Graph) blindness(checked []string, kind Kind) []Blindness {
	blind := make([]Blindness, 0)
	if g.schema < 12 {
		for _, contextName := range checked {
			if g.legacy[contextName] || kind == KindNetwork {
				blind = append(blind, Blindness{Context: contextName, Reason: "inventory schema predates network inventory"})
			}
		}
	}
	failed := assessment.BlindContexts(g.data.Contexts, g.needs(kind))
	for _, contextName := range failed {
		if !contains(checked, contextName) {
			continue
		}
		blind = append(blind, Blindness{Context: contextName, Reason: assessment.CoverageReason(g.data.Contexts, contextName, g.needs(kind))})
	}
	return uniqueBlind(blind)
}

func uniqueBlind(values []Blindness) []Blindness {
	seen := make(map[string]bool)
	out := make([]Blindness, 0, len(values))
	for _, value := range values {
		key := value.Context + "\x00" + value.Reason
		if !seen[key] {
			seen[key] = true
			out = append(out, value)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Context != out[j].Context {
			return out[i].Context < out[j].Context
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
