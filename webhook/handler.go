package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Handle(w http.ResponseWriter, r *http.Request) {
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

	resp := evaluate(review.Request)
	resp.UID = review.Request.UID
	review.Response = resp

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(review) //nolint:errcheck
}

func evaluate(req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	var violations []string

	switch req.Resource.Resource {
	case "deployments":
		var obj appsv1.Deployment
		if err := json.Unmarshal(req.Object.Raw, &obj); err != nil {
			return deny(fmt.Sprintf("failed to decode Deployment: %v", err))
		}
		violations = validatePodSpec(obj.Spec.Template.Spec)

	case "statefulsets":
		var obj appsv1.StatefulSet
		if err := json.Unmarshal(req.Object.Raw, &obj); err != nil {
			return deny(fmt.Sprintf("failed to decode StatefulSet: %v", err))
		}
		violations = validatePodSpec(obj.Spec.Template.Spec)

	default:
		return allow()
	}

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
