package guestfs_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestGuestfs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Guestfs Suite")
}
