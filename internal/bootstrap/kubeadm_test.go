package bootstrap

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	xoclient "github.com/vatesfr/xenorchestra-go-sdk/client"
	xok8scommon "github.com/vatesfr/xenorchestra-k8s-common"
	k8smocks "github.com/vatesfr/xenorchestra-k8s-common/mocks"
)

var _ = Describe("MergeSSHKeysIntoCloudConfig", func() {
	It("leaves cloud-config unchanged when no keys are given", func() {
		input := "#cloud-config\nssh_authorized_keys:\n  - existing-key\n"
		out, err := MergeSSHKeysIntoCloudConfig(input, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal(input))
	})

	It("adds new keys to the cloud-config", func() {
		out, err := MergeSSHKeysIntoCloudConfig(
			"#cloud-config\nssh_authorized_keys:\n  - existing-key\n",
			[]string{"new-key"},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("new-key"))
		Expect(out).To(ContainSubstring("existing-key"))
	})

	It("deduplicates keys already present in the config", func() {
		out, err := MergeSSHKeysIntoCloudConfig(
			"#cloud-config\nssh_authorized_keys:\n  - dup-key\n",
			[]string{"dup-key", "new-key"},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("- dup-key"))
		count := 0
		for i := 0; i <= len(out)-len("- dup-key"); i++ {
			if out[i:i+len("- dup-key")] == "- dup-key" {
				count++
			}
		}
		Expect(count).To(Equal(1))
	})

	It("creates ssh_authorized_keys from scratch when missing", func() {
		out, err := MergeSSHKeysIntoCloudConfig("#cloud-config\n", []string{"key-a"})
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("key-a"))
	})

	It("returns an error on invalid YAML", func() {
		_, err := MergeSSHKeysIntoCloudConfig("{{{invalid", []string{"key"})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("BuildKubeadmCloudConfig", func() {
	var (
		ctrl    *gomock.Controller
		mockV1  *MockXOClient
		mockLib *k8smocks.MockLibrary
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockV1 = NewMockXOClient(ctrl)
		mockLib = k8smocks.NewMockLibrary(ctrl)
		mockLib.EXPECT().V1Client().Return(mockV1).AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("returns bootstrap data as-is when injectSSHKeys is false", func() {
		out, err := BuildKubeadmCloudConfig(context.Background(),
			&xok8scommon.XoClient{Client: mockLib},
			[]byte("#cloud-config\noriginal-data\n"), false)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("#cloud-config\noriginal-data\n"))
	})

	It("returns empty string when injectSSHKeys is false and no bootstrap data", func() {
		out, err := BuildKubeadmCloudConfig(context.Background(),
			&xok8scommon.XoClient{Client: mockLib},
			nil, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal(""))
	})

	It("merges SSH keys into the cloud-config", func() {
		mockV1.EXPECT().
			GetCurrentUser().
			Return(&xoclient.User{
				Preferences: xoclient.Preferences{
					SshKeys: []xoclient.SshKey{{Key: "ssh-rsa AAA... user@host"}},
				},
			}, nil)

		out, err := BuildKubeadmCloudConfig(context.Background(),
			&xok8scommon.XoClient{Client: mockLib},
			[]byte("#cloud-config\n"), true)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("ssh-rsa"))
	})

	It("generates a minimal cloud-config when no bootstrap data", func() {
		mockV1.EXPECT().
			GetCurrentUser().
			Return(&xoclient.User{
				Preferences: xoclient.Preferences{
					SshKeys: []xoclient.SshKey{{Key: "ssh-ed25519 abc... test@test"}},
				},
			}, nil)

		out, err := BuildKubeadmCloudConfig(context.Background(),
			&xok8scommon.XoClient{Client: mockLib},
			nil, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("#cloud-config"))
		Expect(out).To(ContainSubstring("ssh-ed25519"))
	})

	It("returns an error when V1Client is nil", func() {
		mockLib2 := k8smocks.NewMockLibrary(ctrl)
		mockLib2.EXPECT().V1Client().Return(nil)

		_, err := BuildKubeadmCloudConfig(context.Background(),
			&xok8scommon.XoClient{Client: mockLib2},
			nil, true)
		Expect(err).To(HaveOccurred())
	})
})
