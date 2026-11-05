package report

// Sensitivity classifies what a column can reveal about the source estate.
type Sensitivity string

const (
	ClassNone     Sensitivity = "none"
	ClassName     Sensitivity = "name"
	ClassIP       Sensitivity = "ip"
	ClassPath     Sensitivity = "path"
	ClassID       Sensitivity = "id"
	ClassFreeText Sensitivity = "free-text"
	ClassTopology Sensitivity = "topology"
)

// Pseudonym kinds. A kind is both the token prefix and the namespace an
// equality is preserved in: the same original value under the same kind
// always maps to the same token, in every sheet.
const (
	kindVM         = "vm"
	kindHost       = "host"
	kindCluster    = "cluster"
	kindDatacenter = "dc"
	kindDatastore  = "ds"
	kindNetwork    = "net"
	kindSwitch     = "sw"
	kindContext    = "ctx"
	kindEndpoint   = "ep"
	kindName       = "name"
	kindSnapshot   = "snap"
	kindIP         = "ip"
	kindMAC        = "mac"
	kindPath       = "path"
	kindID         = "id"
	kindUUID       = "uuid"
	kindText       = "text"
	kindDevice     = "dev"
)

// colRule declares how one column is handled when pseudonymization is on.
// kind is empty for a column that is always kept as-is. scrub marks
// system-generated free text that is not hashed but has every known
// pseudonymized value replaced in place. list marks a cell holding several
// values joined by ", " or "; ".
type colRule struct {
	class Sensitivity
	kind  string
	list  bool
	scrub bool
}

func keep() colRule                { return colRule{class: ClassNone} }
func topo() colRule                { return colRule{class: ClassTopology} }
func name(kind string) colRule     { return colRule{class: ClassName, kind: kind} }
func id(kind string) colRule       { return colRule{class: ClassID, kind: kind} }
func nameList(kind string) colRule { return colRule{class: ClassName, kind: kind, list: true} }
func text() colRule                { return colRule{class: ClassFreeText, kind: kindText} }
func scrubbed() colRule            { return colRule{class: ClassFreeText, scrub: true} }

var (
	ipRule   = colRule{class: ClassIP, kind: kindIP, list: true}
	pathRule = colRule{class: ClassPath, kind: kindPath}
)

