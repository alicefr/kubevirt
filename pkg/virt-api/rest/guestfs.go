package rest

import (
	"fmt"
	"net/http"

	restful "github.com/emicklei/go-restful"
	"k8s.io/apimachinery/pkg/api/errors"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "kubevirt.io/client-go/apis/core/v1"
)

func (app *SubresourceAPIApp) GuestfsRequestHandler(request *restful.Request, response *restful.Response) {
	name := request.PathParameter("name")
	namespace := request.PathParameter("namespace")

	vm, err := app.fetchVirtualMachine(name, namespace)
	if err != nil {
		writeError(err, response)
		return
	}
	if err := app.validateGuestfsRequest(vm); err != nil {
		writeError(errors.NewBadRequest(err.Error()), response)
		return
	}
	// Patch the VM to set the RunStrategy to Maintenance
	patch := fmt.Sprintf("{\"spec\":{\"runStrategy\": \"%s\"}}", v1.RunStrategyMaintenance)
	if _, err := app.virtCli.VirtualMachine(namespace).Patch(vm.GetName(), types.MergePatchType, []byte(patch)); err != nil {
		writeError(errors.NewInternalError(err), response)
		return
	}
	response.WriteHeader(http.StatusAccepted)
}

func (app *SubresourceAPIApp) validateGuestfsRequest(vm *v1.VirtualMachine) error {
	strategy, err := vm.RunStrategy()
	if err != nil {
		return err
	}
	if strategy == v1.RunStrategyAlways {
		return fmt.Errorf("VM cannot be running")
	}
	// Check if there are any left VMI before changing the RunStrategy
	if strategy != v1.RunStrategyMaintenance {
		vmi, _ := app.virtCli.VirtualMachineInstance(vm.ObjectMeta.Namespace).Get(vm.ObjectMeta.Name, &k8smetav1.GetOptions{})
		if vmi != nil {
			if !vmi.IsFinal() {
				return fmt.Errorf("Old VMIs need to be finalized or VM needs to be stopped")
			}
		}
	}
	// TODO afrosi Check maintenance pods label
	return nil
}
