package util

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/joshvanl/cni-migration/pkg/types"
)

type Factory struct {
	log    *logrus.Entry
	ctx    context.Context
	client *kubernetes.Clientset
}

func New(log *logrus.Entry, ctx context.Context, client *kubernetes.Clientset) *Factory {
	return &Factory{
		log:    log,
		ctx:    ctx,
		client: client,
	}
}

func (f *Factory) CreateDaemonSet(yamlFilePath, namespace, name string) error {
	if err := f.createResource(yamlFilePath, namespace, name); err != nil {
		return err
	}

	if err := f.waitDaemonSetReady(namespace, name); err != nil {
		return err
	}

	return nil
}

func (f *Factory) DeletePodsOnNode(nodeName string) error {
	nss, err := f.client.CoreV1().Namespaces().List(f.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	toBeDeleted := make(map[*corev1.Pod]struct{})

	for _, ns := range nss.Items {
		pods, err := f.client.CoreV1().Pods(ns.Name).List(f.ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}

		for _, p := range pods.Items {
			if p.Spec.NodeName == nodeName {
				f.log.Debugf("deleting pod %s on node %s", p.Name, nodeName)
				toBeDeleted[p.DeepCopy()] = struct{}{}

				err = f.client.CoreV1().Pods(ns.Name).Delete(f.ctx, p.Name, metav1.DeleteOptions{})
				if err != nil {
					return err
				}
			}
		}
	}

	// Wait for pods to be deleted
	for {
		for p := range toBeDeleted {
			_, err = f.client.CoreV1().Pods(p.Namespace).Get(f.ctx, p.Name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				delete(toBeDeleted, p)
				continue
			}
			if err != nil {
				return err
			}
		}

		if len(toBeDeleted) == 0 {
			break
		}

		time.Sleep(time.Second)
	}

	return nil
}

func (f *Factory) createResource(yamlFilePath, namespace, name string) error {
	filePath := filepath.Join(types.ResourcesDirectory, yamlFilePath)

	f.log.Debugf("Applying %s: %s", name, filePath)

	args := []string{"kubectl", "apply", "--namespace", namespace, "-f", filePath}
	if err := f.RunCommand(args...); err != nil {
		return err
	}

	return nil
}

func (f *Factory) RunCommand(args ...string) error {
	f.log.Debugf("%s", args)

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
