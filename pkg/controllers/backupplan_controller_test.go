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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	backupv1alpha1 "github.com/kubism/backup-operator/api/v1alpha1"
	"golang.org/x/sync/errgroup"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

const (
	accessKeyID     = "TESTACCESSKEY"
	secretAccessKey = "TESTSECRETKEY"
)

// Add api types to test here
var planTypes = [2]backupv1alpha1.BackupPlan{
	&backupv1alpha1.ConsulBackupPlan{},
	&backupv1alpha1.MongoDBBackupPlan{},
}

type CreateNewBackupPlanFunc = func(namespace string) backupv1alpha1.BackupPlan

// Add function to create api types to test here
var createTypeFuncs = map[string]CreateNewBackupPlanFunc{
	backupv1alpha1.ConsulBackupPlanKind: func(namespace string) backupv1alpha1.BackupPlan {
		return newConsulBackupPlan(namespace)
	},
	backupv1alpha1.MongoDBBackupPlanKind: func(namespace string) backupv1alpha1.BackupPlan {
		return newMongoDBBackupPlan(namespace)
	},
}

type UpdateMongoDBBackupPlanFunc = func(spec *backupv1alpha1.MongoDBBackupPlan)
type UpdateConsulBackupPlanFunc = func(spec *backupv1alpha1.ConsulBackupPlan)

func newObjectMeta(namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace: namespace,
		Name:      newTestName(),
	}
}

func newBackupPlanSpec(namespace string) backupv1alpha1.BackupPlanSpec {
	return backupv1alpha1.BackupPlanSpec{
		Schedule:              "* * * * *",
		ActiveDeadlineSeconds: 3600,
		Retention:             2,
		Destination: &backupv1alpha1.Destination{
			S3: &backupv1alpha1.S3{
				Endpoint:        "localhost:8000",
				Bucket:          "test",
				UseSSL:          false,
				AccessKeyID:     accessKeyID,
				SecretAccessKey: secretAccessKey,
				PartSize:        5242880,
			},
		},
		Pushgateway: &backupv1alpha1.Pushgateway{},
	}
}

func newConsulBackupPlan(namespace string, updates ...UpdateConsulBackupPlanFunc) backupv1alpha1.BackupPlan {
	plan := &backupv1alpha1.ConsulBackupPlan{
		ObjectMeta: newObjectMeta(namespace),
		Spec: backupv1alpha1.ConsulBackupPlanSpec{
			BackupPlanSpec: newBackupPlanSpec(namespace),
			Address:        "localhost:27017",
		},
	}
	for _, f := range updates {
		f(plan)
	}
	return plan
}

func newMongoDBBackupPlan(namespace string, updates ...UpdateMongoDBBackupPlanFunc) backupv1alpha1.BackupPlan {
	plan := &backupv1alpha1.MongoDBBackupPlan{
		ObjectMeta: newObjectMeta(namespace),
		Spec: backupv1alpha1.MongoDBBackupPlanSpec{
			BackupPlanSpec: newBackupPlanSpec(namespace),
			URI:            "mongodb://localhost:27017",
		},
	}
	for _, f := range updates {
		f(plan)
	}
	return plan
}

func mustCreateNewMongoDBBackupPlan(namespace string, updates ...UpdateMongoDBBackupPlanFunc) backupv1alpha1.BackupPlan {
	plan := newMongoDBBackupPlan(namespace, updates...)
	Expect(k8sClient.Create(context.Background(), plan)).Should(Succeed())
	return plan
}

func mustCreateNewBackupPlan(planType backupv1alpha1.BackupPlan, namespace string) backupv1alpha1.BackupPlan {
	f := createTypeFuncs[planType.GetKind()]
	plan := f(namespace)
	Expect(k8sClient.Create(context.Background(), plan)).Should(Succeed())
	return plan
}

