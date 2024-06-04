package device_manager

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"

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

	Context("device plugin lifecycle", func() {
		var workDir string
		var err error
		var dpi *DevicePluginBase
		var stop chan struct{}
		var devicePath string

		BeforeEach(func() {
			workDir, err = os.MkdirTemp("", "kubevirt-test")
			Expect(err).ToNot(HaveOccurred())

			devicePath = path.Join(workDir, "foo")
			fileObj, err := os.Create(devicePath)
			Expect(err).ToNot(HaveOccurred())
			fileObj.Close()

			dpi = NewGenericDevicePlugin("foo", devicePath, 1, "rw", true)
			dpi.socketPath = filepath.Join(workDir, "test.sock")
			dpi.server = grpc.NewServer([]grpc.ServerOption{}...)
			dpi.done = make(chan struct{})
			dpi.deviceRoot = "/"
			stop = make(chan struct{})
			dpi.stop = stop

		})

		AfterEach(func() {
			close(stop)
			os.RemoveAll(workDir)
		})

		It("Should stop if the device plugin socket file is deleted", func() {
			os.OpenFile(dpi.socketPath, os.O_RDONLY|os.O_CREATE, 0666)

			errChan := make(chan error, 1)
			go func(errChan chan error) {
				errChan <- dpi.healthCheck()
			}(errChan)
			Consistently(func() string {
				return dpi.devs[0].Health
			}, 2*time.Second, 500*time.Millisecond).Should(Equal(pluginapi.Healthy))
			Expect(os.Remove(dpi.socketPath)).To(Succeed())

			Expect(<-errChan).ToNot(HaveOccurred())
		})

		It("Should monitor health of device node", func() {

			os.OpenFile(dpi.socketPath, os.O_RDONLY|os.O_CREATE, 0666)

			go dpi.healthCheck()
			Expect(dpi.devs[0].Health).To(Equal(pluginapi.Healthy))

			time.Sleep(1 * time.Second)
			By("Removing a (fake) device node")
			os.Remove(devicePath)

			By("waiting for healthcheck to send Unhealthy message")
			Eventually(func() string {
				return (<-dpi.health).Health
			}, 5*time.Second).Should(Equal(pluginapi.Unhealthy))

			By("Creating a new (fake) device node")
			fileObj, err := os.Create(devicePath)
			Expect(err).ToNot(HaveOccurred())
			fileObj.Close()

			By("waiting for healthcheck to send Healthy message")
			Eventually(func() string {
				return (<-dpi.health).Health
			}, 5*time.Second).Should(Equal(pluginapi.Healthy))
		})
	})
})
