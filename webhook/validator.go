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

// validateQuota checks that every resource used by any container is covered by
// at least one entry in quotaed. Quota entries may be bare ("cpu"), or prefixed
// with "requests." or "limits." — any of the three forms is sufficient.
func validateQuota(spec corev1.PodSpec, quotaed map[corev1.ResourceName]bool) []string {
	var violations []string
	all := append(spec.Containers, spec.InitContainers...)
	for _, c := range all {
		seen := map[corev1.ResourceName]bool{}
		for res := range c.Resources.Requests {
			if !seen[res] && !coveredByQuota(res, quotaed) {
				violations = append(violations, fmt.Sprintf("container %q: resource %q is not covered by any ResourceQuota", c.Name, res))
				seen[res] = true
			}
		}
		for res := range c.Resources.Limits {
			if !seen[res] && !coveredByQuota(res, quotaed) {
				violations = append(violations, fmt.Sprintf("container %q: resource %q is not covered by any ResourceQuota", c.Name, res))
				seen[res] = true
			}
		}
	}
	return violations
}

func coveredByQuota(res corev1.ResourceName, quotaed map[corev1.ResourceName]bool) bool {
	name := string(res)
	return quotaed[res] ||
		quotaed[corev1.ResourceName("requests."+name)] ||
		quotaed[corev1.ResourceName("limits."+name)]
}
