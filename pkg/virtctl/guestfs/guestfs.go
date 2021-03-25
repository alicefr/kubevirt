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
	"k8s.io/apimachinery/pkg/api/resource"
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

const (
	defaultImageName = "libguestfs-tools"
	// KvmDevice defines the resource as in pkg/virt-controller/services/template.go, but we don't import the package to avoid compile conflicts when the os is windows
	KvmDevice     = "devices.kubevirt.io/kvm"
	volume        = "volume"
	contName      = "libguestfs"
	diskDir       = "/disks"
	diskPath      = "/dev/vda"
	podNamePrefix = "libguestfs-tools"
	appliancePath = "/usr/local/lib/guestfs"
)

var (
	pvc         string
	image       string
	timeout     = 500 * time.Second
	pullPolicy  = corev1.PullIfNotPresent
	kubevirtNs  string
	kubevirtCRD string
	kvm         bool
	namespace   string
	podName     string
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
		Example: usage(),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := guestfsCommand{clientConfig: clientConfig}
			return c.run(cmd, args)
		},
	}
	cmd.PersistentFlags().StringVar(&pvc, "pvc", "", "pvc claim name")
	cmd.MarkPersistentFlagRequired("pvc")
	cmd.PersistentFlags().StringVar(&image, "image", "", "libguestfs-tools container image")
	cmd.PersistentFlags().StringVar(&kubevirtNs, "kubevirt-ns", "kubevirt", "namespace where kubevirt resources are installed")
	cmd.PersistentFlags().StringVar(&kubevirtCRD, "kubevirt-crd", "kubevirt", "name of the kubevirt resource")
	cmd.PersistentFlags().StringVar(&namespace, "pvc-namespace", "default", "namespace of the pvc")
	cmd.PersistentFlags().BoolVar(&kvm, "kvm", true, "Use kvm for the libguestfs-tools container")
	cmd.SetUsageTemplate(templates.UsageTemplate())
	return cmd
}

func usage() string {
	usage := `  # Create a pod with libguestfs-tools, mount the pvc and attach a shell to it:
  {{ProgramName}} guestfs --pvc pvc`
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
type AttacherCreator func(client *K8sClient, p *corev1.Pod, command string) error

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
	var inUse bool
	conf, err := c.clientConfig.ClientConfig()
	if err != nil {
		return err
	}
	client, err := createClientFunc(conf, c.clientConfig)
	if err != nil {
		return err
	}
	// if the image has not been set from the command line, then we fetch the registry and the image information from the kubevirt CRD
	if image == "" {
		if err = client.setImageFromKubeVirt(); err != nil {
			return err
		}
	}
	exist, _ := client.existsPVC(pvc, namespace)
	if !exist {
		return fmt.Errorf("The PVC %s doesn't exist", pvc)
	}
	inUse, err = client.isPVCinUse(pvc, namespace)
	if err != nil {
		return err
	}
	if inUse {
		return fmt.Errorf("PVC %s is used by another pod", pvc)
	}
	isBlock, err := client.isPVCVolumeBlock(pvc, namespace)
	if err != nil {
		return err
	}
	defer client.removePod(namespace)
	return client.createInteractivePodWithPVC(pvc, image, namespace, "/entrypoint.sh", []string{}, isBlock)
}

// K8sClient holds the information of the Kubernetes client
type K8sClient struct {
	Client     kubernetes.Interface
	config     *rest.Config
	VirtClient kubecli.KubevirtClient
}

