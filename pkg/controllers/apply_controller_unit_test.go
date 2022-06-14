package controllers

import (
	"context"
	json2 "encoding/json"
	"fmt"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	testing2 "k8s.io/client-go/testing"
	"k8s.io/klog/v2"
	"math"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic/fake"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

var (
	fakeDynamicClient = fake.NewSimpleDynamicClient(runtime.NewScheme())
	appliedWork       = &workv1alpha1.AppliedWork{}
	ownerRef          = metav1.OwnerReference{
		APIVersion: workv1alpha1.GroupVersion.String(),
		Kind:       appliedWork.Kind,
		Name:       appliedWork.GetName(),
		UID:        appliedWork.GetUID(),
	}
	testGvr = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "Deployment",
	}
	expectedGvr = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "Deployment",
	}
	emptyGvr       = schema.GroupVersionResource{}
	testDeployment = appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "TestDeployment",
			OwnerReferences: []metav1.OwnerReference{
				ownerRef,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:                nil,
			Selector:                nil,
			Template:                v1.PodTemplateSpec{},
			Strategy:                appsv1.DeploymentStrategy{},
			MinReadySeconds:         5,
			RevisionHistoryLimit:    nil,
			Paused:                  false,
			ProgressDeadlineSeconds: nil,
		},
	}
	testDeploymentWithDifferentOwner = appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "TestDeployment",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: getRandomString(),
					Kind:       getRandomString(),
					Name:       getRandomString(),
					UID:        types.UID(getRandomString()),
				},
			},
		},
	}
	testPod = v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "core/v1",
		},
	}
	testInvalidYaml                        = []byte(getRandomString())
	rawTestDeployment, _                   = json.Marshal(testDeployment)
	rawTestDeploymentWithDifferentOwner, _ = json.Marshal(testDeploymentWithDifferentOwner)
	rawInvalidResource, _                  = json.Marshal(testInvalidYaml)
	rawMissingResource, _                  = json.Marshal(testPod)
	testManifest                           = workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawTestDeployment,
	}}
	testManifestDifferentOwner = workv1alpha1.Manifest{
		RawExtension: runtime.RawExtension{
			Raw: rawTestDeploymentWithDifferentOwner,
		},
	}
	InvalidManifest = workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawInvalidResource,
	}}
	MissingManifest = workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawMissingResource,
	}}
)

// This interface is needed for testMapper abstract class.
type testMapper struct {
	meta.RESTMapper
}

func (m testMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	if gk.Kind == "Deployment" {
		return &meta.RESTMapping{
			Resource:         testGvr,
			GroupVersionKind: testDeployment.GroupVersionKind(),
			Scope:            nil,
		}, nil
	} else {
		return nil, errors.New("test error: mapping does not exist.")
	}
}

