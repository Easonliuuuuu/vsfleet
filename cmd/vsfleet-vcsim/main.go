// Command vsfleet-vcsim is the small external-process simulator used by the
// integration suite. govmomi v0.56.0 ships simulator as a library, not as an
// installable vcsim module, so this preserves the process boundary without
// adding a second dependency or changing the production read-only packages.
package main

import (
	cryptorand "crypto/rand"
	"crypto/tls"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/vmware/govmomi/simulator"
)

func main() {
	listen := flag.String("l", "127.0.0.1:0", "listen address")
	username := flag.String("username", "user", "simulator username")
	password := flag.String("password", "pass", "simulator password")
	datacenters := flag.Int("dc", 1, "number of datacenters")
	clusters := flag.Int("cluster", 1, "clusters per datacenter")
	hosts := flag.Int("host", 3, "hosts per cluster")
	standaloneHosts := flag.Int("standalone-host", 0, "standalone hosts per datacenter")
	vms := flag.Int("vm", 2, "virtual machines per resource pool")
	datastores := flag.Int("ds", 1, "datastores per datacenter")
	portgroups := flag.Int("pg", 1, "distributed port groups per datacenter")
	apps := flag.Int("app", 0, "vApps per cluster")
	pools := flag.Int("pool", 0, "child resource pools per cluster")
	flag.Parse()

	model := simulator.VPX()
	model.Datacenter = *datacenters
	model.Cluster = *clusters
	model.ClusterHost = *hosts
	model.Host = *standaloneHosts
	model.Machine = *vms
	model.Datastore = *datastores
	model.Portgroup = *portgroups
	model.App = *apps
	model.Pool = *pools
	// simulator's stock model intentionally generates stable UUIDs from object
	// names. Real vCenters do not share VM or instance UUIDs, and the external
	// integration process must model that distinction when two identical flag
	// sets are started side by side.
	model.ServiceContent.About.InstanceUuid = randomUUID()
	if err := model.Create(); err != nil {
		fatalf("create simulator model: %v", err)
	}
	defer model.Remove()
	for _, entity := range model.Map().All("VirtualMachine") {
		vm, ok := entity.(*simulator.VirtualMachine)
		if !ok || vm.Config == nil {
			continue
		}
		vm.Config.Uuid = randomUUID()
		vm.Config.InstanceUuid = randomUUID()
		vm.Summary.Config.Uuid = vm.Config.Uuid
		vm.Summary.Config.InstanceUuid = vm.Config.InstanceUuid
	}

	model.Service.Listen = &url.URL{Host: *listen, User: url.UserPassword(*username, *password)}
	model.Service.TLS = new(tls.Config)
	server := model.Service.NewServer()
	defer server.Close()

	endpoint := *server.URL
	endpoint.User = url.UserPassword(*username, *password)
	fmt.Printf("export GOVC_URL=%s GOVC_SIM_PID=%d\n", endpoint.String(), os.Getpid())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	signal.Stop(stop)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func randomUUID() string {
	var value [16]byte
	if _, err := cryptorand.Read(value[:]); err != nil {
		fatalf("generate simulator identity: %v", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}
