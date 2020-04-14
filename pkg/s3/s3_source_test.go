/*
Copyright 2020 Backup Operator Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package s3

import (
	"bytes"

	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/kubism-io/backup-operator/pkg/util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("S3Source", func() {
	It("should read s3 key to buffer", func() {
		data := []byte("temporarycontent")
		bucket := "bucketa"
		key := "keya"
		src, err := NewS3Source(endpoint, accessKeyID, secretAccessKey, false, bucket, key)
		Expect(err).ToNot(HaveOccurred())
		Expect(src).ToNot(BeNil())
		_, err = src.Client.PutObject(&s3.PutObjectInput{
			Body:   bytes.NewReader(data),
			Bucket: &bucket,
			Key:    &key,
		})
		Expect(err).ToNot(HaveOccurred())
		dst, _ := util.NewBufferDestination()
		err = src.Stream(dst)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst.Data[key]).Should(Equal(data))
	})
})
