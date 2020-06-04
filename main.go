package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/fatih/color"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	AppName            = "shiny-potato"
	VolumeMountName    = AppName
	MountPath          = "/mnt/test"
	DefaultResultsFile = AppName + ".json"
	DefaultImage       = "docker.io/alpine:latest"
	DefaultSize        = "100m"
	DefaultCount       = 3

	APICallRetryInterval     = 5000 * time.Millisecond
	DefaultPollTimeout       = 30 * time.Minute
	DefaultSleepMilliseconds = 3000
)

var (
	fs             *flag.FlagSet
	resultsFile    *string
	noResultsFile  *bool
	resultsStdout  *bool
	noResults      *bool
	kubeconfig     *string
	prefix         *string
	namespace      *string
	image          *string
	storageClass   *string
	count          *int
	reqStorageSize *string
	Command        = []string{"tail", "-f", "/dev/null"}
	Labels         = map[string]string{
		"app": "shiny-potato",
	}
)

type PodWithPvc struct {
	Namespace *string
	Command   string
	Pvc       []*Pvc
	Pod       []*Pod
}

type Pod struct {
	Name      *string
	Namespace *string               `json:"-"`
	Image     *string               `json:"-"`
	ClientSet *kubernetes.Clientset `json:"-"`
	Timings   Timing
}

type Pvc struct {
	Name         *string
	Namespace    *string               `json:"-"`
	ClientSet    *kubernetes.Clientset `json:"-"`
	Size         *string
	StorageClass *string
	Timings      Timing
}

