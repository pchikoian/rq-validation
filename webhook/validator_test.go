package webhook

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func fullRes() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}

func ctr(name string, res corev1.ResourceRequirements) corev1.Container {
	return corev1.Container{Name: name, Resources: res}
}

func spec(containers ...corev1.Container) corev1.PodSpec {
	return corev1.PodSpec{Containers: containers}
}

func TestAllResourcesPresent(t *testing.T) {
	v := validatePodSpec(spec(ctr("app", fullRes())))
	if len(v) != 0 {
		t.Errorf("expected no violations, got: %v", v)
	}
}

func TestMissingResourcesBlock(t *testing.T) {
	v := validatePodSpec(spec(ctr("app", corev1.ResourceRequirements{})))
	if len(v) != 4 {
		t.Errorf("expected 4 violations, got %d: %v", len(v), v)
	}
}

func TestMissingLimits(t *testing.T) {
	res := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
	v := validatePodSpec(spec(ctr("app", res)))
	if len(v) != 2 {
		t.Errorf("expected 2 violations (missing limits), got %d: %v", len(v), v)
	}
}

func TestMissingRequests(t *testing.T) {
	res := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
	v := validatePodSpec(spec(ctr("app", res)))
	if len(v) != 2 {
		t.Errorf("expected 2 violations (missing requests), got %d: %v", len(v), v)
	}
}

func TestInitContainerValidated(t *testing.T) {
	s := corev1.PodSpec{
		Containers:     []corev1.Container{ctr("app", fullRes())},
		InitContainers: []corev1.Container{ctr("init", corev1.ResourceRequirements{})},
	}
	v := validatePodSpec(s)
	if len(v) != 4 {
		t.Errorf("expected 4 violations from initContainer, got %d: %v", len(v), v)
	}
}

func TestMultipleContainersOneBad(t *testing.T) {
	v := validatePodSpec(spec(
		ctr("good", fullRes()),
		ctr("bad", corev1.ResourceRequirements{}),
	))
	if len(v) != 4 {
		t.Errorf("expected 4 violations, got %d: %v", len(v), v)
	}
	for _, msg := range v {
		if !strings.Contains(msg, `"bad"`) {
			t.Errorf("violation should name 'bad' container: %s", msg)
		}
	}
}

// ── validateQuota tests ───────────────────────────────────────────────────────

func quotaWith(names ...corev1.ResourceName) map[corev1.ResourceName]bool {
	m := map[corev1.ResourceName]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

func TestQuotaAllCovered(t *testing.T) {
	q := quotaWith("requests.cpu", "requests.memory", "limits.cpu", "limits.memory")
	if v := validateQuota(spec(ctr("app", fullRes())), q); len(v) != 0 {
		t.Errorf("expected no violations, got: %v", v)
	}
}

func TestQuotaBareNameCoversRequestsAndLimits(t *testing.T) {
	q := quotaWith("cpu", "memory")
	if v := validateQuota(spec(ctr("app", fullRes())), q); len(v) != 0 {
		t.Errorf("bare quota names should cover both requests and limits, got: %v", v)
	}
}

func TestQuotaUncoveredResource(t *testing.T) {
	q := quotaWith("requests.cpu", "requests.memory", "limits.cpu", "limits.memory")
	res := fullRes()
	res.Requests["nvidia.com/gpu"] = resource.MustParse("1")
	res.Limits["nvidia.com/gpu"] = resource.MustParse("1")
	v := validateQuota(spec(ctr("app", res)), q)
	if len(v) != 1 || !strings.Contains(v[0], "nvidia.com/gpu") {
		t.Errorf("expected 1 violation for nvidia.com/gpu, got: %v", v)
	}
}

func TestQuotaDeduplicatesPerResource(t *testing.T) {
	// cpu appears in both requests and limits; should produce only one violation
	q := quotaWith("requests.memory", "limits.memory")
	v := validateQuota(spec(ctr("app", fullRes())), q)
	if len(v) != 1 || !strings.Contains(v[0], "cpu") {
		t.Errorf("expected 1 violation for cpu (deduplicated), got: %v", v)
	}
}

func TestQuotaInitContainerChecked(t *testing.T) {
	q := quotaWith("requests.cpu", "requests.memory", "limits.cpu", "limits.memory")
	s := corev1.PodSpec{
		Containers:     []corev1.Container{ctr("app", fullRes())},
		InitContainers: []corev1.Container{ctr("init", fullRes())},
	}
	s.InitContainers[0].Resources.Requests["nvidia.com/gpu"] = resource.MustParse("1")
	s.InitContainers[0].Resources.Limits["nvidia.com/gpu"] = resource.MustParse("1")
	v := validateQuota(s, q)
	if len(v) != 1 || !strings.Contains(v[0], `"init"`) {
		t.Errorf("expected 1 violation naming init container, got: %v", v)
	}
}

func TestMissingOnlyMemoryLimit(t *testing.T) {
	res := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("500m"),
			// memory limit absent
		},
	}
	v := validatePodSpec(spec(ctr("app", res)))
	if len(v) != 1 || !strings.Contains(v[0], "limits.memory") {
		t.Errorf("expected 1 violation for limits.memory, got: %v", v)
	}
}
