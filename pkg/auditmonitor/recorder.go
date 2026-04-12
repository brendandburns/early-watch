package auditmonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
)

const (
	// DefaultEventNamespace is the namespace where ManualTouchEvents are
	// stored when no override is provided.
	DefaultEventNamespace = "early-watch-system"

	// LabelResource is the label key used to index events by resource type.
	LabelResource = "earlywatch.io/resource"

	// LabelResourceNamespace is the label key used to index events by the
	// namespace of the touched resource.
	LabelResourceNamespace = "earlywatch.io/resource-namespace"

	// LabelResourceName is the label key used to index events by the name
	// of the touched resource.
	LabelResourceName = "earlywatch.io/resource-name"

	// LabelAPIGroup is the label key used to index events by API group.
	LabelAPIGroup = "earlywatch.io/api-group"

	// LabelOperation is the label key used to index events by operation.
	LabelOperation = "earlywatch.io/operation"
)

// TouchRecorder creates ManualTouchEvent custom resources in the cluster and
// optionally sends notifications to configured alerting sinks.
type TouchRecorder struct {
	Client          client.Client
	EventNamespace  string
}

// Record creates a ManualTouchEvent for the given TouchRecord and sends
// any configured alerts.
func (r *TouchRecorder) Record(ctx context.Context, touch TouchRecord) error {
	logger := log.FromContext(ctx)

	ns := r.EventNamespace
	if ns == "" {
		ns = DefaultEventNamespace
	}

	event := touch.Event
	op := string(touch.Operation)

	// Build a deterministic name from auditID so duplicate deliveries are
	// idempotent (the Create will fail with AlreadyExists on retry).
	name := sanitizeName("mte-" + event.AuditID)

	mte := &ewv1alpha1.ManualTouchEvent{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "earlywatch.io/v1alpha1",
			Kind:       "ManualTouchEvent",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				LabelResource:          event.ObjectRef.Resource,
				LabelResourceNamespace: event.ObjectRef.Namespace,
				LabelResourceName:      event.ObjectRef.Name,
				LabelAPIGroup:          event.ObjectRef.APIGroup,
				LabelOperation:         op,
			},
		},
		Spec: ewv1alpha1.ManualTouchEventSpec{
			Timestamp:         eventTime(event.RequestReceivedTimestamp),
			User:              event.User.Username,
			UserAgent:         event.UserAgent,
			Operation:         op,
			APIGroup:          event.ObjectRef.APIGroup,
			Resource:          event.ObjectRef.Resource,
			ResourceName:      event.ObjectRef.Name,
			ResourceNamespace: event.ObjectRef.Namespace,
			SourceIP:          firstSourceIP(event.SourceIPs),
			AuditID:           event.AuditID,
			MonitorName:       touch.MonitorName,
			MonitorNamespace:  touch.MonitorNamespace,
		},
	}

	if err := r.Client.Create(ctx, mte); err != nil {
		// Ignore AlreadyExists — idempotent retry.
		if isAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating ManualTouchEvent: %w", err)
	}

	logger.Info("Recorded ManualTouchEvent",
		"name", name,
		"namespace", ns,
		"user", event.User.Username,
		"resource", event.ObjectRef.Resource,
		"resourceName", event.ObjectRef.Name,
	)

	// Send alerts (best-effort; errors are logged, not returned).
	if err := r.sendAlerts(ctx, touch, mte); err != nil {
		logger.Error(err, "Failed to send alert for ManualTouchEvent", "name", name)
	}

	return nil
}

// sendAlerts dispatches notifications to any configured alerting sinks for the
// monitor that generated this touch record.
func (r *TouchRecorder) sendAlerts(ctx context.Context, touch TouchRecord, mte *ewv1alpha1.ManualTouchEvent) error {
	// Fetch the originating monitor to read its alerting config.
	monitor := &ewv1alpha1.ManualTouchMonitor{}
	if err := r.Client.Get(ctx, client.ObjectKey{
		Name:      touch.MonitorName,
		Namespace: touch.MonitorNamespace,
	}, monitor); err != nil {
		return fmt.Errorf("fetching ManualTouchMonitor %s/%s: %w",
			touch.MonitorNamespace, touch.MonitorName, err)
	}

	if monitor.Spec.Alerting == nil {
		return nil
	}

	if monitor.Spec.Alerting.SlackWebhookURL != "" {
		if err := sendSlackAlert(ctx, monitor.Spec.Alerting.SlackWebhookURL, mte); err != nil {
			return fmt.Errorf("sending Slack alert: %w", err)
		}
	}

	return nil
}

// sendSlackAlert POSTs a simple Slack message to the configured incoming
// webhook URL.
func sendSlackAlert(ctx context.Context, webhookURL string, mte *ewv1alpha1.ManualTouchEvent) error {
	spec := mte.Spec
	msg := fmt.Sprintf(
		":warning: *Manual touch detected* — "+
			"user *%s* performed *%s* on `%s/%s` (namespace: `%s`) via `%s`",
		spec.User,
		spec.Operation,
		spec.Resource,
		spec.ResourceName,
		spec.ResourceNamespace,
		spec.UserAgent,
	)

	payload := map[string]string{"text": msg}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling Slack payload: %w", err)
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building Slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POSTing to Slack: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Slack returned non-2xx status: %d", resp.StatusCode)
	}

	return nil
}

// sanitizeName converts an arbitrary string into a valid Kubernetes resource
// name by lower-casing and replacing non-alphanumeric characters with dashes,
// then truncating to 253 characters.
func sanitizeName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	if len(result) > 253 {
		result = result[:253]
	}
	return result
}

// isAlreadyExists returns true when the error indicates a resource already
// exists.  This avoids importing k8s.io/apimachinery/pkg/api/errors directly.
func isAlreadyExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already exists")
}
