package admitters

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-openapi/errors"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	v1 "kubevirt.io/api/core/v1"
	virtstoragev1alpha1 "kubevirt.io/api/storage/v1alpha1"
	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/pkg/testutils"
	"kubevirt.io/kubevirt/pkg/virt-api/webhooks"
)

var _ = Describe("Storage migration admitter", func() {
	//	config, _, kvInformer := testutils.NewFakeClusterConfigUsingKVConfig(&v1.KubeVirtConfiguration{})
	config, _, _ := testutils.NewFakeClusterConfigUsingKVConfig(&v1.KubeVirtConfiguration{})
	testns := "kubevirt-test-ns"
	var ctrl *gomock.Controller

	//	var kubeClient *fake.Clientset
	var virtClient *kubecli.MockKubevirtClient
	var vmiClient *kubecli.MockVirtualMachineInstanceInterface
	var storageMigrationAdmitter StorageMigrationAdmitter

	//	enableFeatureGate := func(featureGate string) {
	//		testutils.UpdateFakeKubeVirtClusterConfig(kvInformer, &v1.KubeVirt{
	//			Spec: v1.KubeVirtSpec{
	//				Configuration: v1.KubeVirtConfiguration{
	//					DeveloperConfiguration: &v1.DeveloperConfiguration{
	//						FeatureGates: []string{featureGate},
	//					},
	//				},
	//			},
	//		})
	//	}
	//	disableFeatureGates := func() {
	//		testutils.UpdateFakeKubeVirtClusterConfig(kvInformer, &v1.KubeVirt{
	//			Spec: v1.KubeVirtSpec{
	//				Configuration: v1.KubeVirtConfiguration{
	//					DeveloperConfiguration: &v1.DeveloperConfiguration{
	//						FeatureGates: make([]string, 0),
	//					},
	//				},
	//			},
	//		})
	//	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		virtClient = kubecli.NewMockKubevirtClient(ctrl)
		vmiClient = kubecli.NewMockVirtualMachineInstanceInterface(ctrl)
		//kubeClient = fake.NewSimpleClientset()

		//	virtClient.EXPECT().CoreV1().Return(kubeClient.CoreV1()).AnyTimes()
		virtClient.EXPECT().VirtualMachineInstance(testns).Return(vmiClient).AnyTimes()
		storageMigrationAdmitter = StorageMigrationAdmitter{
			Config: config,
			Client: virtClient,
		}
	})

	Context("Storage migrations validation", func() {
		It("should reject Storage migration when VMI doesn't exist", func() {
			vmiName := "vmi-not-exist"
			errMsg := fmt.Sprintf("the VMI \"%s/%s\" does not exist", testns, vmiName)
			vmiClient.EXPECT().Get(context.Background(), vmiName, gomock.Any()).Return(nil, errors.NotFound(errMsg))
			sm := virtstoragev1alpha1.StorageMigration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sm",
					Namespace: testns,
				},
				Spec: virtstoragev1alpha1.StorageMigrationSpec{
					VMIName: vmiName,
				},
			}
			bytes, _ := json.Marshal(&sm)
			ar := &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					Resource: webhooks.StorageMigrationGroupVersionResource,
					Object: runtime.RawExtension{
						Raw: bytes,
					},
				},
			}
			resp := storageMigrationAdmitter.Admit(ar)
			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result.Message).To(Equal(errMsg))
		})

	})
})