func TestApplyManifest(t *testing.T) {
	testDeploymentWithOwnerRef := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "TestDeployment",
			OwnerReferences: []metav1.OwnerReference{
				ownerRef,
			}},
	}
	rawDeploymentWithOwnerRef, _ := json.Marshal(testDeploymentWithOwnerRef)
	testManifestWithOwnerRef := workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawDeploymentWithOwnerRef,
	}}
	failDynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	failDynamicClient.PrependReactor("get", "*", func(action testing2.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("Failed to apply an unstructrued object")
	})

	testCases := map[string]struct {
		reconciler ApplyWorkReconciler
		ml         []workv1alpha1.Manifest
		mcl        []workv1alpha1.ManifestCondition
		generation int64
		wantGvr    schema.GroupVersionResource
		wantErr    error
	}{
		"manifest is in proper format/ happy path": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, testManifest),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    expectedGvr,
			wantErr:    nil,
		},
		"manifest is in proper format/ owner reference already exists / should succeed": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, testManifestWithOwnerRef),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    expectedGvr,
			wantErr:    nil,
		},
		"manifest has incorrect syntax/ decode fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, InvalidManifest),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    emptyGvr,
			wantErr: &json2.UnmarshalTypeError{
				Value: "string",
				Type:  reflect.TypeOf(map[string]interface{}{}),
			},
		},
		"manifest is correct / object not mapped in restmapper / decode fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, MissingManifest),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    emptyGvr,
			wantErr:    errors.New("failed to find gvr from restmapping: test error: mapping does not exist."),
		},
		"manifest is in proper format / existing ManifestCondition does not have condition / fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, testManifest),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    expectedGvr,
			wantErr:    nil,
		},
		"manifest is in proper format / existing ManifestCondition has incorrect condition / fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml: append([]workv1alpha1.Manifest{}, testManifest),
			mcl: append([]workv1alpha1.ManifestCondition{}, workv1alpha1.ManifestCondition{
				Identifier: workv1alpha1.ResourceIdentifier{
					Ordinal:   0,
					Group:     testDeployment.GroupVersionKind().Group,
					Version:   testDeployment.GroupVersionKind().Version,
					Kind:      testDeployment.GroupVersionKind().Kind,
					Resource:  testGvr.Resource,
					Namespace: getRandomString(),
					Name:      getRandomString(),
				},
				Conditions: nil,
			}),
			generation: 0,
			wantGvr:    expectedGvr,
			wantErr:    nil,
		},
		"manifest is in proper format/ should fail applyUnstructured": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: failDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, testManifest),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    expectedGvr,
			wantErr:    errors.New("Failed to apply an unstructrued object"),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			resultList := testCase.reconciler.applyManifests(testCase.ml, testCase.mcl, ownerRef)
			for _, result := range resultList {
				if testCase.wantErr != nil {
					assert.Containsf(t, result.err.Error(), testCase.wantErr.Error(), "Incorrect error for Testcase %s", testName)
				}
				if result.identifier.Kind == "Deployment" {
					assert.Equalf(t, testCase.generation, result.generation, "Testcase %s: generation incorrect", testName)
				} else {
					assert.Equalf(t, int64(0), result.generation, "Testcase %s: non-involved resources generation changed", testName)
				}
			}
		})
	}
}

func TestApplyUnstructured(t *testing.T) {

	//Resource setup for checking update
	testDeploymentDiffSpec := testDeployment
	testDeploymentDiffSpec.Spec.MinReadySeconds = 0
	rawDiffSpec, _ := json.Marshal(testDeploymentDiffSpec)
	testManifestDiffSpec := workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawDiffSpec,
	}}
	unstructuredObjDiffSpec := &unstructured.Unstructured{}
	err := unstructuredObjDiffSpec.UnmarshalJSON(testManifestDiffSpec.Raw)
	specHash, _ := generateSpecHash(unstructuredObjDiffSpec)
	unstructuredObjDiffSpec.SetAnnotations(map[string]string{"multicluster.x-k8s.io/spec-hash": specHash})
	dynamicClientDiffSpec := fake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClientDiffSpec.PrependReactor("get", "*", func(action testing2.Action) (handled bool, ret runtime.Object, err error) {
		return true, unstructuredObjDiffSpec, nil
	})

	//Resource setup for owner ref check
	unstructuredObjDiffOwner := &unstructured.Unstructured{}
	err = unstructuredObjDiffOwner.UnmarshalJSON(testManifestDifferentOwner.Raw)

	dynamicClientDiffOwner := fake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClientDiffOwner.PrependReactor("get", "*", func(action testing2.Action) (handled bool, ret runtime.Object, err error) {
		return true, unstructuredObjDiffOwner, nil
	})

	unstructuredObj := &unstructured.Unstructured{}
	err = unstructuredObj.UnmarshalJSON(testManifest.Raw)
	if err != nil {
		klog.Fatal("unmarshal failed")
	}

	corruptedUnstructuredObj := &unstructured.Unstructured{}
	err = corruptedUnstructuredObj.UnmarshalJSON(testManifest.Raw)
	corruptedUnstructuredObj.Object["test"] = math.Inf(1)

	testCases := map[string]struct {
		reconciler       ApplyWorkReconciler
		gvr              schema.GroupVersionResource
		workObj          *unstructured.Unstructured
		resultName       string
		resultGeneration int64
		resultBool       bool
		wantErr          error
	}{
		"happy path": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			gvr:              testGvr,
			workObj:          unstructuredObj,
			resultName:       testDeployment.Name,
			resultGeneration: 0,
			resultBool:       true,
			wantErr:          nil,
		},
		"Given unstructured has invalid manifest / fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			gvr:              testGvr,
			workObj:          corruptedUnstructuredObj,
			resultName:       "",
			resultGeneration: 0,
			resultBool:       false,
			wantErr:          errors.New("unsupported value"),
		},
		"Owner Reference is not shared/ should fail ": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: dynamicClientDiffOwner,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			gvr:              testGvr,
			workObj:          unstructuredObj,
			resultName:       "",
			resultGeneration: 0,
			resultBool:       false,
			wantErr:          errors.New("this object is not owned by the work-api"),
		},
		"Existing Resources needs to be updated/ should succeed with changed information ": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: dynamicClientDiffSpec,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			gvr:              testGvr,
			workObj:          unstructuredObj,
			resultName:       testDeployment.Name,
			resultGeneration: 1,
			resultBool:       true,
			wantErr:          nil,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			applyResult, applyResultBool, err := testCase.reconciler.applyUnstructured(testCase.gvr, testCase.workObj, 0)
			if testCase.wantErr != nil {
				assert.Containsf(t, err.Error(), testCase.wantErr.Error(), "Incorrect error for Testcase %s", testName)
			} else {
				assert.Equalf(t, testCase.resultName, applyResult.GetName(), "Testcase %s ", testName)
				assert.Equalf(t, testCase.resultGeneration, applyResult.GetGeneration(), "Testcase %s ", testName)
			}
			assert.Equalf(t, testCase.resultBool, applyResultBool, "Testcase %s ", testName)
		})
	}
}

