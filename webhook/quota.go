package webhook

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// QuotaFetcher returns the set of resource names covered by ResourceQuotas in a namespace.
type QuotaFetcher interface {
	ResourcesForNamespace(ctx context.Context, namespace string) (map[corev1.ResourceName]bool, error)
}

type k8sQuotas struct {
	client kubernetes.Interface
}

// NewK8sQuotaFetcher returns a QuotaFetcher backed by the Kubernetes API.
func NewK8sQuotaFetcher(client kubernetes.Interface) QuotaFetcher {
	return &k8sQuotas{client: client}
}

func (k *k8sQuotas) ResourcesForNamespace(ctx context.Context, namespace string) (map[corev1.ResourceName]bool, error) {
	list, err := k.client.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := map[corev1.ResourceName]bool{}
	for _, q := range list.Items {
		for res := range q.Spec.Hard {
			out[res] = true
		}
	}
	return out, nil
}
