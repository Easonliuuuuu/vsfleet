package vsphere

import "context"

// ListTemplates returns the VM templates in a vCenter.
//
// vSphere models a template as a virtual machine with config.template set, so
// this shares the VM retrieval rather than duplicating a near-identical type.
func (c *Client) ListTemplates(ctx context.Context) ([]VM, error) {
	all, err := c.listAllVMs(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, vm := range all {
		if vm.IsTemplate {
			out = append(out, vm)
		}
	}
	return out, nil
}
