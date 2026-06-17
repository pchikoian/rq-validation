package webhook

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

var required = []corev1.ResourceName{
	corev1.ResourceCPU,
	corev1.ResourceMemory,
}

func validatePodSpec(spec corev1.PodSpec) []string {
	var violations []string
	all := append(spec.Containers, spec.InitContainers...)
	for _, c := range all {
		violations = append(violations, checkContainer(c)...)
	}
	return violations
}

func checkContainer(c corev1.Container) []string {
	var v []string
	for _, res := range required {
		if isMissing(c.Resources.Requests, res) {
			v = append(v, fmt.Sprintf("container %q: missing resources.requests.%s", c.Name, res))
		}
		if isMissing(c.Resources.Limits, res) {
			v = append(v, fmt.Sprintf("container %q: missing resources.limits.%s", c.Name, res))
		}
	}
	return v
}

func isMissing(list corev1.ResourceList, name corev1.ResourceName) bool {
	if list == nil {
		return true
	}
	q, ok := list[name]
	return !ok || q.IsZero()
}
