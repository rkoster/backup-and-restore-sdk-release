package blobstore_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestBlobstoreBackupRestore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BlobstoreBackupRestore Suite")
}
