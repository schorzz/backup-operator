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
	"crypto/tls"
	"net/http"
	"path/filepath"
	"sort"

	"github.com/kubism/backup-operator/pkg/backup"
	"github.com/kubism/backup-operator/pkg/logger"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type S3DestinationConf struct {
	Endpoint            string
	AccessKey           string
	SecretKey           string
	EncryptionKey       *string
	EncryptionAlgorithm string
	DisableSSL          bool
	InsecureSkipVerify  bool
	Bucket              string
	Prefix              string
	PartSize            int64
}

func NewS3Destination(conf *S3DestinationConf) (*S3Destination, error) {
	newSession, err := session.NewSession(&aws.Config{
		Credentials:      credentials.NewStaticCredentials(conf.AccessKey, conf.SecretKey, ""),
		Endpoint:         aws.String(conf.Endpoint),
		Region:           aws.String("us-east-1"),
		DisableSSL:       aws.Bool(conf.DisableSSL),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		return nil, err
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: conf.InsecureSkipVerify},
	}
	cl := &http.Client{Transport: tr}
	client := s3.New(newSession, aws.NewConfig().WithHTTPClient(cl))

	// Create bucket, if not exists
	_, err = client.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(conf.Bucket),
	})
	if err != nil { // If bucket already exists ignore error
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() != s3.ErrCodeBucketAlreadyExists && aerr.Code() != s3.ErrCodeBucketAlreadyOwnedByYou {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return &S3Destination{
		Session:             newSession,
		Client:              client,
		EncryptionKey:       conf.EncryptionKey,
		EncryptionAlgorithm: conf.EncryptionAlgorithm,
		Uploader: s3manager.NewUploaderWithClient(client, func(u *s3manager.Uploader) {
			u.PartSize = conf.PartSize
		}),
		Bucket: conf.Bucket,
		Prefix: conf.Prefix,
		log:    logger.WithName("s3dst"),
	}, nil
}

type S3Destination struct {
	Session             *session.Session
	Client              *s3.S3
	EncryptionKey       *string
	EncryptionAlgorithm string
	Uploader            *s3manager.Uploader
	Bucket              string
	Prefix              string
	log                 logger.Logger
}

func (s *S3Destination) Store(obj backup.Object) (int64, error) {
	key := filepath.Join(s.Prefix, obj.ID)
	params := &s3manager.UploadInput{
		Bucket: &s.Bucket,
		Key:    &key,
		Body:   obj.Data,
	}

	if s.EncryptionKey != nil {
		if s.EncryptionAlgorithm == "" {
			params.SSECustomerAlgorithm = aws.String(DefaultEncryptionAlgorithm)
		} else {
			params.SSECustomerAlgorithm = &s.EncryptionAlgorithm
		}
		params.SSECustomerKey = s.EncryptionKey
	}

	s.log.Info("upload starting", "bucket", s.Bucket, "key", key)
	res, err := s.Uploader.Upload(params)
	if err != nil {
		return 0, err
	}
	s.log.Info("upload successful", "result", res)

	headObjectInput := &s3.HeadObjectInput{
		Bucket: &s.Bucket,
		Key:    &key,
	}

	if s.EncryptionKey != nil {
		if s.EncryptionAlgorithm == "" {
			headObjectInput.SSECustomerAlgorithm = aws.String(DefaultEncryptionAlgorithm)
		} else {
			headObjectInput.SSECustomerAlgorithm = &s.EncryptionAlgorithm
		}
		headObjectInput.SSECustomerKey = s.EncryptionKey
	}

	head, err := s.Client.HeadObject(headObjectInput)
	if err != nil {
		return 0, err
	}
	return *head.ContentLength, nil
}

func (s *S3Destination) EnsureRetention(max int) error {
	// NOTE: using V1 list method is intentional as V2 malfunctioned on older ceph s3 installations
	input := &s3.ListObjectsInput{
		Bucket: &s.Bucket,
		Prefix: &s.Prefix,
	}
	objects := sortableObjectSlice{}
	err := s.Client.ListObjectsPages(input,
		func(page *s3.ListObjectsOutput, lastPage bool) bool {
			objects = append(objects, page.Contents...)
			return true
		})
	if err != nil {
		return err
	}
	if len(objects) > max {
		sort.Sort(objects)
		obsolete := objects[max:]
		if len(objects) > 0 {
			for _, obj := range obsolete {
				input := &s3.DeleteObjectInput{
					Bucket: &s.Bucket,
					Key:    obj.Key,
				}
				_, err := s.Client.DeleteObject(input)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type sortableObjectSlice []*s3.Object

func (s sortableObjectSlice) Len() int {
	return len(s)
}

func (s sortableObjectSlice) Less(i, j int) bool {
	if s[i].LastModified == s[j].LastModified {
		return *s[i].Key < *s[j].Key
	}
	return s[i].LastModified.After(*s[j].LastModified)
}

func (s sortableObjectSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
