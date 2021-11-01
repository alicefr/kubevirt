package guestfs

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/pkg/virtctl/templates"
	"kubevirt.io/kubevirt/pkg/virtctl/utils"
)

var (
	timeout   = 500 * time.Second
	namespace string
)

type guestfsCommand struct {
	clientConfig clientcmd.ClientConfig
}

// NewGuestfsShellCommand returns a cobra.Command for starting libguestfs-tool pod and attach it to a pvc
func NewGuestfsShellCommand(clientConfig clientcmd.ClientConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "guestfs",
		Short:   "Start a shell into the libguestfs pod",
		Long:    `Create a pod with libguestfs-tools, mount the pvc and attach a shell to it. The pvc is mounted under the /disks directory inside the pod for filesystem-based pvcs, or as /dev/vda for block-based pvcs`,
		Args:    cobra.ExactArgs(1),
		Example: usage(),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := guestfsCommand{clientConfig: clientConfig}
			return c.run(cmd, args)
		},
	}
	cmd.SetUsageTemplate(templates.UsageTemplate())
	return cmd
}

func usage() string {
	usage := `  # Create a pod with libguestfs-tools, mount the pvc and attach a shell to it:
  {{ProgramName}} guestfs <vm-name>`
	return usage
}

// ClientCreator is a function to return the Kubernetes client
type ClientCreator func(config *rest.Config, virtClientConfig clientcmd.ClientConfig) (*K8sClient, error)

var createClientFunc ClientCreator

// SetClient allows overriding the default Kubernetes client. Useful for creating a mock function for the testing.
func SetClient(f ClientCreator) {
	createClientFunc = f
}

// SetDefaulClient sets the default function to create the Kubernetes client
func SetDefaulClient() {
	createClientFunc = createClient
}

// AttacherCreator is a function that attach a command to a pod using the Kubernetes client
type AttacherCreator func(client *K8sClient, p *corev1.Pod) error

var createAttacherFunc AttacherCreator

// SetAttacher allows overriding the default attacher function. Useful for creating a mock function for the testing.
func SetAttacher(f AttacherCreator) {
	createAttacherFunc = f
}

// SetDefaulAttacher sets the default function to attach to a pod
func SetDefaulAttacher() {
	createAttacherFunc = createAttacher
}

func init() {
	SetDefaulClient()
	SetDefaulAttacher()
}

func (c *guestfsCommand) run(cmd *cobra.Command, args []string) error {
	vmName := args[0]
	namespace, _, err := c.clientConfig.Namespace()
	if err != nil {
		return err
	}
	conf, err := c.clientConfig.ClientConfig()
	if err != nil {
		return err
	}

	client, err := createClientFunc(conf, c.clientConfig)
	if err != nil {
		return err
	}
	defer client.removePod(namespace)
	return client.createGuestfs(vmName, namespace)
}

// K8sClient holds the information of the Kubernetes client
type K8sClient struct {
	Client     kubernetes.Interface
	config     *rest.Config
	VirtClient kubecli.KubevirtClient
}

func createClient(config *rest.Config, virtClientConfig clientcmd.ClientConfig) (*K8sClient, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return &K8sClient{}, err
	}
	virtClient, err := kubecli.GetKubevirtClientFromClientConfig(virtClientConfig)
	if err != nil {
		return &K8sClient{}, fmt.Errorf("cannot obtain KubeVirt client: %v", err)
	}
	return &K8sClient{
		Client:     client,
		config:     config,
		VirtClient: virtClient,
	}, nil
}

func (client *K8sClient) waitForContainerRunning(pod, cont, ns string, timeout time.Duration) error {
	terminated := "Terminated"
	chTerm := make(chan os.Signal, 1)
	c := make(chan string, 1)
	signal.Notify(chTerm, os.Interrupt, syscall.SIGTERM)
	// if the user killed the guestfs command, the libguestfs-tools pod is also removed
	go func() {
		<-chTerm
		client.removePod(ns)
		c <- terminated
	}()

	go func() {
		for {
			pod, err := client.Client.CoreV1().Pods(ns).Get(context.TODO(), pod, metav1.GetOptions{})
			if err != nil {
				c <- err.Error()
			}
			if pod.Status.Phase != corev1.PodPending {
				c <- string(pod.Status.Phase)

			}
			for _, c := range pod.Status.ContainerStatuses {
				if c.State.Waiting != nil {
					fmt.Printf("Waiting for container %s still in pending, reason: %s, message: %s \n", c.Name, c.State.Waiting.Reason, c.State.Waiting.Message)
				}
			}

			time.Sleep(5 * time.Second)
		}
	}()
	select {
	case res := <-c:
		if res == string(corev1.PodRunning) || res == terminated {
			return nil
		}
		return fmt.Errorf("Pod is not in running state but got %s", res)
	case <-time.After(timeout):
		return fmt.Errorf("timeout in waiting for the containers to be started in pod %s", pod)
	}

}

// createAttacher attaches the stdin, stdout, and stderr to the container shell
func createAttacher(client *K8sClient, p *corev1.Pod) error {
	req := client.Client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(p.Name).
		Namespace(p.Namespace).
		SubResource("attach")
	req.VersionedParams(
		&corev1.PodAttachOptions{
			Container: "guestfs",
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec,
	)
	exec, err := remotecommand.NewSPDYExecutor(client.config, "POST", req.URL())
	if err != nil {
		return err
	}

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	resChan := make(chan error)

	go func() {
		resChan <- exec.Stream(remotecommand.StreamOptions{
			Stdin:  stdinReader,
			Stdout: stdoutWriter,
			Stderr: stdoutWriter,
		})
	}()
	return utils.AttachConsole(stdinReader, stdoutReader, stdinWriter, stdoutWriter,
		"If you don't see a command prompt, try pressing enter.", resChan)
}

func (client *K8sClient) createGuestfs(vmName, namespace string) error {
	podName := fmt.Sprintf("guestfs-%s", vmName)
	// TODO send create guestfs request
	err := client.waitForContainerRunning(podName, "guestfs", namespace, timeout)
	if err != nil {
		return err
	}
	p, err := client.Client.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return createAttacherFunc(client, p)
}

func (client *K8sClient) removePod(ns string) error {
	// TODO send delete guestfs request
	return nil
}