// General backup reconciler tests
var _ = Describe("BackupPlanReconciler", func() {
	ctx := context.Background()
	namespace := ""

	BeforeEach(func() {
		namespace = mustCreateNamespace()
	})
	AfterEach(func() {
		mustDeleteNamespace(namespace)
	})

	It("can create BackupPlans", func() {
		Context("with missing data", func() {
			for _, planType := range planTypes {
				Expect(k8sClient.Create(ctx, planType.New())).ShouldNot(Succeed())
			}
		})
		Context("with valid data", func() {
			for _, planType := range planTypes {
				plan := mustCreateNewBackupPlan(planType, namespace)
				defer mustRemoveFinalizers(plan)
			}
		})
	})
	It("can process BackupPlans", func() {
		Context("which are just created", func() {
			for _, planType := range planTypes {
				plan := mustCreateNewBackupPlan(planType, namespace)
				defer mustRemoveFinalizers(plan)
				res := mustReconcile(plan)
				Expect(res.Requeue).To(Equal(false))
			}
		})
		Context("which were deleted", func() {
			for _, planType := range planTypes {
				plan := mustCreateNewBackupPlan(planType, namespace)
				defer func() {
					// If this test fails, we need to make sure the finalizers are removed
					if err := k8sClient.Get(ctx, namespacedName(plan), plan); err == nil {
						mustRemoveFinalizers(plan)
					}
				}()
				res := mustReconcile(plan)
				Expect(res.Requeue).To(Equal(false))
				Expect(k8sClient.Delete(ctx, plan)).Should(Succeed())
				Expect(k8sClient.Get(ctx, namespacedName(plan), plan)).Should(Succeed())
				res = mustReconcile(plan)
				Expect(res.Requeue).To(Equal(false))
				// Check if the owned resources were freed
				var secret corev1.Secret
				Expect(client.IgnoreNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Namespace: plan.GetStatus().Secret.Namespace,
					Name:      plan.GetStatus().Secret.Name,
				}, &secret))).Should(Succeed())
				var cronJob batchv1beta1.CronJob
				Expect(client.IgnoreNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Namespace: plan.GetStatus().CronJob.Namespace,
					Name:      plan.GetStatus().CronJob.Name,
				}, &cronJob))).Should(Succeed())
			}
		})
	})
	DescribeTable("can process BackupPlans multiple times",
		func(count int) {
			for _, planType := range planTypes {
				plan := mustCreateNewBackupPlan(planType, namespace)
				defer mustRemoveFinalizers(plan)
				for i := 0; i < count; i++ {
					res := mustReconcile(plan)
					Expect(res.Requeue).To(Equal(false))
				}
			}
		},
		Entry("twice", 2),
		Entry("three times", 3),
		Entry("five times", 5),
	)
	It("creates relevant Secret", func() {
		for _, planType := range planTypes {
			plan := mustCreateNewBackupPlan(planType, namespace)
			defer mustRemoveFinalizers(plan)
			res := mustReconcile(plan)
			Expect(res.Requeue).To(Equal(false))
			Expect(k8sClient.Get(ctx, namespacedName(plan), plan)).Should(Succeed())
			var secret corev1.Secret
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: plan.GetStatus().Secret.Namespace,
				Name:      plan.GetStatus().Secret.Name,
			}, &secret)).Should(Succeed())
			Expect(secret.Data).NotTo(BeNil())
			raw, ok := secret.Data[secretFieldName]
			Expect(ok).To(Equal(true))
			content := plan.New()
			Expect(json.Unmarshal(raw, &content)).Should(Succeed())
			Expect(content.GetSpec()).To(Equal(plan.GetSpec()))
		}
	})
	It("creates relevant CronJob", func() {
		for _, planType := range planTypes {
			plan := mustCreateNewBackupPlan(planType, namespace)
			defer mustRemoveFinalizers(plan)
			res := mustReconcile(plan)
			Expect(res.Requeue).To(Equal(false))
			Expect(k8sClient.Get(ctx, namespacedName(plan), plan)).Should(Succeed())
			var cronJob batchv1beta1.CronJob
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: plan.GetStatus().CronJob.Namespace,
				Name:      plan.GetStatus().CronJob.Name,
			}, &cronJob)).Should(Succeed())
		}
	})
})

