package admitters

import (
	"context"
	"encoding/json"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubevirt.io/client-go/kubecli"

	virtstoragev1alpha1 "kubevirt.io/api/storage/v1alpha1"

	webhookutils "kubevirt.io/kubevirt/pkg/util/webhooks"
	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"
)

// StorageMigrationAdmitter validates StorageMigration
type StorageMigrationAdmitter struct {
	Config *virtconfig.ClusterConfig
	Client kubecli.KubevirtClient
}

// NewStorageMigrationAdmitter creates a StorageMigrationAdmitter
func NewStorageMigrationAdmitter(config *virtconfig.ClusterConfig, client kubecli.KubevirtClient) *StorageMigrationAdmitter {
	return &StorageMigrationAdmitter{
		Config: config,
		Client: client,
	}
}

func (admitter *StorageMigrationAdmitter) validatePVCsExistance(sm *virtstoragev1alpha1.StorageMigration) error {
	namespace := sm.ObjectMeta.Namespace
	for _, migVol := range sm.Spec.MigratedVolume {
		if _, err := admitter.Client.CoreV1().PersistentVolumeClaims(namespace).Get(context.Background(), migVol.SourcePvc, metav1.GetOptions{}); err != nil {
			return fmt.Errorf("the source PVC \"%s/%s\" does not exist", namespace, migVol.SourcePvc)
		}
		if _, err := admitter.Client.CoreV1().PersistentVolumeClaims(namespace).Get(context.Background(), migVol.DestinationPvc, metav1.GetOptions{}); err != nil {
			return fmt.Errorf("the destination PVC \"%s/%s\" does not exist", namespace, migVol.DestinationPvc)
		}
	}
	return nil

}

func (admitter *StorageMigrationAdmitter) Admit(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	if ar.Request.Resource.Group != virtstoragev1alpha1.SchemeGroupVersion.Group ||
		ar.Request.Resource.Resource != "storagemigrations" {
		return webhookutils.ToAdmissionResponseError(fmt.Errorf("unexpected resource %+v", ar.Request.Resource))
	}

	sm := &virtstoragev1alpha1.StorageMigration{}
	err := json.Unmarshal(ar.Request.Object.Raw, sm)
	if err != nil {
		return webhookutils.ToAdmissionResponseError(err)
	}

	// Ensure that the VMI exists
	_, err = admitter.Client.VirtualMachineInstance(sm.Namespace).Get(context.Background(), sm.Spec.VMIName, &metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return webhookutils.ToAdmissionResponseError(fmt.Errorf("the VMI \"%s/%s\" does not exist", sm.Namespace, sm.Spec.VMIName))
	} else if err != nil {
		return webhookutils.ToAdmissionResponseError(err)
	}

	// Ensure the source and destination PVCs exist
	if err = admitter.validatePVCsExistance(sm); err != nil {
		return webhookutils.ToAdmissionResponseError(err)
	}

	// TODO: Ensure that the source and destination storage classes exist

	reviewResponse := admissionv1.AdmissionResponse{
		Allowed: true,
	}
	return &reviewResponse
}