func (client *K8sClient) setImageFromKubeVirt() error {
	kubevirt, err := client.VirtClient.KubeVirt(kubevirtNs).Get(kubevirtCRD, &metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot obtain KubeVirt resource: %v", err)
	}
	pullPolicy = kubevirt.Spec.ImagePullPolicy
	image = fmt.Sprintf("%s/%s:%s", kubevirt.Status.TargetKubeVirtRegistry, defaultImageName, kubevirt.Status.TargetKubeVirtVersion)
	return nil
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

func (client *K8sClient) existsPVC(pvc, ns string) (bool, error) {
	p, err := client.Client.CoreV1().PersistentVolumeClaims(ns).Get(context.TODO(), pvc, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if p.Name == "" {
		return false, nil
	}
	return true, nil
}

func (client *K8sClient) isPVCVolumeBlock(pvc, ns string) (bool, error) {
	p, err := client.Client.CoreV1().PersistentVolumeClaims(ns).Get(context.TODO(), pvc, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if *p.Spec.VolumeMode == corev1.PersistentVolumeBlock {
		return true, nil
	}
	return false, nil
}

func (client *K8sClient) existsPod(pod, ns string) bool {
	p, err := client.Client.CoreV1().Pods(ns).Get(context.TODO(), pod, metav1.GetOptions{})
	if err != nil {
		return false
	}
	if p.Name == "" {
		return false
	}
	return true
}

func (client *K8sClient) isPVCinUse(pvc, ns string) (bool, error) {
	pods, err := client.getPodsForPVC(pvc, ns)
	if err != nil {
		return false, err
	}
	if len(pods) > 0 {
		return true, nil
	}
	return false, nil
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

func (client *K8sClient) getPodsForPVC(pvcName, ns string) ([]corev1.Pod, error) {
	nsPods, err := client.Client.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return []corev1.Pod{}, err
	}

	var pods []corev1.Pod

	for _, pod := range nsPods.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.VolumeSource.PersistentVolumeClaim != nil && volume.VolumeSource.PersistentVolumeClaim.ClaimName == pvcName {
				pods = append(pods, pod)
			}
		}
	}

	return pods, nil
}

func createLibguestfsPod(pvc, image, cmd string, args []string, kvm, isBlock bool) *corev1.Pod {
	var resources corev1.ResourceRequirements
	var user, group int64
	podName = fmt.Sprintf("%s-%s", podNamePrefix, pvc)
	user = 0
	group = 0
	if kvm {
		resources = corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				KvmDevice: resource.MustParse("1"),
			},
		}
	}
	c := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: volume,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc,
							ReadOnly:  false,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:    contName,
					Image:   image,
					Command: []string{cmd},
					Args:    args,
					// Set env variable to start libguestfs correctly using qemu directly and with a fixed appliance 					// in /usr/local/lib/guestfs/appliance
					Env: []corev1.EnvVar{
						{
							Name:  "LIBGUESTFS_BACKEND",
							Value: "direct",
						},
						{
							Name:  "LIBGUESTFS_PATH",
							Value: "/usr/local/lib/guestfs/appliance",
						},
					},
					ImagePullPolicy: pullPolicy,
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:  &user,
						RunAsGroup: &group,
					},
					Stdin:     true,
					TTY:       true,
					Resources: resources,
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	if isBlock {
		c.Spec.Containers[0].VolumeDevices = append(c.Spec.Containers[0].VolumeDevices, corev1.VolumeDevice{
			Name:       volume,
			DevicePath: diskPath,
		})
		fmt.Printf("The PVC has been mounted at %s \n", diskPath)
		return c
	}
	// PVC volume mode is filesystem
	c.Spec.Containers[0].VolumeMounts = append(c.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      volume,
		ReadOnly:  false,
		MountPath: diskDir,
	})

	c.Spec.Containers[0].WorkingDir = diskDir
	fmt.Printf("The PVC has been mounted at %s \n", diskDir)

	return c
}

// createAttacher attaches the stdin, stdout, and stderr to the container shell
func createAttacher(client *K8sClient, p *corev1.Pod, command string) error {
	req := client.Client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(p.Name).
		Namespace(p.Namespace).
		SubResource("attach")
	req.VersionedParams(
		&corev1.PodAttachOptions{
			Container: contName,
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

func (client *K8sClient) createInteractivePodWithPVC(pvc, image, ns, command string, args []string, isblock bool) error {
	pod := createLibguestfsPod(pvc, image, command, args, kvm, isblock)
	p, err := client.Client.CoreV1().Pods(ns).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	err = client.waitForContainerRunning(podName, contName, ns, timeout)
	if err != nil {
		return err
	}
	return createAttacherFunc(client, p, command)
}

func (client *K8sClient) removePod(ns string) error {
	return client.Client.CoreV1().Pods(ns).Delete(context.TODO(), podName, metav1.DeleteOptions{})
}
