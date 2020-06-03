package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	Image           = "docker.io/alpine:latest"
	VolumeMountName = "shiny-potato"
	MountPath       = "/mnt/test"

	APICallRetryInterval     = 5000 * time.Millisecond
	DefaultPollTimeout       = 30 * time.Minute
	DefaultSleepMilliseconds = 3000
)

var (
	fs             *flag.FlagSet
	kubeconfig     *string
	prefix         *string
	namespace      *string
	storageClass   *string
	count          *int
	reqStorageSize *string
	Command        = []string{"tail", "-f", "/dev/null"}
	Labels         = map[string]string{
		"app": "shiny-potato",
	}
)

type Pvc struct {
	Name         *string
	Namespace    *string
	ClientSet    *kubernetes.Clientset
	Size         *string
	StorageClass *string
	Timings      Timing
}

type Pod struct {
	Name      *string
	Namespace *string
	ClientSet *kubernetes.Clientset
	Timings   Timing
}

type Timing struct {
	Start    time.Time
	End      time.Time
	Duration time.Duration
}

type Resource interface {
	Create() error
	WaitCreate() error
	Delete() error
	WaitDelete() error
}

///////////////////////////
// PersistentVolumeClaim //
///////////////////////////
func newPvClaim(ns, name, size string, sc *string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    Labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: sc,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName(corev1.ResourceStorage): resource.MustParse(size),
				},
			},
		},
	}
}

func (p *Pvc) Create() error {
	fmt.Printf(">>> Creating PersistentVolumeClaim: %v/%v\n", *p.Namespace, *p.Name)
	pvc := newPvClaim(*p.Namespace, *p.Name, *p.Size, p.StorageClass)
	p.Timings.Start = time.Now()
	_, err := p.ClientSet.CoreV1().PersistentVolumeClaims(*p.Namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (p *Pvc) WaitCreate() error {
	return wait.PollImmediate(APICallRetryInterval, DefaultPollTimeout, func() (bool, error) {
		pvc, err := p.ClientSet.CoreV1().PersistentVolumeClaims(*p.Namespace).Get(context.TODO(), *p.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if pvc.Status.Phase == corev1.ClaimBound {
			p.Timings.End = time.Now()
			p.Timings.Duration = time.Since(p.Timings.Start)
			return true, nil
		}

		return false, nil
	})
}

func (p *Pvc) Delete() error {
	fmt.Printf(">>> Deleting PersistentVolumeClaim: %v/%v\n", *p.Namespace, *p.Name)
	p.Timings.Start = time.Now()
	deletePolicy := metav1.DeletePropagationForeground
	err := p.ClientSet.CoreV1().PersistentVolumeClaims(*p.Namespace).
		Delete(context.TODO(), *p.Name, metav1.DeleteOptions{PropagationPolicy: &deletePolicy})
	if err != nil {
		return err
	}
	p.Timings.End = time.Now()
	p.Timings.Duration = time.Since(p.Timings.Start)

	return nil
}

func (p *Pvc) WaitDelete() error {
	return wait.PollImmediate(APICallRetryInterval, DefaultPollTimeout, func() (bool, error) {
		_, err := p.ClientSet.CoreV1().PersistentVolumeClaims(*p.Namespace).Get(context.TODO(), *p.Name, metav1.GetOptions{})
		if k8serr.IsNotFound(err) {
			return true, nil
		}

		return false, err
	})
}

/////////
// Pod //
/////////
func newPod(ns, name, pvcName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    Labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    name,
					Image:   Image,
					Command: Command,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      VolumeMountName,
							MountPath: MountPath,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: VolumeMountName,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
							ReadOnly:  false,
						},
					},
				},
			},
		},
	}
}