// columnRules is the default rule for a column header. A header that is not
// here and has no sheet override is an error: the exporter never guesses.
var columnRules = map[string]colRule{
	// Identity, location and provenance.
	"VM": name(kindVM), "Host": name(kindHost), "Cluster": name(kindCluster), "Datacenter": name(kindDatacenter),
	"Folder": pathRule, "VM ID": id(kindID), "VM UUID": id(kindUUID), "SMBIOS UUID": id(kindUUID), "Folder ID": id(kindID),
	"VI SDK Server": name(kindEndpoint), "VI SDK UUID": id(kindUUID), "vsfleet Context": name(kindContext),
	"Context": name(kindContext), "Endpoint": name(kindEndpoint), "vCenter ID": id(kindUUID),
	"Object ID": id(kindID), "Annotation": text(), "Description": text(),

	// vInfo / per-VM tabs.
	"Powerstate": keep(), "Template": keep(), "Guest state": keep(), "CPUs": keep(), "Memory": keep(),
	"Primary IP Address": ipRule, "In Use MiB": keep(), "DNS Name": name(kindName), "Connection state": keep(),
	"NICs": keep(), "Disks": keep(), "Total disk capacity MiB": keep(), "Provisioned MiB": keep(), "Unshared MiB": keep(),
	"Consolidation Needed": keep(), "PowerOn": keep(), "Suspend Interval": keep(), "Creation date": keep(),
	"Change Version": keep(), "Fixed Passthru HotPlug": keep(), "min Required EVC Mode Key": keep(), "Latency Sensitivity": keep(),
	"Op Notification Timeout": keep(), "EnableUUID": keep(), "CBT": keep(), "Num Monitors": keep(), "Video Ram KiB": keep(),
	"Boot Required": keep(), "Boot delay": keep(), "Boot retry delay": keep(), "Boot retry enabled": keep(), "Boot BIOS setup": keep(),
	"Reboot PowerOff": keep(), "Customization Info": keep(), "Guest Detailed Data": keep(), "VI SDK Server type": keep(), "VI SDK API Version": keep(), "Log directory": pathRule, "Snapshot directory": pathRule, "Suspend directory": pathRule,
	"Network #1": name(kindNetwork), "Network #2": name(kindNetwork), "Network #3": name(kindNetwork), "Network #4": name(kindNetwork),
	"Network #5": name(kindNetwork), "Network #6": name(kindNetwork), "Network #7": name(kindNetwork), "Network #8": name(kindNetwork),
	"Sockets": keep(), "Cores p/s": keep(), "Memory Reservation Locked To Max": keep(), "vApp": name(kindName), "EFI Secure boot": keep(), "Firmware": keep(), "HW version": keep(), "HW upgrade status": keep(), "HW upgrade policy": keep(),
	"HW target": keep(), "OS according to the configuration file": keep(), "SRM Placeholder": keep(),
	"DAS protection": keep(), "HA Restart Priority": keep(), "HA Isolation Response": keep(), "HA VM Monitoring": keep(),
	"Cluster rule(s)": keep(), "Cluster rule name(s)": nameList(kindName),
	osToolsHeader: keep(), usbOSToolsHeader: keep(),
	"Size MiB": keep(), "Tools": keep(), "Tools Version": keep(), "Tools Version Status": keep(), "Tools Running Status": keep(),
	"Hot Add": keep(), "Hot Remove": keep(), "Numa Hotadd Exposed": keep(), "VM Version": keep(), "Required Version": keep(),
	"Upgrade Policy": keep(), "Sync time": keep(), "Kernel Crash state": keep(), "Operation Ready": keep(),
	"State change support": keep(), "Interactive Guest": keep(),

	// vDisk / vPartition.
	"Disk": keep(), "Disk Key": keep(), "Disk UUID": id(kindUUID), "Capacity MiB": keep(), "Raw": keep(),
	"Disk Mode": keep(), "Sharing mode": keep(), "Thin": keep(), "Eagerly Scrub": keep(), "Split": keep(),
	"Write Through": keep(), "Level": keep(), "Shares": keep(), "Reservation": keep(), "Limit": keep(),
	"Controller": keep(), "Label": keep(), "SCSI Unit #": keep(), "Unit number": keep(), "Unit #": keep(), "Shared Bus": keep(), "Path": pathRule,
	"Raw LUN ID": id(kindID), "Raw Comp. Mode": keep(), "Consumed MiB": keep(), "Free MiB": keep(),
	"Free %": keep(), "Filesystem": keep(),
	// Disk Path lists the guest filesystems on a disk, the same paths
	// vPartition's Disk column names.
	"Disk Path": colRule{class: ClassPath, kind: kindPath, list: true},

	// vNetwork / vCD / vUSB.
	"NIC label": keep(), "Adapter": keep(), "Network": name(kindNetwork), "Connected": keep(), "Starts Connected": keep(),
	"Mac Address": id(kindMAC), "IPv4 Address": ipRule, "IPv6 Address": ipRule,
	"Direct Path IO": keep(), "Device": keep(), "Device Node": keep(), "Device Key": keep(),
	// Device Type names an ISO's datastore path or a host's device.
	"Device Type": text(), "Backing type": keep(), "Backing path": pathRule,
	"Backing device": name(kindDevice), "Backing host": name(kindHost), "Backing datastore ID": id(kindID),
	"Backing object ID": id(kindID), "Use auto detect": keep(), "Controller label": keep(), "Vendor ID": keep(),
	"Product ID": keep(), "Family": keep(), "Speed": keep(), "EHCI enabled": keep(), "Auto connect": keep(), "Bus number": keep(),

	// vSnapshot.
	"Date / time": keep(), "Quiesced": keep(), "State": keep(), "Filename": pathRule, "Size MiB (vmsn)": keep(), "Size MiB (total)": keep(),

	// vSource.
	"OS type": keep(), "API type": keep(), "API version": keep(), "Version": keep(), "Patch level": keep(),
	"Build": keep(), "Fullname": keep(), "Product name": keep(), "Product version": keep(), "Product line": keep(),
	"Vendor": keep(),

	// vHost / vHBA / vNIC / vSwitch / vPort.
	"in Maintenance Mode": keep(), "# Cores": keep(), "CPU usage %": keep(), "# Memory": keep(), "Memory usage %": keep(),
	"# NICs": keep(), "# HBAs": keep(), "# VMs total": keep(), "ESX Version": keep(), "Model": keep(), "Bus": keep(), "Status": keep(), "Driver": keep(),
	"Pci": keep(), "PCI": keep(), "Storage protocol": keep(), "WWNN": id(kindID), "WWPN": id(kindID),
	"iSCSI name": id(kindID), "iSCSI alias": name(kindName), "Type": keep(), "Duplex": keep(),
	"Switch": name(kindSwitch), "# Ports": keep(), "Free Ports": keep(), "MTU": keep(),
	"Uplinks": topo(), "Promiscuous Mode": keep(), "Mac Changes": keep(), "Forged Transmits": keep(),
	"Traffic Shaping": keep(), "Port Group": name(kindNetwork), "VLAN": topo(),
	"CPU Model": keep(), "HT Available": keep(), "HT Active": keep(), "# CPU": keep(), "Cores per CPU": keep(),
	"# VMs": keep(), "VMs per Core": keep(), "# vCPUs": keep(), "vCPUs per Core": keep(), "vRAM": keep(),
	"VMotion support": keep(), "Storage VMotion support": keep(), "Current EVC": keep(), "Max EVC": keep(), "Boot time": keep(),
	"DNS Servers": ipRule, "Domain": name(kindName), "NTP Server(s)": nameList(kindName), "NTPD running": keep(),
	"Time Zone": keep(), "Time Zone Name": keep(), "GMT Offset": keep(), "Serial number": id(kindID),
	"BIOS Vendor": keep(), "BIOS Version": keep(), "BIOS Date": keep(), "Certificate Issuer": text(),
	"Certificate Start Date": keep(), "Certificate Expiry Date": keep(), "Certificate Subject": text(),
	"in Quarantine Mode": keep(), "Memory Tiering Type": keep(), "ATS Heartbeat": keep(), "ATS Locking": keep(),
	"Current CPU power man. policy": keep(), "Supported CPU power man.": keep(), "Host Power Policy": keep(),
	"Domain List": nameList(kindName), "DNS Search Order": nameList(kindName), "Service tag": id(kindID),
	"OEM specific string": text(), "AutoDeploy.MachineIdentity": id(kindID), "WWN": id(kindID),
	"Network Device": keep(), "MAC": id(kindMAC), "Uplink port": topo(), "WakeOn": keep(),

	// dvSwitch / dvPort / vSC_VMK / vMultiPath.
	"Max Ports": keep(), "UUID": id(kindUUID), "Contact": text(), "Contact detail": text(), "Hosts": nameList(kindHost),
	"Uplink ports": topo(), "Link discovery protocol": keep(), "Link discovery operation": keep(), "LACP version": keep(),
	"Port": name(kindNetwork), "Key": id(kindID), "Uplink": topo(), "Allow Promiscuous": keep(),
	"Policy": keep(), "Notify Switch": keep(), "Rolling Order": keep(), "In Traffic Shaping": keep(),
	"Out Traffic Shaping": keep(), "Blocked": keep(), "Auto expand": keep(), "Active Uplink": topo(),
	"Standby Uplink": topo(), "Logical switch UUID": id(kindUUID), "Segment ID": topo(),
	"TSO": keep(), "Netstack": keep(), "DHCP": keep(), "IP Address": ipRule, "Subnet mask": topo(),
	"IP 6 Address": ipRule, "Gateway": ipRule, "IP 6 Gateway": ipRule,
	"Service console": keep(), "LUN": id(kindID), "Device path": pathRule, "Local disk": keep(), "Path count": keep(),
	"Active paths": keep(), "Standby paths": keep(), "Dead paths": keep(), "Disabled paths": keep(), "Working paths": keep(),
	// A LUN's canonical name, serial and display name identify the array
	// behind it; Disk, Address and Display name share the kindID namespace,
	// so a datastore still joins to its LUN.
	"Display name": id(kindID), "Serial #": id(kindID), "Oper. State": keep(), "vStorage": keep(), "Queue depth": keep(), "Revision": keep(),
	"Path 1": keep(), "Path 1 state": keep(), "Path 2": keep(), "Path 2 state": keep(), "Path 3": keep(), "Path 3 state": keep(), "Path 4": keep(), "Path 4 state": keep(),
	"Path 5": keep(), "Path 5 state": keep(), "Path 6": keep(), "Path 6 state": keep(), "Path 7": keep(), "Path 7 state": keep(), "Path 8": keep(), "Path 8 state": keep(),

	// vCluster / vRP / vDatastore.
	"Address": id(kindID), "SIOC enabled": keep(), "SIOC Threshold": keep(), "# Hosts": keep(),
	"Cluster name": name(kindName), "Cluster capacity MiB": keep(), "Cluster free space MiB": keep(), "Block size": keep(), "Max Blocks": keep(),
	"# Extents": keep(), "Major Version": keep(), "VMFS Upgradeable": keep(), "MHA": keep(),
	"NumHosts": keep(), "numEffectiveHosts": keep(), "TotalCpu": keep(), "NumCpuCores": keep(), "TotalMemory": keep(),
	"HA enabled": keep(), "DRS enabled": keep(), "OverallStatus": keep(), "NumCpuThreads": keep(), "Num VMotions": keep(),
	"Effective Cpu": keep(), "Effective Memory": keep(), "DRS default VM behavior": keep(),
	"Failover Level": keep(), "AdmissionControlEnabled": keep(), "Host monitoring": keep(), "HB Datastore Candidate Policy": keep(),
	"Isolation Response": keep(), "Restart Priority": keep(), "Cluster Settings": keep(), "Max Failures": keep(),
	"Max Failure Window": keep(), "Failure Interval": keep(), "Min Up Time": keep(), "VM Monitoring": keep(),
	"DRS vmotion rate": keep(), "DPM enabled": keep(), "DPM default behavior": keep(), "DPM Host Power Action Rate": keep(), "Resource pool": pathRule, "VMs": keep(), "vCPUs": keep(),
	"CPU limit": keep(), "CPU overheadLimit": keep(), "CPU reservation": keep(), "CPU level": keep(), "CPU shares": keep(),
	"CPU expandableReservation": keep(), "Mem Configured": keep(), "Mem limit": keep(), "Mem overheadLimit": keep(),
	"Mem reservation": keep(), "Mem level": keep(), "Mem shares": keep(), "Mem expandableReservation": keep(),
	"Config status": keep(), "Accessible": keep(), "Maintenance mode": keep(), "URL": id(kindID),

	// vFileInfo.
	"Friendly Path Name": pathRule, "File Name": pathRule, "File Type": keep(), "File Size in bytes": keep(),
	"Internal Sort Column": pathRule, "Datastore": name(kindDatastore), "Datastore ID": id(kindID),

	// vHealth.
	"Message": scrubbed(), "Message type": keep(), "Category": keep(), "vsfleet Rule": keep(),
	"Recommendation": scrubbed(), "Evidence": scrubbed(), "Object type": keep(),

	// vsfleetCoverage.
	"Run ID": keep(), "Run label": text(), "Run started": keep(), "Run finished": keep(), "Run status": keep(),
	"Sheet": keep(), "Collection status": keep(), "Item count": keep(), "Error": scrubbed(),

	// vsfleetPerformance.
	"Inventory match": keep(), "Counter": keep(), "Unit": keep(), "Aggregation": keep(), "Window start": keep(),
	"Window end": keep(), "Interval s": keep(), "Expected samples": keep(), "Successful samples": keep(),
	"Missing samples": keep(), "Average": keep(), "Peak": keep(), "P95": keep(), "Reason": scrubbed(),
	"Sizing signal": keep(), "Signal reason": scrubbed(), "Source": keep(),

	// vLicense / vsfleetLicenseAssignment. The key column is already redacted.
	"Labels": text(), "Cost Unit": keep(), "Total": keep(), "Used": keep(), "Expiration Date": keep(),
	"Features": keep(), "License": name(kindName), "Edition key": keep(), "Entity": name(kindName),
	"Entity type": keep(), "Entity ID": id(kindID), "Scope": name(kindName),
}

