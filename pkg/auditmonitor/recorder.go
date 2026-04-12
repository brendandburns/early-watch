package auditmonitor

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	Client         client.Client
	EventNamespace string
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

	// Build a deterministic name from auditID + monitor identity so:
	//   1. Duplicate deliveries of the same event to the same monitor are
	//      idempotent (AlreadyExists on retry).
	//   2. Multiple monitors matching the same event each produce their own
	//      ManualTouchEvent (fanout).
	name := sanitizeName("mte-" + event.AuditID + "-" + touch.MonitorNamespace + "-" + touch.MonitorName)

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
		if apierrors.IsAlreadyExists(err) {
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
