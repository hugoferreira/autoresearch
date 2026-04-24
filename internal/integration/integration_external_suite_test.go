package integration_test

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
)

func TestIntegrationTestSuite(t *testing.T) {
	ginkgo.RunSpecs(t, "Integration Suite")
}
