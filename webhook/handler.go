package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Handler is the admission webhook HTTP handler.
type Handler struct {
	quotas QuotaFetcher
}

// New returns a Handler wired to the given QuotaFetcher.
func New(qf QuotaFetcher) *Handler {
	return &Handler{quotas: qf}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var review admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		http.Error(w, "failed to decode AdmissionReview", http.StatusBadRequest)
		return
	}

	resp := h.evaluate(r.Context(), review.Request)
	resp.UID = review.Request.UID
	review.Response = resp

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(review) //nolint:errcheck
}

func (h *Handler) evaluate(ctx context.Context, req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	var spec corev1.PodSpec

	switch req.Resource.Resource {
	case "deployments":
		var obj appsv1.Deployment
		if err := json.Unmarshal(req.Object.Raw, &obj); err != nil {
			return deny(fmt.Sprintf("failed to decode Deployment: %v", err))
		}
		spec = obj.Spec.Template.Spec

	case "statefulsets":
		var obj appsv1.StatefulSet
		if err := json.Unmarshal(req.Object.Raw, &obj); err != nil {
			return deny(fmt.Sprintf("failed to decode StatefulSet: %v", err))
		}
		spec = obj.Spec.Template.Spec

	default:
		return allow()
	}

	violations := validatePodSpec(spec)

	quotaed, err := h.quotas.ResourcesForNamespace(ctx, req.Namespace)
	if err != nil {
		return deny(fmt.Sprintf("failed to fetch ResourceQuota: %v", err))
	}
	if len(quotaed) == 0 {
		return deny("no ResourceQuota with resource limits found in namespace " + req.Namespace)
	}
	violations = append(violations, validateQuota(spec, quotaed)...)

	if len(violations) > 0 {
		return deny("resource validation failed:\n  - " + strings.Join(violations, "\n  - "))
	}
	return allow()
}

func allow() *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{Allowed: true}
}

func deny(reason string) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: false,
		Result:  &metav1.Status{Message: reason},
	}
}
