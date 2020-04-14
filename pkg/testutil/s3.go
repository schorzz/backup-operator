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

package testutil

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/ory/dockertest/v3"
)

func WaitForS3(pool *dockertest.Pool, endpoint, accessKeyID, secretAccessKey string) error {
	return pool.Retry(func() error {
		newSession, err := session.NewSession(&aws.Config{
			Credentials:      credentials.NewStaticCredentials(accessKeyID, secretAccessKey, ""),
			Endpoint:         aws.String(endpoint),
			Region:           aws.String("us-east-1"),
			DisableSSL:       aws.Bool(true),
			S3ForcePathStyle: aws.Bool(true),
		})
		if err != nil {
			return err
		}
		client := s3.New(newSession)
		input := &s3.ListBucketsInput{}
		_, err = client.ListBuckets(input)
		if err != nil {
			return err
		}
		return nil
	})
}