func TestReconcile(t *testing.T) {
	workNamespace := getRandomString()
	workName := getRandomString()
	appliedWorkNamespace := getRandomString()
	appliedWorkName := getRandomString()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: workNamespace,
			Name:      workName,
		},
	}
	wrongReq := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: getRandomString(),
			Name:      getRandomString(),
		},
	}
	invalidReq := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "",
			Name:      "",
		},
	}

	//Setups for Work-related clients
	getMock := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {

		if key.Namespace != workNamespace {
			return &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonNotFound,
				}}
		}
		o, _ := obj.(*workv1alpha1.Work)
		*o = workv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  workNamespace,
				Name:       workName,
				Finalizers: []string{"multicluster.x-k8s.io/work-cleanup"},
			},
			Spec: workv1alpha1.WorkSpec{Workload: workv1alpha1.WorkloadTemplate{Manifests: []workv1alpha1.Manifest{testManifest}}},
		}
		return nil
	}

	// Setups for AppliedWork-related clients
	getMockAppliedWork := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {

		if key.Namespace != workNamespace {
			return &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonNotFound,
				}}
		}
		o, _ := obj.(*workv1alpha1.AppliedWork)
		*o = workv1alpha1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: appliedWorkNamespace,
				Name:      appliedWorkName,
			},
			Spec: workv1alpha1.AppliedWorkSpec{
				WorkName:      workNamespace,
				WorkNamespace: workName,
			},
		}
		return nil
	}

	testCases := map[string]struct {
		reconciler ApplyWorkReconciler
		req        ctrl.Request
		wantErr    error
	}{
		"happy path": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient: &test.MockClient{
					MockGet: getMockAppliedWork,
				},
				log:        logr.Logger{},
				restMapper: testMapper{},
			},
			req:     req,
			wantErr: nil,
		},
		"work without finalizer / should fail": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*workv1alpha1.Work)
						*o = workv1alpha1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: workNamespace,
								Name:      workName,
							},
						}
						return nil
					},
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			req:     req,
			wantErr: nil,
		},
		"work cannot be retrieved, client failed due to not found error": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			req:     wrongReq,
			wantErr: nil,
		},
		"work cannot be retrieved, client failed due to client error": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return fmt.Errorf("client failing")
					},
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			req:     invalidReq,
			wantErr: errors.New(""),
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			_, err := testCase.reconciler.Reconcile(context.Background(), testCase.req)
			if testCase.wantErr != nil {
				assert.Containsf(t, err.Error(), testCase.wantErr.Error(), "incorrect error for Testcase %s", testName)
			}
		})
	}
}

func getRandomString() string {
	return utilrand.String(10)
}
