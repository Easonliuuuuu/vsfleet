package assessment

import "github.com/easonliuuuuu/vsfleet/internal/vsphere"

// DatastoreIdentity returns strong backing identities used to join the same
// datastore across contexts and vCenters. Local datastores intentionally have
// no cross-context identity.
func DatastoreIdentity(datastore vsphere.Datastore) []string {
	return datastore.IdentityKeys()
}