// MongoDB specific tests
var _ = Describe("MongoDBBackupPlanReconciler", func() {
	ctx := context.Background()
	namespace := ""

	BeforeEach(func() {
		namespace = mustCreateNamespace()
	})
	AfterEach(func() {
		mustDeleteNamespace(namespace)
	})

	It("works end-to-end", func() {
		if !shouldRunLongTests {
			Skip("TEST_LONG not set")
		}
		g, _ := errgroup.WithContext(ctx)
		g.Go(func() error {
			return helm.Install(namespace, "src", "bitnami/mongodb")
		})
		g.Go(func() error {
			return helm.Install(namespace, "dst", "stable/minio", "--set", fmt.Sprintf("accessKey=%s,secretKey=%s,readinessProbe.initialDelaySeconds=10", accessKeyID, secretAccessKey))
		})
		g.Go(func() error {
			return helm.Install(namespace, "mon", "stable/prometheus-pushgateway")
		})
		g.Go(func() error {
			return helm.Install(namespace, "op", "../../charts/backup-operator")
		})
		g.Go(func() error {
			return kind.LoadDockerImage(workerImage)
		})
		Expect(g.Wait()).Should(Succeed())
		defer func() {
			_ = helm.Uninstall(namespace, "op") // Make sure it is gone before other tests
		}()
		var mongodbSecret corev1.Secret
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      "src-mongodb",
		}, &mongodbSecret)).Should(Succeed())
		plan := mustCreateNewMongoDBBackupPlan(namespace, func(p *backupv1alpha1.MongoDBBackupPlan) {
			p.Spec.Env = []corev1.EnvVar{
				{
					Name: "MONGODB_ROOT_PASSWORD",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: mongodbSecret.ObjectMeta.Name,
							},
							Key: "mongodb-root-password",
						},
					},
				},
			}
			p.Spec.URI = "mongodb://root:$MONGODB_ROOT_PASSWORD@src-mongodb:27017/admin"
			p.Spec.Destination.S3.Endpoint = "http://dst-minio:9000"
			p.Spec.Pushgateway.URL = "mon-prometheus-pushgateway:9091"
		})
		defer mustRemoveFinalizers(plan)
		// res := mustReconcile(plan)
		// Expect(res.Requeue).To(Equal(false))
		reconciled := false
		for !reconciled {
			Expect(k8sClient.Get(ctx, namespacedName(plan), plan)).Should(Succeed())
			if plan.GetStatus().CronJob != nil {
				reconciled = true
			}
		}
		var cronJob batchv1beta1.CronJob
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Namespace: plan.GetStatus().CronJob.Namespace,
			Name:      plan.GetStatus().CronJob.Name,
		}, &cronJob)).Should(Succeed())
		spawned := false
		for !spawned {
			Expect(k8sClient.Get(ctx, namespacedName(&cronJob), &cronJob)).Should(Succeed())
			if len(cronJob.Status.Active) > 0 {
				spawned = true
			}
		}
		var job batchv1.Job
		job.ObjectMeta.Name = cronJob.Status.Active[0].Name
		job.ObjectMeta.Namespace = cronJob.Status.Active[0].Namespace
		done := false
		for !done {
			Expect(k8sClient.Get(ctx, namespacedName(&job), &job)).Should(Succeed())
			Expect(job.Status.Failed).Should(BeNumerically("==", 0))
			if job.Status.Succeeded == 1 {
				done = true
			}
		}
		// TODO: test retention?
		var testjob batchv1.Job
		testjob.ObjectMeta.Name = "test"
		testjob.ObjectMeta.Namespace = namespace
		activeDeadlineSeconds := (int64)(60)
		testjob.Spec.ActiveDeadlineSeconds = &activeDeadlineSeconds
		testjob.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
		testjob.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:  "test",
				Image: "minio/mc",
				Command: []string{"/bin/ash", "-c", fmt.Sprintf(`
set -euo pipefail
mc config host add dst http://dst-minio:9000 %s %s
count=$(mc ls dst/test/%s/%s | wc -l)
sleep 10
if [ "$count" -gt "0" ]; then
  echo "$count objects found"
else
  echo "no objects found"
  exit 1
fi
apk add --update curl jq
app=$(curl -X GET http://mon-prometheus-pushgateway:9091/api/v1/metrics | jq -r ".data[0].backup_last_success_timestamp_seconds.metrics[0].labels.app")
if [ "$app" = "%s" ]; then
  echo "expected metrics exist"
else
  echo "unexpected app label: $app"
  exit 2
fi
				`, accessKeyID, secretAccessKey, namespace, plan.GetObjectMeta().Name, "mongodb")},
			},
		}
		Expect(k8sClient.Create(ctx, &testjob)).Should(Succeed())
		done = false
		for !done {
			Expect(k8sClient.Get(ctx, namespacedName(&testjob), &testjob)).Should(Succeed())
			Expect(testjob.Status.Failed).Should(BeNumerically("==", 0))
			if testjob.Status.Succeeded == 1 {
				done = true
			}
		}
		Expect(k8sClient.Delete(ctx, plan)).Should(Succeed())
	})
})
