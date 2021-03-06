package blobstore_test

import (
	. "github.com/cloudfoundry-incubator/blobstore-backup-restore"

	"os"

	"os/exec"

	"bytes"
	"encoding/json"

	"io/ioutil"

	"fmt"

	"strconv"
	"time"

	"strings"

	"io"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("S3Bucket", func() {
	var bucketObjectUnderTest S3Bucket
	var creds S3AccessKey

	var firstVersionOfTest1 string
	var secondVersionOfTest1 string
	var thirdVersionOfTest1 string
	var firstVersionOfTest2 string
	var deletedVersionOfTest2 string

	RunBucketTests := func(mainRegion, secondaryRegion, endpoint, accessKey, secretKey string) {
		var mainBucketName string
		BeforeEach(func() {
			mainBucketName = "sdk-integration-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
			createBucket(mainRegion, mainBucketName, endpoint, accessKey, secretKey)
			enableBucketVersioning(mainBucketName, endpoint, accessKey, secretKey)
			firstVersionOfTest1 = uploadFile(mainBucketName, endpoint, "test-1", "TEST-1-A", accessKey, secretKey)
			secondVersionOfTest1 = uploadFile(mainBucketName, endpoint, "test-1", "TEST-1-B", accessKey, secretKey)
			thirdVersionOfTest1 = uploadFile(mainBucketName, endpoint, "test-1", "TEST-1-C", accessKey, secretKey)
			firstVersionOfTest2 = uploadFile(mainBucketName, endpoint, "test-2", "TEST-2-A", accessKey, secretKey)
			deletedVersionOfTest2 = deleteFile(mainBucketName, endpoint, "test-2", accessKey, secretKey)
		})

		AfterEach(func() {
			deleteAllVersions(mainRegion, mainBucketName, endpoint, accessKey, secretKey)
			deleteBucket(mainBucketName, endpoint, accessKey, secretKey)
		})

		JustBeforeEach(func() {
			bucketObjectUnderTest = NewS3Bucket("aws", mainBucketName, mainRegion, endpoint, creds)
		})

		Describe("Versions", func() {
			var versions []Version
			var err error

			JustBeforeEach(func() {
				versions, err = bucketObjectUnderTest.Versions()
			})

			Context("when retrieving versions succeeds", func() {
				BeforeEach(func() {
					creds = S3AccessKey{
						Id:     accessKey,
						Secret: secretKey,
					}
				})

				It("returns a list of all versions in the bucket", func() {
					Expect(err).NotTo(HaveOccurred())
					Expect(versions).To(ConsistOf(
						Version{Id: firstVersionOfTest1, Key: "test-1", IsLatest: false},
						Version{Id: secondVersionOfTest1, Key: "test-1", IsLatest: false},
						Version{Id: thirdVersionOfTest1, Key: "test-1", IsLatest: true},
						Version{Id: firstVersionOfTest2, Key: "test-2", IsLatest: false},
					))
				})
			})

			Context("when retrieving versions fails", func() {
				BeforeEach(func() {
					creds = S3AccessKey{}
				})

				It("returns the error", func() {
					Expect(versions).To(BeNil())
					Expect(err).To(MatchError(ContainSubstring("An error occurred")))
				})
			})
		})

		Describe("CopyVersions from same bucket", func() {
			var err error

			BeforeEach(func() {
				uploadFile(mainBucketName, endpoint, "test-3", "TEST-3-A", accessKey, secretKey)
			})

			JustBeforeEach(func() {
				err = bucketObjectUnderTest.CopyVersions(mainRegion, mainBucketName, []BlobVersion{
					{BlobKey: "test-1", Id: secondVersionOfTest1},
					{BlobKey: "test-2", Id: firstVersionOfTest2},
				})
			})

			Context("when putting versions succeeds", func() {
				BeforeEach(func() {
					creds = S3AccessKey{
						Id:     accessKey,
						Secret: secretKey,
					}
				})

				It("restores the versions to the specified ones and does not delete blobs that are not specified from the bucket", func() {
					Expect(err).NotTo(HaveOccurred())

					Expect(listFiles(mainBucketName, endpoint, accessKey, secretKey)).To(ConsistOf("test-1", "test-2", "test-3"))
					Expect(getFileContents(mainBucketName, endpoint, "test-1", accessKey, secretKey)).To(Equal("TEST-1-B"))
					Expect(getFileContents(mainBucketName, endpoint, "test-2", accessKey, secretKey)).To(Equal("TEST-2-A"))
					Expect(getFileContents(mainBucketName, endpoint, "test-3", accessKey, secretKey)).To(Equal("TEST-3-A"))
				})
			})

			Context("when putting versions fails", func() {
				BeforeEach(func() {
					creds = S3AccessKey{}
				})

				It("errors", func() {
					Expect(err).To(HaveOccurred())
				})
			})
		})

		Describe("CopyVersions from different bucket", func() {

			var secondaryBucketName string
			var versionOfFileWhichWasSubsequentlyDeleted, versionOfFileToBeRestored string
			var err error

			BeforeEach(func() {
				creds = S3AccessKey{
					Id:     accessKey,
					Secret: secretKey,
				}
				deleteAllVersions(mainRegion, mainBucketName, endpoint, accessKey, secretKey)
				secondaryBucketName = "sdk-integration-test-secondary" + strconv.FormatInt(time.Now().UnixNano(), 10)
				createBucket(secondaryRegion, secondaryBucketName, endpoint, accessKey, secretKey)
				enableBucketVersioning(secondaryBucketName, endpoint, accessKey, secretKey)
				versionOfFileToBeRestored = uploadFile(secondaryBucketName, endpoint, "file-to-restore", "whatever", accessKey, secretKey)
				versionOfFileWhichWasSubsequentlyDeleted = uploadFile(secondaryBucketName, endpoint, "deleted-file-to-restore", "whatever", accessKey, secretKey)
				deleteFile(secondaryBucketName, endpoint, "deleted-file-to-restore", accessKey, secretKey)
				uploadFile(mainBucketName, endpoint, "file-to-be-destroyed-by-restore", "whatever", accessKey, secretKey)
			})

			JustBeforeEach(func() {
				err = bucketObjectUnderTest.CopyVersions(secondaryRegion, secondaryBucketName,
					[]BlobVersion{
						{BlobKey: "file-to-restore", Id: versionOfFileToBeRestored},
						{BlobKey: "deleted-file-to-restore", Id: versionOfFileWhichWasSubsequentlyDeleted},
					})
			})

			It("restores files from the secondary to the main bucket and does not delete pre-existing blobs", func() {
				Expect(err).NotTo(HaveOccurred())

				Expect(listFiles(mainBucketName, endpoint, accessKey, secretKey)).To(
					ConsistOf("file-to-restore", "deleted-file-to-restore", "file-to-be-destroyed-by-restore"),
				)
			})

			AfterEach(func() {
				deleteAllVersions(secondaryRegion, secondaryBucketName, endpoint, accessKey, secretKey)
				deleteBucket(secondaryBucketName, endpoint, accessKey, secretKey)
			})
		})

	}

	Describe("AWS S3 bucket", func() {
		RunBucketTests(
			"eu-west-1",
			"us-west-1",
			"",
			os.Getenv("TEST_AWS_ACCESS_KEY_ID"),
			os.Getenv("TEST_AWS_SECRET_ACCESS_KEY"),
		)
	})

	Describe("ECS S3-compatible bucket", func() {
		RunBucketTests(
			"eu-west-1",
			"us-east-1",
			"https://object.ecstestdrive.com",
			os.Getenv("TEST_ECS_ACCESS_KEY_ID"),
			os.Getenv("TEST_ECS_SECRET_ACCESS_KEY"),
		)
	})

	Describe("Empty AWS S3 bucket", func() {
		var region string
		var bucketName string
		var endpoint string

		BeforeEach(func() {
			region = "eu-west-1"
			bucketName = "bbr-integration-test-bucket-empty"
			endpoint = ""

			creds = S3AccessKey{
				Id:     os.Getenv("TEST_AWS_ACCESS_KEY_ID"),
				Secret: os.Getenv("TEST_AWS_SECRET_ACCESS_KEY"),
			}

			deleteAllVersions(region, bucketName, endpoint, creds.Id, creds.Secret)
			bucketObjectUnderTest = NewS3Bucket("aws", bucketName, region, endpoint, creds)
		})

		Context("when backup an empty bucket", func() {
			It("does not fail", func() {
				_, err := bucketObjectUnderTest.Versions()
				Expect(err).NotTo(HaveOccurred())
			})
		})
		Context("when restore from an empty bucket", func() {
			It("does not fail", func() {
				err := bucketObjectUnderTest.CopyVersions(region, bucketName, []BlobVersion{})
				Expect(err).NotTo(HaveOccurred())

			})
		})
	})
})

func listFiles(bucket, endpoint string, accessKey string, secretKey string) []string {

	baseCmd := constructBaseCmd(endpoint)
	baseCmd = append(baseCmd, "s3api",
		"list-objects",
		"--bucket", bucket)

	outputBuffer := runAwsCommand(accessKey, secretKey, baseCmd)

	var response ListResponse
	json.Unmarshal(outputBuffer.Bytes(), &response)

	keys := []string{}
	for _, entry := range response.Contents {
		keys = append(keys, entry.Key)
	}

	return keys
}

func constructBaseCmd(endpoint string) []string {
	if endpoint != "" {
		return []string{"--endpoint", endpoint}
	}
	return []string{}
}

func getFileContents(bucket, endpoint, key string, accessKey string, secretKey string) string {

	baseCmd := constructBaseCmd(endpoint)
	baseCmd = append(baseCmd, "s3",
		"cp",
		fmt.Sprintf("s3://%s/%s", bucket, key),
		"-")

	outputBuffer := runAwsCommand(accessKey, secretKey, baseCmd)

	return outputBuffer.String()
}

func uploadFile(bucket, endpoint, key, body, accessKey, secretKey string) string {
	bodyFile, _ := ioutil.TempFile("", key)
	bodyFile.WriteString(body)
	bodyFile.Close()

	baseCmd := constructBaseCmd(endpoint)
	baseCmd = append(baseCmd, "s3api",
		"put-object",
		"--bucket", bucket,
		"--key", key,
		"--body", bodyFile.Name())

	outputBuffer := runAwsCommand(accessKey, secretKey, baseCmd)

	var response PutResponse
	json.Unmarshal(outputBuffer.Bytes(), &response)

	return response.VersionId
}

func createBucket(region, bucket, endpoint, accessKey, secretKey string) {

	baseCmd := constructBaseCmd(endpoint)
	baseCmd = append(baseCmd, "s3api",
		"create-bucket",
		"--bucket", bucket,
		"--region", region,
		"--create-bucket-configuration", "LocationConstraint="+region)

	runAwsCommand(accessKey, secretKey, baseCmd)
}

func enableBucketVersioning(bucket, endpoint, accessKey, secretKey string) {
	baseCmd := constructBaseCmd(endpoint)
	baseCmd = append(baseCmd, "s3api",
		"put-bucket-versioning",
		"--bucket", bucket,
		"--versioning-configuration", "Status=Enabled")

	runAwsCommand(accessKey, secretKey, baseCmd)
}

func deleteBucket(bucket, endpoint, accessKey, secretKey string) {

	baseCmd := constructBaseCmd(endpoint)
	baseCmd = append(baseCmd, "s3api",
		"delete-bucket",
		"--bucket", bucket)

	runAwsCommand(accessKey, secretKey, baseCmd)
}

func deleteFile(bucket, endpoint, key, accessKey, secretKey string) string {

	baseCmd := constructBaseCmd(endpoint)
	baseCmd = append(baseCmd, "s3api",
		"delete-object",
		"--bucket", bucket,
		"--key", key)

	outputBuffer := runAwsCommand(accessKey, secretKey, baseCmd)

	var response PutResponse
	json.Unmarshal(outputBuffer.Bytes(), &response)

	return response.VersionId
}
func runAwsCommand(accessKey string, secretKey string, baseCmd []string) *bytes.Buffer {
	outputBuffer := new(bytes.Buffer)
	awsCmd := newAwsCommand(accessKey, secretKey, baseCmd)

	fmt.Fprintf(GinkgoWriter, "Running command: aws %s", strings.Join(baseCmd, " "))
	session, err := gexec.Start(awsCmd, io.MultiWriter(GinkgoWriter, outputBuffer), GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(session, 1*time.Minute).Should(gexec.Exit())
	Expect(session.ExitCode()).To(BeZero())

	return outputBuffer
}

func deleteVersion(bucket, endpoint, key, versionId string, accessKey string, secretKey string) {

	baseCmd := constructBaseCmd(endpoint)
	baseCmd = append(baseCmd, "s3api",
		"delete-object",
		"--bucket", bucket,
		"--key", key,
		"--version-id", versionId)
	runAwsCommand(accessKey, secretKey, baseCmd)
}

func deleteAllVersions(region, bucket, endpoint string, accessKey string, secretKey string) {

	baseCmd := constructBaseCmd(endpoint)
	baseCmd = append(baseCmd, "s3api",
		"list-object-versions",
		"--bucket", bucket)

	outputBuffer := runAwsCommand(accessKey, secretKey, baseCmd)

	var response VersionsResponse
	json.Unmarshal(outputBuffer.Bytes(), &response)

	for _, version := range response.Versions {
		deleteVersion(bucket, endpoint, version.Key, version.VersionId, accessKey, secretKey)
	}

	for _, version := range response.DeleteMarkers {
		deleteVersion(bucket, endpoint, version.Key, version.VersionId, accessKey, secretKey)
	}

}

func newAwsCommand(accessKey string, secretKey string, baseCmd []string) *exec.Cmd {
	awsCmd := exec.Command("aws", baseCmd...)
	awsCmd.Env = append(awsCmd.Env, "AWS_ACCESS_KEY_ID="+accessKey)
	awsCmd.Env = append(awsCmd.Env, "AWS_SECRET_ACCESS_KEY="+secretKey)

	return awsCmd
}

type PutResponse struct {
	VersionId string
}

type VersionsResponse struct {
	Versions      []VersionItem
	DeleteMarkers []VersionItem
}

type VersionItem struct {
	Key       string
	VersionId string
}

type ListResponse struct {
	Contents []ListResponseEntry
}

type ListResponseEntry struct {
	Key string
}
