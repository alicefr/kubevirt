package kubecli

import (
	"context"

	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	v1 "kubevirt.io/client-go/api/v1"
)

func (k *kubevirt) GuestfsImageConfig() GuestfsImageConfigInterface {
	return &gs{
		restClient: k.restClient,
		resource:   "gsconfigs",
	}
}

type gs struct {
	restClient *rest.RESTClient
	resource   string
}

// Create new GuestfsImageConfig in the cluster
func (o *gs) Create(gs *v1.GuestfsImageConfig) (*v1.GuestfsImageConfig, error) {
	newGs := &v1.GuestfsImageConfig{}
	err := o.restClient.Post().
		Resource(o.resource).
		Body(gs).
		Do(context.Background()).
		Into(newGs)
	newGs.SetGroupVersionKind(v1.GuestfsGroupVersionKind)
	return newGs, err
}

// Get the GuestfsImageConfig from the cluster by its name
func (o *gs) Get(name string, options *k8smetav1.GetOptions) (*v1.GuestfsImageConfig, error) {
	newGs := &v1.GuestfsImageConfig{}
	err := o.restClient.Get().
		Resource(o.resource).
		Name(name).
		VersionedParams(options, scheme.ParameterCodec).
		Do(context.Background()).
		Into(newGs)
	newGs.SetGroupVersionKind(v1.GuestfsGroupVersionKind)
	return newGs, err
}

// Update the GuestfsImageConfig instance in the cluster in given namespace
func (o *gs) Update(gs *v1.GuestfsImageConfig) (*v1.GuestfsImageConfig, error) {
	updatedGs := &v1.GuestfsImageConfig{}
	err := o.restClient.Put().
		Resource(o.resource).
		Name(gs.ObjectMeta.Name).
		Body(gs).
		Do(context.Background()).
		Into(updatedGs)
	updatedGs.SetGroupVersionKind(v1.GuestfsGroupVersionKind)
	return updatedGs, err
}

// Delete the defined GuestfsImageConfig in the cluster
func (o *gs) Delete(name string, options *k8smetav1.DeleteOptions) error {
	err := o.restClient.Delete().
		Resource(o.resource).
		Name(name).
		Body(options).
		Do(context.Background()).
		Error()

	return err
}

// List all GuestfsImageConfig in the cluster
func (o *gs) List(options *k8smetav1.ListOptions) (*v1.GuestfsImageConfigList, error) {
	newGsList := &v1.GuestfsImageConfigList{}
	err := o.restClient.Get().
		Resource(o.resource).
		VersionedParams(options, scheme.ParameterCodec).
		Do(context.Background()).
		Into(newGsList)

	for _, vm := range newGsList.Items {
		vm.SetGroupVersionKind(v1.GuestfsGroupVersionKind)
	}

	return newGsList, err
}