type Timing struct {
	Start    time.Time
	End      time.Time
	Duration string
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
	fmt.Printf(">>> [PVCLAIM] %v/%v creating...\n", *p.Namespace, *p.Name)
	pvc := newPvClaim(*p.Namespace, *p.Name, *p.Size, p.StorageClass)
	p.Timings.Start = time.Now()
	_, err := p.ClientSet.CoreV1().PersistentVolumeClaims(*p.Namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	fmt.Printf(">>> [PVCLAIM] %v/%v created\n", *p.Namespace, *p.Name)
	return nil
}

func (p *Pvc) WaitCreate() error {
	return wait.PollImmediate(APICallRetryInterval, DefaultPollTimeout, func() (bool, error) {
		fmt.Printf(">>> [PVCLAIM] %v/%v waiting to be bound....\n", *p.Namespace, *p.Name)
		pvc, err := p.ClientSet.CoreV1().PersistentVolumeClaims(*p.Namespace).Get(context.TODO(), *p.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if pvc.Status.Phase == corev1.ClaimBound {
			p.Timings.End = time.Now()
			p.Timings.Duration = time.Since(p.Timings.Start).String()
			logSuccess(fmt.Sprintf(">>> [PVCLAIM] %v/%v bound, time elapsed: %v\n", *p.Namespace, *p.Name, p.Timings.Duration))
			return true, nil
		}

		return false, nil
	})
}

func (p *Pvc) Delete() error {
	fmt.Printf(">>> [PVCLAIM] %v/%v deleting...\n", *p.Namespace, *p.Name)
	p.Timings.Start = time.Now()
	deletePolicy := metav1.DeletePropagationForeground
	err := p.ClientSet.CoreV1().PersistentVolumeClaims(*p.Namespace).
		Delete(context.TODO(), *p.Name, metav1.DeleteOptions{PropagationPolicy: &deletePolicy})
	if err != nil {
		return err
	}

	fmt.Printf(">>> [PVCLAIM] %v/%v deletion started\n", *p.Namespace, *p.Name)
	return nil
}

func (p *Pvc) WaitDelete() error {
	return wait.PollImmediate(APICallRetryInterval, DefaultPollTimeout, func() (bool, error) {
		fmt.Printf(">>> [PVCLAIM] %v/%v waiting to be deleted...\n", *p.Namespace, *p.Name)
		_, err := p.ClientSet.CoreV1().PersistentVolumeClaims(*p.Namespace).Get(context.TODO(), *p.Name, metav1.GetOptions{})
		if k8serr.IsNotFound(err) {
			p.Timings.End = time.Now()
			p.Timings.Duration = time.Since(p.Timings.Start).String()

			logSuccess(fmt.Sprintf(">>> [PVCLAIM] %v/%v deleted, time elapsed: %v\n", *p.Namespace, *p.Name, p.Timings.Duration))
			return true, nil
		}

		return false, err
	})
}

func logSuccess(message string) {
	color.Green(message)
}

/////////
// Pod //
/////////
func newPod(ns, name, image, pvcName string) *corev1.Pod {
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
					Image:   image,
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
	fmt.Printf(">>> [POD] %v/%v creating...\n", *p.Namespace, *p.Name)
	pod := newPod(*p.Namespace, *p.Name, *p.Image, *p.Name)
	p.Timings.Start = time.Now()
	_, err := p.ClientSet.CoreV1().Pods(*p.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	fmt.Printf(">>> [POD] %v/%v created\n", *p.Namespace, *p.Name)
	return nil
}

func (p *Pod) WaitCreate() error {
	return wait.PollImmediate(APICallRetryInterval, DefaultPollTimeout, func() (bool, error) {
		fmt.Printf(">>> [POD] %v/%v waiting to be ready....\n", *p.Namespace, *p.Name)
		pod, err := p.ClientSet.CoreV1().Pods(*p.Namespace).Get(context.TODO(), *p.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady {
				if condition.Status == corev1.ConditionTrue {
					p.Timings.End = time.Now()
					p.Timings.Duration = time.Since(p.Timings.Start).String()
					logSuccess(fmt.Sprintf(">>> [POD] %v/%v ready, time elapsed: %v\n", *p.Namespace, *p.Name, p.Timings.Duration))
					return true, nil
				}
			}
		}

		return false, nil
	})
}

func (p *Pod) Delete() error {
	fmt.Printf(">>> [POD] %v/%v deleting...\n", *p.Namespace, *p.Name)
	p.Timings.Start = time.Now()
	deletePolicy := metav1.DeletePropagationForeground
	err := p.ClientSet.CoreV1().Pods(*p.Namespace).
		Delete(context.TODO(), *p.Name, metav1.DeleteOptions{PropagationPolicy: &deletePolicy})
	if err != nil {
		return err
	}

	fmt.Printf(">>> [POD] %v/%v deleting started\n", *p.Namespace, *p.Name)
	return nil
}

func (p *Pod) WaitDelete() error {
	return wait.PollImmediate(APICallRetryInterval, DefaultPollTimeout, func() (bool, error) {
		fmt.Printf(">>> [POD] %v/%v waiting to be deleted....\n", *p.Namespace, *p.Name)
		_, err := p.ClientSet.CoreV1().Pods(*p.Namespace).Get(context.TODO(), *p.Name, metav1.GetOptions{})
		if k8serr.IsNotFound(err) {
			p.Timings.End = time.Now()
			p.Timings.Duration = time.Since(p.Timings.Start).String()
			logSuccess(fmt.Sprintf(">>> [POD] %v/%v deleted, time elapsed: %v\n", *p.Namespace, *p.Name, p.Timings.Duration))
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

	prefix = fs.String("prefix", AppName, "name prefix used for pods and pvcs")
	resultsFile = fs.String("results-file", DefaultResultsFile, "path to write the json results")
	noResultsFile = fs.Bool("no-results-file", false, "do not write the json results to a file")
	resultsStdout = fs.Bool("results-stdout", false, "show json results to stdout (verbose)")
	noResults = fs.Bool("no-results", false, "do not output json results at all")
	namespace = fs.String("namespace", metav1.NamespaceDefault, "namespace for the pods")
	image = fs.String("image", DefaultImage, "pod image")
	kubeconfig = fs.String("kubeconfig", os.Getenv("KUBECONFIG"), "absolute path to the kubeconfig file (mandatory)")
	count = fs.Int("count", DefaultCount, "Number of pod with pvc to create")
	storageClass = fs.String("storage-class", "", "Storage Class of the PersistentVolumeClaims (mandatory)")
	reqStorageSize = fs.String("pvc-size", DefaultSize, "Requested size of the PersistentVolumeClaims")

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

	pvcs := make([]*Pvc, 0, *count)
	pods := make([]*Pod, 0, *count)

	doneChan := make(chan bool)
	deployErrChan := make(chan error)
	cleanErrChan := make(chan error)

	start := time.Now()
	fmt.Println(">>> Starting:", start)
	for i := 1; i <= *count; i++ {
		name := fmt.Sprintf("%v-%04d", *prefix, i)

		// Generate PVC
		pvc := Pvc{Namespace: namespace, Name: &name, ClientSet: clientset, Size: reqStorageSize, StorageClass: storageClass}
		pvcs = append(pvcs, &pvc)

		// Generate POD
		pod := Pod{Namespace: namespace, Name: &name, Image: image, ClientSet: clientset}
		pods = append(pods, &pod)

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
		// POD+PVC = 2
		if i == (*count * 2) {
			break
		}
	}

	pwp := PodWithPvc{
		Namespace: namespace,
		Command:   fs.Name(),
		Pvc:       pvcs,
		Pod:       pods,
	}

	if !*noResults {
		outputMarshal, err := json.MarshalIndent(pwp, "", "  ")
		if err != nil {
			panic(err)
		}

		if !*noResultsFile && *resultsFile != "" {
			fmt.Printf(">>> Writing results to: %v\n", *resultsFile)
			err := ioutil.WriteFile(*resultsFile, outputMarshal, 0644)
			if err != nil {
				panic(err)
			}
			logSuccess(">>> Results successfully written to file")
		}

		if *resultsStdout {
			fmt.Println(string(outputMarshal))
		}
	}

	fmt.Println(">>> Finished:", time.Now())
	fmt.Println(">>> Duration:", time.Since(start))
}
