package device_manager

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	pluginapi "kubevirt.io/kubevirt/pkg/virt-handler/device-manager/deviceplugin/v1beta1"
)

type fakeServer struct {
	listAndWatchResponseError error
	count                     int
}

func (fakeServer *f) setlistAndWatchResponseError(err error) {
	f.listAndWatchResponseError = err
}

func (fakeServer *f) Send(m *ListAndWatchResponse) error {
	f.count++
	return f.listAndWatchResponseError
}

var _ = Describe("Device plugin base", func() {
	Context("List and Watch", func() {
		var (
			fakeServer   fakeServer
			devicePlugin *DevicePluginBase
			stop         <-chan struct{}
		)
		BeforeEach(func() {
			stop = make(chan struct{})
			fakeServer = &fakeServer{
				count: 0,
			}
			devicePlugin = &DevicePluginBase{
				done: make(chan struct{}),
				stop: stop,
			}
		})
		DescribeTable("should initially send the request to the server", func(err error) {
			fakeServer.setlistAndWatchResponseError(err)
			Expect(fakeServer.Send(&ListAndWatchResponse{})).To(Equal(err))
			Expect(devicePlugin.ListAndWatch(nil, fakeServer)).NotTo(HaveOccurred())
		},
			Entry("with no error", nil),
			Entry("with an error", errors.New("error")),
		)

		It("should gracefully stop the device plugin", func() {
			errChan := make(chan error)
			go func() {
				errChan <- devicePlugin.ListAndWatch(nil, fakeServer)
			}()
			close(stop)
			err := <-errChan
			Expect(err).NotTo(HaveOccurred())
		})

		It("should send the device infomation to the server", func() {
			errChan := make(chan error)
			devicePlugin.DeviceInfo = []*pluginapi.Device{
				&pluginapi.Device{}, &pluginapi.Device{}, &pluginapi.Device{},
			}
			go func() {
				errChan <- devicePlugin.ListAndWatch(fakeServer, stop)
			}()
			close(done)
			err := <-errChan
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeServer.count).To(Equal(3))
		})
	})
})
