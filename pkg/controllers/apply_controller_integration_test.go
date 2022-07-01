/*
Copyright 2021 The Kubernetes Authors.

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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

var _ = Describe("Work Reconciler", func() {
	var resourceName string
	var resourceNamespace string
	var workName string
	var workNamespace string
	var configMapA corev1.ConfigMap

	const timeout = time.Second * 30
	const interval = time.Second * 1

	BeforeEach(func() {
		workName = utilrand.String(5)
		workNamespace = utilrand.String(5)
		resourceName = utilrand.String(5)
		resourceNamespace = utilrand.String(5)

		wns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: workNamespace,
			},
		}
		_, err := k8sClient.CoreV1().Namespaces().Create(context.Background(), wns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		rns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourceNamespace,
			},
		}
		_, err = k8sClient.CoreV1().Namespaces().Create(context.Background(), rns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		configMapA = corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: resourceNamespace,
			},
			Data: map[string]string{
				"test": "test",
			},
		}

		work := &workv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workName,
				Namespace: workNamespace,
			},
			Spec: workv1alpha1.WorkSpec{
				Workload: workv1alpha1.WorkloadTemplate{
					Manifests: []workv1alpha1.Manifest{
						{
							RawExtension: runtime.RawExtension{Object: &configMapA},
						},
					},
				},
			},
		}

		_, createWorkErr := workClient.MulticlusterV1alpha1().Works(workNamespace).Create(context.Background(), work, metav1.CreateOptions{})
		Expect(createWorkErr).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// TODO: Ensure that all resources are being deleted.
		err := k8sClient.CoreV1().Namespaces().Delete(context.Background(), workNamespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("receives a work to reconcile", func() {
		It("should verify that the work contains finalizer", func() {
			Eventually(func() bool {
				currentWork, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				return controllerutil.ContainsFinalizer(currentWork, workFinalizer)
			}, timeout, interval).Should(BeTrue())
		})

		It("AppliedWork should have been created", func() {
			Eventually(func() bool {
				appliedWorkObject, err := workClient.MulticlusterV1alpha1().AppliedWorks().Get(context.Background(), workName, metav1.GetOptions{})
				if err == nil {
					return appliedWorkObject.Spec.WorkName == workName
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("manifest is correctly applied", func() {
			Eventually(func() bool {
				resource, err := k8sClient.CoreV1().ConfigMaps(resourceNamespace).Get(context.Background(), resourceName, metav1.GetOptions{})
				if err == nil {
					return resource.GetName() == resourceName
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("manifest condition should be correctly saved", func() {
			Eventually(func() bool {
				currentWork, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
				if err == nil {
					if len(currentWork.Status.ManifestConditions) > 0 {
						return currentWork.Status.ManifestConditions[0].Identifier.Name == resourceName &&
							currentWork.Status.ManifestConditions[0].Identifier.Namespace == resourceNamespace &&
							currentWork.Status.Conditions[0].Status == metav1.ConditionTrue
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("appliedResourceMeta should be correctly saved", func() {
			Eventually(func() bool {
				appliedWorkObject, err := workClient.MulticlusterV1alpha1().AppliedWorks().Get(context.Background(), workName, metav1.GetOptions{})
				if err == nil {
					if len(appliedWorkObject.Status.AppliedResources) > 0 {
						return appliedWorkObject.Status.AppliedResources[0].Name == resourceName &&
							appliedWorkObject.Status.AppliedResources[0].Namespace == resourceNamespace
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("New Work with manifests that depends on each other was reconciled", func() {
		It("All manifests should be created eventually", func() {
			testResourceNamespaceName := utilrand.String(5)
			testResourceNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testResourceNamespaceName,
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
			}
			testCm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      utilrand.String(5),
					Namespace: testResourceNamespaceName,
				},
				Data: map[string]string{
					"test": "test",
				},
			}
			testWork := &workv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      utilrand.String(5),
					Namespace: workNamespace,
				},
				Spec: workv1alpha1.WorkSpec{
					Workload: workv1alpha1.WorkloadTemplate{
						Manifests: []workv1alpha1.Manifest{
							{
								RawExtension: runtime.RawExtension{Object: testCm},
							},
							{
								RawExtension: runtime.RawExtension{Object: testResourceNamespace},
							},
						},
					},
				},
			}
			_, createWorkErr := workClient.MulticlusterV1alpha1().Works(workNamespace).Create(context.Background(), testWork, metav1.CreateOptions{})
			Expect(createWorkErr).ToNot(HaveOccurred())

			Eventually(func() bool {
				ns, err := k8sClient.CoreV1().Namespaces().Get(context.Background(), testResourceNamespaceName, metav1.GetOptions{})
				if err != nil || ns == nil {
					return false
				}
				resource, err := k8sClient.CoreV1().ConfigMaps(testResourceNamespaceName).Get(context.Background(), testCm.Name, metav1.GetOptions{})
				if err == nil {
					return resource.Data["test"] == "test"
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Existing Work is being updated", func() {
		It("Resources regarding Work should be updated", func() {
			cm := corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
				Data: map[string]string{
					"updated_test": "updated_test",
				},
			}

			var originalWork *workv1alpha1.Work
			var err error
			originalWork, err = workClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			newWork := originalWork.DeepCopy()
			newWork.Spec.Workload.Manifests = []workv1alpha1.Manifest{
				{
					RawExtension: runtime.RawExtension{Object: &cm},
				},
			}
			_, err = workClient.MulticlusterV1alpha1().Works(workNamespace).Update(context.Background(), newWork, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Work Status", func() {
				Eventually(func() bool {
					currentWork, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
					if err == nil {
						if len(currentWork.Status.Conditions) > 0 && len(currentWork.Status.ManifestConditions) > 0 {
							return currentWork.Status.ManifestConditions[0].Identifier.Name == cm.Name &&
								currentWork.Status.ManifestConditions[0].Identifier.Namespace == cm.Namespace &&
								currentWork.Status.Conditions[0].Type == ConditionTypeApplied &&
								currentWork.Status.Conditions[0].Status == metav1.ConditionTrue
						}
					}

					return false
				}, timeout, interval).Should(BeTrue())
			})

			By("Applied Manifest", func() {
				Eventually(func() bool {
					configMap, err := k8sClient.CoreV1().ConfigMaps(resourceNamespace).Get(context.Background(), resourceName, metav1.GetOptions{})
					if err == nil {
						return configMap.Data["updated_test"] == "updated_test"
					}
					return false
				}, timeout, interval).Should(BeTrue())
			})
		})
	})
})
