package vatesmachine

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestVatesmachine(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Vatesmachine Suite")
}