func (p *Pod) Create() error {
	fmt.Printf(">>> Creating Pod: %v/%v\n", *p.Namespace, *p.Name)
	pod := newPod(*p.Namespace, *p.Name, *p.Name)
	p.Timings.Start = time.Now()
	_, err := p.ClientSet.CoreV1().Pods(*p.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (p *Pod) WaitCreate() error {
	return wait.PollImmediate(APICallRetryInterval, DefaultPollTimeout, func() (bool, error) {
		pod, err := p.ClientSet.CoreV1().Pods(*p.Namespace).Get(context.TODO(), *p.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady {
				if condition.Status == corev1.ConditionTrue {
					p.Timings.End = time.Now()
					p.Timings.Duration = time.Since(p.Timings.Start)
					return true, nil
				}
			}
		}

		return false, nil
	})
}

func (p *Pod) Delete() error {
	fmt.Printf(">>> Deleting Pod: %v/%v\n", *p.Namespace, *p.Name)
	p.Timings.Start = time.Now()
	deletePolicy := metav1.DeletePropagationForeground
	err := p.ClientSet.CoreV1().Pods(*p.Namespace).
		Delete(context.TODO(), *p.Name, metav1.DeleteOptions{PropagationPolicy: &deletePolicy})
	if err != nil {
		return err
	}
	p.Timings.End = time.Now()
	p.Timings.Duration = time.Since(p.Timings.Start)

	return nil
}

func (p *Pod) WaitDelete() error {
	return wait.PollImmediate(APICallRetryInterval, DefaultPollTimeout, func() (bool, error) {
		_, err := p.ClientSet.CoreV1().Pods(*p.Namespace).Get(context.TODO(), *p.Name, metav1.GetOptions{})
		if k8serr.IsNotFound(err) {
			return true, nil
		}

		return false, err
	})
}

func Deploy(rsc Resource, errChan chan<- error, doneChan chan<- bool) {
	errCreate := rsc.Create()
	if errCreate != nil {
		errChan <- errCreate
	}

	errWait := rsc.WaitCreate()
	if errWait != nil {
		errChan <- errWait
	}

	doneChan <- true
}

func Clean(rsc Resource, errChan chan<- error, doneChan chan<- bool) {
	errCreate := rsc.Delete()
	if errCreate != nil {
		errChan <- errCreate
	}

	errWait := rsc.WaitDelete()
	if errWait != nil {
		errChan <- errWait
	}

	doneChan <- true
}

func parseArgs(name string, args []string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)

	prefix = fs.String("prefix", "shiny-potato", "name prefix used for pods and pvcs")
	namespace = fs.String("namespace", "default", "namespace for the pods")
	storageClass = fs.String("storage-class", "", "Storage Class of the PersistentVolumeClaims")
	count = fs.Int("count", 3, "Number of pod with pvc to create")
	reqStorageSize = fs.String("pvc-size", "100m", "Requested size of the PersistentVolumeClaims")
	kubeconfig = fs.String("kubeconfig", os.Getenv("KUBECONFIG"), "absolute path to the kubeconfig file")

	fs.Parse(args[2:])

	return fs
}

func main() {
	args := os.Args
	if len(args) < 2 {
		fmt.Printf("You must pass a sub-command: [deploy|clean|help]\n")
		os.Exit(1)
	}

	switch args[1] {
	case "deploy":
		fs = parseArgs("deploy", args)
	case "clean":
		fs = parseArgs("clean", args)
	default:
		fmt.Printf("You must pass a sub-command: [deploy|clean]\n\n")
		fs = parseArgs("help", args)
		fs.PrintDefaults()
		os.Exit(1)
	}

	if (*kubeconfig == "") || (*storageClass == "") {
		fmt.Println("kubeconfig or storage class missing")
		flag.PrintDefaults()
		os.Exit(1)
	}

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	//pvcs := make([]*Pvc, 0, *count)
	//pods := make([]*Pod, 0, *count)

	doneChan := make(chan bool)
	deployErrChan := make(chan error)
	cleanErrChan := make(chan error)

	start := time.Now()
	fmt.Println(">>> Starting:", start)
	for i := 1; i <= *count; i++ {
		name := fmt.Sprintf("%v-%04d", *prefix, i)

		// Generate PVC
		pvc := Pvc{Name: &name, ClientSet: clientset, Namespace: namespace, Size: reqStorageSize, StorageClass: storageClass}
		//	pvcs = append(pvcs, &pvc)

		// Generate POD
		pod := Pod{Name: &name, ClientSet: clientset, Namespace: namespace}
		//	pods = append(pods, &pod)

		switch fs.Name() {
		case "deploy":
			go Deploy(&pvc, deployErrChan, doneChan)
			go Deploy(&pod, deployErrChan, doneChan)
			time.Sleep(time.Duration(rand.Intn(DefaultSleepMilliseconds)) * time.Millisecond)
		case "clean":
			go Clean(&pvc, cleanErrChan, doneChan)
			go Clean(&pod, cleanErrChan, doneChan)
			time.Sleep(time.Duration(rand.Intn(DefaultSleepMilliseconds)) * time.Millisecond)
		}
	}

	i := 0
	for {
		select {
		case err := <-deployErrChan:
			if !(k8serr.IsAlreadyExists(err)) {
				panic(err)
			}
		case err := <-cleanErrChan:
			if !(k8serr.IsNotFound(err)) {
				panic(err)
			}
		case <-doneChan:
			i++
		}
		// POD + PVC
		if i == (*count * 2) {
			break
		}
	}

	//	for _, p := range pvcs {
	//		fmt.Println(*p.Name)
	//	}
	//
	//	for _, p := range pods {
	//		fmt.Println(*p.Name)
	//	}
	fmt.Println(">>> Finished:", time.Now())
	fmt.Println(">>> Duration:", time.Since(start))
}