// sheetRules override columnRules where the same header means something else.
var sheetRules = map[string]colRule{
	"vSource/Name":          keep(),
	"vCluster/Name":         name(kindCluster),
	"vRP/Name":              name(kindName),
	"vDatastore/Name":       name(kindDatastore),
	"vSnapshot/Name":        name(kindSnapshot),
	"vSnapshot/Description": text(),
	"vHealth/Name":          name(kindName),
	"vLicense/Name":         name(kindName),
	"vPartition/Disk":       pathRule,
	"vMultiPath/Disk":       id(kindID),
	"vLicense/Key":          keep(),

	// vsfleetMetadata. Category, attribute and tag names are operator free
	// text and are pseudonymized like annotations; the same name maps to the
	// same token, so grouping by field or value still works. Object IDs share
	// the kindID namespace with "VM ID" and "Object ID" elsewhere, which is
	// what links a row back to its object.
	"vsfleetMetadata/Captured at":     keep(),
	"vsfleetMetadata/Kind":            keep(),
	"vsfleetMetadata/Object name":     name(kindName),
	"vsfleetMetadata/Metadata source": keep(),
	"vsfleetMetadata/Field ID":        id(kindID),
	"vsfleetMetadata/Field":           text(),
	"vsfleetMetadata/Value ID":        id(kindID),
	"vsfleetMetadata/Value":           text(),
	"vsfleetMetadata/Source status":   keep(),
	"vsfleetMetadata/Source error":    scrubbed(),
}

// ruleFor resolves a column's rule and reports whether one exists.
func ruleFor(sheetName, header string) (colRule, bool) {
	if r, ok := sheetRules[sheetName+"/"+header]; ok {
		return r, true
	}
	r, ok := columnRules[header]
	return r, ok
}
