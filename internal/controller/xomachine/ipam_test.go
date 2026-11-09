package xomachine

import (
	"context"
	"regexp"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ipamv1 "sigs.k8s.io/cluster-api/api/ipam/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

func TestNetplan(t *testing.T) {
	tests := []struct {
		name    string
		address *Address
		iface   string
		mac     string
		want    string
	}{
		{
			name:    "renders an address, its default route and its resolver",
			address: &Address{IP: "10.30.1.42", Prefix: 24, Gateway: "10.30.1.1"},
			iface:   "eth0",
			want: `version: 2
ethernets:
  eth0:
    dhcp4: false
    addresses:
      - 10.30.1.42/24
    routes:
      - to: default
        via: 10.30.1.1
    nameservers:
      addresses:
        - 10.30.1.1
`,
		},
		{
			name:    "renders no route when the pool gave no gateway",
			address: &Address{IP: "10.30.1.42", Prefix: 16},
			iface:   "eth0",
			want: `version: 2
ethernets:
  eth0:
    dhcp4: false
    addresses:
      - 10.30.1.42/16
`,
		},
		{
			name:    "matches the interface by its MAC when given one",
			address: &Address{IP: "10.30.1.42", Prefix: 24, Gateway: "10.30.1.1"},
			iface:   "eth0",
			mac:     "02:aa:bb:cc:dd:ee",
			want: `version: 2
ethernets:
  eth0:
    match:
      macaddress: "02:aa:bb:cc:dd:ee"
    dhcp4: false
    addresses:
      - 10.30.1.42/24
    routes:
      - to: default
        via: 10.30.1.1
    nameservers:
      addresses:
        - 10.30.1.1
`,
		},
		{
			name:    "renders the interface it was given",
			address: &Address{IP: "10.30.1.42", Prefix: 24},
			iface:   "ens3",
			want: `version: 2
ethernets:
  ens3:
    dhcp4: false
    addresses:
      - 10.30.1.42/24
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Netplan(tt.address, tt.iface, tt.mac); got != tt.want {
				t.Errorf("Netplan() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestNodeMAC(t *testing.T) {
	machine := func(namespace, name, uid string) *infrastructurev1beta2.XOMachine {
		return &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID(uid)},
		}
	}
	macPattern := regexp.MustCompile(`^02(:[0-9a-f]{2}){5}$`)

	a := NodeMAC(machine("default", "vates-mp-1", "0b7c5a3e-2f4d-4e8a-9c1b-6d2e3f4a5b6c"))
	if !macPattern.MatchString(a) {
		t.Errorf("NodeMAC() = %q, want a locally administered unicast MAC (02:xx:xx:xx:xx:xx)", a)
	}
	if again := NodeMAC(machine("default", "vates-mp-1", "0b7c5a3e-2f4d-4e8a-9c1b-6d2e3f4a5b6c")); again != a {
		t.Errorf("NodeMAC() = %q then %q for the same machine, want a stable MAC", a, again)
	}
	if other := NodeMAC(machine("default", "vates-mp-1", "9f8e7d6c-5b4a-4392-8170-6f5e4d3c2b1a")); other == a {
		t.Errorf("NodeMAC() = %q for two machines with different UIDs, want different MACs", other)
	}
	if byName := NodeMAC(machine("default", "vates-mp-1", "")); !macPattern.MatchString(byName) {
		t.Errorf("NodeMAC() without a UID = %q, want a MAC derived from the name", byName)
	}
}

func ipamScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		clusterv1.AddToScheme,
		ipamv1.AddToScheme,
		infrastructurev1beta2.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("building the scheme: %v", err)
		}
	}
	return scheme
}

func TestClaimedAddress(t *testing.T) {
	pool := ipamv1.IPPoolReference{
		APIGroup: "ipam.cluster.x-k8s.io",
		Kind:     "InClusterIPPool",
		Name:     "cluster-pool",
	}
	machine := func(pools ...ipamv1.IPPoolReference) *infrastructurev1beta2.XOMachine {
		m := &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vates-mp-2",
				Namespace: "default",
				Labels:    map[string]string{clusterv1.ClusterNameLabel: "vates-mp"},
			},
		}
		if len(pools) > 0 {
			m.Spec.NetworkConfig = &infrastructurev1beta2.NetworkConfig{AddressesFromPools: pools}
		}
		return m
	}

	// answer builds the state an IPAM provider leaves behind once it has
	// answered a claim.
	answer := func(claim *ipamv1.IPAddressClaim) []client.Object {
		claim.Status.AddressRef = ipamv1.IPAddressReference{Name: claim.Name + "-addr"}
		return []client.Object{
			claim,
			&ipamv1.IPAddress{
				ObjectMeta: metav1.ObjectMeta{Name: claim.Name + "-addr", Namespace: claim.Namespace},
				Spec: ipamv1.IPAddressSpec{
					Address: "10.30.1.42",
					Prefix:  ptrTo(int32(24)),
					Gateway: "10.30.1.1",
				},
			},
		}
	}

	tests := []struct {
		name    string
		machine *infrastructurev1beta2.XOMachine
		objects []client.Object
		want    *Address
		wantErr bool
		// wantClaim is whether an IPAddressClaim must exist afterwards.
		wantClaim bool
	}{
		{
			name:    "a machine that asks for no pool claims nothing",
			machine: machine(),
			want:    nil,
		},
		{
			name:      "the first call creates the claim and waits",
			machine:   machine(pool),
			want:      nil,
			wantClaim: true,
		},
		{
			name:    "a claim no provider has answered yet is not an address",
			machine: machine(pool),
			objects: []client.Object{&ipamv1.IPAddressClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "vates-mp-2", Namespace: "default"},
				Spec:       ipamv1.IPAddressClaimSpec{PoolRef: pool},
			}},
			want:      nil,
			wantClaim: true,
		},
		{
			name:    "an answered claim yields the address",
			machine: machine(pool),
			objects: answer(&ipamv1.IPAddressClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "vates-mp-2", Namespace: "default"},
				Spec:       ipamv1.IPAddressClaimSpec{PoolRef: pool},
			}),
			want:      &Address{IP: "10.30.1.42", Prefix: 24, Gateway: "10.30.1.1"},
			wantClaim: true,
		},
		{
			name:    "an answered claim whose address is gone is an error",
			machine: machine(pool),
			objects: []client.Object{&ipamv1.IPAddressClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "vates-mp-2", Namespace: "default"},
				Spec:       ipamv1.IPAddressClaimSpec{PoolRef: pool},
				Status:     ipamv1.IPAddressClaimStatus{AddressRef: ipamv1.IPAddressReference{Name: "gone"}},
			}},
			wantErr:   true,
			wantClaim: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := ipamScheme(t)
			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.machine)
			if len(tt.objects) > 0 {
				builder = builder.WithObjects(tt.objects...)
			}
			c := builder.Build()

			got, err := ClaimedAddress(context.Background(), c, tt.machine)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ClaimedAddress() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ClaimedAddress() unexpected error: %v", err)
			}
			if got == nil || tt.want == nil {
				if got != tt.want {
					t.Fatalf("ClaimedAddress() = %+v, want %+v", got, tt.want)
				}
			} else if *got != *tt.want {
				t.Errorf("ClaimedAddress() = %+v, want %+v", got, tt.want)
			}

			claim := &ipamv1.IPAddressClaim{}
			err = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "vates-mp-2"}, claim)
			switch {
			case tt.wantClaim && err != nil:
				t.Errorf("no IPAddressClaim was created: %v", err)
			case !tt.wantClaim && err == nil:
				t.Errorf("an IPAddressClaim was created but none was expected")
			case !tt.wantClaim && !apierrors.IsNotFound(err):
				t.Errorf("reading the IPAddressClaim: %v", err)
			}
		})
	}
}

func ptrTo[T any](v T) *T { return &v }
