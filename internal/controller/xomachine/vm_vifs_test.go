package xomachine

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/vatesfr/xenorchestra-go-sdk/pkg/payloads"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ipamv1 "sigs.k8s.io/cluster-api/api/ipam/v1beta2"

	infrastructurev1beta2 "github.com/vatesfr/cluster-api-provider-vates/api/v1beta2"
)

func TestAddVIFsToParamsMAC(t *testing.T) {
	networks := []infrastructurev1beta2.Network{
		{NetworkID: "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"},
		{NetworkID: "2b3c4d5e-6f7a-4b8c-9d0e-1f2a3b4c5d6e"},
	}
	machine := func(pools ...ipamv1.IPPoolReference) *infrastructurev1beta2.XOMachine {
		return &infrastructurev1beta2.XOMachine{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vates-mp-2", UID: "0b7c5a3e-2f4d-4e8a-9c1b-6d2e3f4a5b6c"},
			Spec: infrastructurev1beta2.XOMachineSpec{
				NetworkConfig: &infrastructurev1beta2.NetworkConfig{Networks: networks, AddressesFromPools: pools},
			},
		}
	}

	t.Run("the first VIF of an IPAM machine carries the MAC its netplan matches", func(t *testing.T) {
		m := machine(ipamv1.IPPoolReference{APIGroup: "ipam.cluster.x-k8s.io", Kind: "InClusterIPPool", Name: "pool"})
		params := &payloads.CreateVMParams{}
		addVIFsToParams(context.Background(), m, nil, params, uuid.Nil)

		if len(params.VIFs) != 2 {
			t.Fatalf("got %d VIFs, want 2", len(params.VIFs))
		}
		if params.VIFs[0].MAC == nil || *params.VIFs[0].MAC != NodeMAC(m) {
			t.Errorf("first VIF MAC = %v, want %q", params.VIFs[0].MAC, NodeMAC(m))
		}
		if params.VIFs[1].MAC != nil {
			t.Errorf("second VIF MAC = %q, want none (XO picks it)", *params.VIFs[1].MAC)
		}
	})

	t.Run("a machine without IPAM lets XO pick every MAC", func(t *testing.T) {
		params := &payloads.CreateVMParams{}
		addVIFsToParams(context.Background(), machine(), nil, params, uuid.Nil)

		for i, vif := range params.VIFs {
			if vif.MAC != nil {
				t.Errorf("VIF %d MAC = %q, want none", i, *vif.MAC)
			}
		}
	})
}
