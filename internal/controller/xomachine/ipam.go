package xomachine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ipamv1 "sigs.k8s.io/cluster-api/api/ipam/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

// Address is the address an IPAM provider granted a machine.
type Address struct {
	// IP is the address itself, e.g. "10.30.1.42".
	IP string
	// Prefix is its prefix length, e.g. 24.
	Prefix int
	// Gateway is the network's gateway. Optional.
	Gateway string
}

// ClaimedAddress returns the address the machine's IPAM claim holds, creating
// the claim on the first call.
//
// A nil address with a nil error means the claim exists but no IPAM provider has
// answered yet: the caller waits rather than inventing one.
//
// The claim is named after the machine and owned by it, so it is removed with
// the machine and a replacement starts from a fresh claim. That is what makes
// the address of a replaced machine free again.
func ClaimedAddress(ctx context.Context, c client.Client, xoMachine *infrastructurev1beta2.XOMachine) (*Address, error) {
	if !UsesIPAM(xoMachine) {
		return nil, nil
	}

	key := types.NamespacedName{Namespace: xoMachine.Namespace, Name: xoMachine.Name}
	claim := &ipamv1.IPAddressClaim{}
	switch err := c.Get(ctx, key, claim); {
	case apierrors.IsNotFound(err):
		claim = &ipamv1.IPAddressClaim{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
			Spec: ipamv1.IPAddressClaimSpec{
				ClusterName: xoMachine.Labels[clusterv1.ClusterNameLabel],
				PoolRef:     xoMachine.Spec.NetworkConfig.AddressesFromPools[0],
			},
		}
		if err := controllerutil.SetControllerReference(xoMachine, claim, c.Scheme()); err != nil {
			return nil, fmt.Errorf("owning the IPAddressClaim: %w", err)
		}
		if err := c.Create(ctx, claim); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("creating the IPAddressClaim: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("reading the IPAddressClaim: %w", err)
	}

	if claim.Status.AddressRef.Name == "" {
		return nil, nil
	}
	address := &ipamv1.IPAddress{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: claim.Namespace, Name: claim.Status.AddressRef.Name}, address); err != nil {
		return nil, fmt.Errorf("reading the IPAddress: %w", err)
	}
	prefix := 0
	if address.Spec.Prefix != nil {
		prefix = int(*address.Spec.Prefix)
	}
	return &Address{
		IP:      address.Spec.Address,
		Prefix:  prefix,
		Gateway: address.Spec.Gateway,
	}, nil
}

// UsesIPAM reports whether the machine takes its address from an IPAM pool.
func UsesIPAM(xoMachine *infrastructurev1beta2.XOMachine) bool {
	return xoMachine.Spec.NetworkConfig != nil && len(xoMachine.Spec.NetworkConfig.AddressesFromPools) > 0
}

// NodeMAC returns the MAC address of the machine's first VIF when its address
// comes from an IPAM pool, so that the rendered netplan can match the interface
// by MAC instead of by name.
//
// The name of the interface depends on the node image: eth0, or enX0 for a Xen
// VIF under the kernel's predictable names (Talos since 1.5). A document naming
// an interface the node does not have leaves the node without an address. The
// MAC is the one thing the provider controls on both sides: it sets it on the
// VIF and writes it in the document.
//
// It is derived from the XOMachine's UID, so a create that is retried, or a VM
// that is adopted, gets the same MAC without storing it anywhere. It is a
// locally administered unicast address (02:...): it cannot collide with a
// vendor's, and its 40 hashed bits make a collision between two machines
// negligible.
func NodeMAC(xoMachine *infrastructurev1beta2.XOMachine) string {
	seed := string(xoMachine.UID)
	if seed == "" {
		seed = xoMachine.Namespace + "/" + xoMachine.Name
	}
	sum := sha256.Sum256([]byte(seed))
	mac := net.HardwareAddr{0x02, sum[0], sum[1], sum[2], sum[3], sum[4]}
	return mac.String()
}

// Netplan renders the address as the network configuration the node reads: one
// interface, a static address, and the gateway as both the default route and the
// resolver.
//
// With a MAC, the interface is matched by it and iface is only the document's
// name for it: the node applies the address to whichever interface carries the
// MAC, whatever its name. Without one, the node looks the interface up by iface.
//
// The document is cloud-init's netplan v2, which is what the node's own
// network-config reader expects, what Xen Orchestra writes from the
// infrastructure provider's network_config, and what a KVM seed ISO carries --
// so the node's addressing does not depend on the hypervisor.
//
// The gateway doubles as the resolver because an IPAM IPAddress carries no DNS:
// the pool knows the address, not the network's resolvers, and on the networks
// this provider targets the gateway is the resolver. A cluster that resolves
// elsewhere needs a field for it, not a guess here.
func Netplan(address *Address, iface, mac string) string {
	var b strings.Builder
	b.WriteString("version: 2\nethernets:\n")
	fmt.Fprintf(&b, "  %s:\n", iface)
	if mac != "" {
		b.WriteString("    match:\n")
		fmt.Fprintf(&b, "      macaddress: \"%s\"\n", mac)
	}
	b.WriteString("    dhcp4: false\n")
	b.WriteString("    addresses:\n")
	fmt.Fprintf(&b, "      - %s/%d\n", address.IP, address.Prefix)
	if address.Gateway != "" {
		b.WriteString("    routes:\n")
		b.WriteString("      - to: default\n")
		fmt.Fprintf(&b, "        via: %s\n", address.Gateway)
		b.WriteString("    nameservers:\n")
		b.WriteString("      addresses:\n")
		fmt.Fprintf(&b, "        - %s\n", address.Gateway)
	}
	return b.String()
}
