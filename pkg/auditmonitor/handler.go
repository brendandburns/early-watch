// Package auditmonitor implements an HTTP sink for Kubernetes audit log
// webhooks.  It receives audit.k8s.io/v1 EventList payloads, detects manual
// touches (kubectl DELETE/CREATE/UPDATE operations), and records them as
// ManualTouchEvent custom resources.
package auditmonitor

import (
	"encoding/json"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// AuditEventList is a minimal representation of audit.k8s.io/v1 EventList.
// Only the fields required for manual-touch detection are included.
type AuditEventList struct {
	Items []AuditEvent `json:"items"`
}

// AuditEvent is a minimal representation of a single audit.k8s.io/v1 Event.
type AuditEvent struct {
	// AuditID is the unique identifier assigned by the API server.
	AuditID string `json:"auditID"`

	// Verb is the HTTP verb of the request (e.g. "delete", "create", "update").
	Verb string `json:"verb"`

	// User contains the identity of the requesting user.
	User AuditUser `json:"user"`

	// UserAgent is the raw User-Agent header sent with the request.
	UserAgent string `json:"userAgent"`

	// SourceIPs is the list of source IPs as observed by the API server.
	SourceIPs []string `json:"sourceIPs"`

	// ObjectRef describes the object the request acted on.
	ObjectRef AuditObjectRef `json:"objectRef"`

	// RequestReceivedTimestamp is when the API server received the request.
	RequestReceivedTimestamp metav1.MicroTime `json:"requestReceivedTimestamp"`

	// Stage is the processing stage at which the event was generated
	// (e.g. "ResponseComplete").
	Stage string `json:"stage"`
}

// AuditUser holds the identity fields of the user that issued the request.
type AuditUser struct {
	Username string   `json:"username"`
	Groups   []string `json:"groups"`
}

// AuditObjectRef describes the Kubernetes object a request acted on.
type AuditObjectRef struct {
	// Resource is the plural resource name, e.g. "services".
	Resource string `json:"resource"`
	// Namespace is the namespace of the resource (empty for cluster-scoped).
	Namespace string `json:"namespace"`
	// Name is the name of the specific resource instance.
	Name string `json:"name"`
	// APIGroup is the API group (empty for core resources).
	APIGroup string `json:"apiGroup"`
	// APIVersion is the API version, e.g. "v1".
	APIVersion string `json:"apiVersion"`
}

// AuditEventHandler is an http.Handler that receives Kubernetes audit webhook
// payloads (audit.k8s.io/v1 EventList), detects manual touches via a
// TouchDetector, and records them via a TouchRecorder.
type AuditEventHandler struct {
	Detector *TouchDetector
	Recorder *TouchRecorder
}

// maxRequestBodyBytes is the maximum audit webhook payload size accepted by
// the handler.  32 MiB is well above any realistic batch size (the API server
// typically sends batches of a few hundred events), but still provides a
// safeguard against memory exhaustion from misconfigured or malicious senders.
const maxRequestBodyBytes = 32 << 20 // 32 MiB

// ServeHTTP implements http.Handler.  The Kubernetes API server POSTs a JSON
// EventList body to this endpoint for each audit batch.
func (h *AuditEventHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := log.FromContext(r.Context())

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	var eventList AuditEventList
	if err := json.NewDecoder(r.Body).Decode(&eventList); err != nil {
		logger.Error(err, "Failed to decode audit EventList")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	for i := range eventList.Items {
		event := &eventList.Items[i]

		// Only process events that have completed at the response stage to
		// avoid double-counting.
		if event.Stage != "ResponseComplete" {
			continue
		}

		touches, err := h.Detector.Detect(r.Context(), event)
		if err != nil {
			logger.Error(err, "Error detecting manual touch", "auditID", event.AuditID)
			continue
		}

		for _, touch := range touches {
			if err := h.Recorder.Record(r.Context(), touch); err != nil {
				logger.Error(err, "Error recording ManualTouchEvent", "auditID", event.AuditID)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// firstSourceIP returns the first source IP from the slice, or an empty string.
func firstSourceIP(ips []string) string {
	if len(ips) > 0 {
		return ips[0]
	}
	return ""
}

// eventTime returns the event timestamp as a metav1.Time, falling back to
// the current time when the timestamp is zero.
func eventTime(ts metav1.MicroTime) metav1.Time {
	if ts.IsZero() {
		return metav1.NewTime(time.Now())
	}
	return metav1.NewTime(ts.Time)
}
