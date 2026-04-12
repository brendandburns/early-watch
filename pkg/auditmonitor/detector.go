package auditmonitor

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
)

// defaultUserAgentPatterns is the list of regular expressions used when no
// patterns are configured in a ManualTouchMonitor.  Any request whose
// User-Agent header starts with "kubectl/" is treated as a manual touch.
var defaultUserAgentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^kubectl/`),
}

// monitoredVerbs is the set of HTTP verbs (lower-cased) that can constitute
// a manual touch.
var monitoredVerbs = map[string]ewv1alpha1.MonitorOperationType{
	"delete": ewv1alpha1.MonitorOperationDelete,
	"create": ewv1alpha1.MonitorOperationCreate,
	"update": ewv1alpha1.MonitorOperationUpdate,
	"patch":  ewv1alpha1.MonitorOperationUpdate, // PATCH maps to UPDATE
}

// TouchRecord holds the information needed to create a ManualTouchEvent.
type TouchRecord struct {
	Event            *AuditEvent
	Operation        ewv1alpha1.MonitorOperationType
	MonitorName      string
	MonitorNamespace string
}

// TouchDetector checks each audit event against all ManualTouchMonitor
// resources in the cluster and returns a TouchRecord for every match.
type TouchDetector struct {
	Client client.Client
}

// Detect evaluates a single audit event against all ManualTouchMonitor
// resources and returns a TouchRecord for each monitor that matches.
func (d *TouchDetector) Detect(ctx context.Context, event *AuditEvent) ([]TouchRecord, error) {
	logger := log.FromContext(ctx).WithValues("auditID", event.AuditID)

	// Map the event verb to a MonitorOperationType.
	op, ok := monitoredVerbs[strings.ToLower(event.Verb)]
	if !ok {
		return nil, nil
	}

	// List all ManualTouchMonitors across all namespaces.
	monitorList := &ewv1alpha1.ManualTouchMonitorList{}
	if err := d.Client.List(ctx, monitorList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("listing ManualTouchMonitors: %w", err)
	}

	var records []TouchRecord

	for i := range monitorList.Items {
		monitor := &monitorList.Items[i]

		if !monitorMatchesEvent(monitor, event, op) {
			continue
		}

		logger.Info("Manual touch detected",
			"monitor", monitor.Name,
			"user", event.User.Username,
			"resource", event.ObjectRef.Resource,
			"name", event.ObjectRef.Name,
		)

		records = append(records, TouchRecord{
			Event:            event,
			Operation:        op,
			MonitorName:      monitor.Name,
			MonitorNamespace: monitor.Namespace,
		})
	}

	return records, nil
}

// monitorMatchesEvent returns true when the audit event matches the monitor's
// configured subjects, operations, user-agent patterns, and service-account
// exclusions.
func monitorMatchesEvent(
	monitor *ewv1alpha1.ManualTouchMonitor,
	event *AuditEvent,
	op ewv1alpha1.MonitorOperationType,
) bool {
	// Check operation.
	if !operationMatches(monitor.Spec.Operations, op) {
		return false
	}

	// Check user-agent.
	patterns := compilePatterns(monitor.Spec.UserAgentPatterns)
	if !userAgentMatches(event.UserAgent, patterns) {
		return false
	}

	// Check service-account exclusions.
	if isExcluded(event.User.Username, monitor.Spec.ExcludeServiceAccounts) {
		return false
	}

	// Check subjects.
	for _, subj := range monitor.Spec.Subjects {
		if subjectMatchesEvent(subj, event) {
			return true
		}
	}

	return false
}

// operationMatches returns true when op is in the allowed list.
func operationMatches(ops []ewv1alpha1.MonitorOperationType, op ewv1alpha1.MonitorOperationType) bool {
	for _, o := range ops {
		if o == op {
			return true
		}
	}
	return false
}

// compilePatterns compiles a slice of regex strings.  When patterns is empty
// the default kubectl pattern is returned.  Patterns that fail to compile are
// silently skipped.
func compilePatterns(patterns []string) []*regexp.Regexp {
	if len(patterns) == 0 {
		return defaultUserAgentPatterns
	}
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		compiled = append(compiled, re)
	}
	if len(compiled) == 0 {
		return defaultUserAgentPatterns
	}
	return compiled
}

// userAgentMatches returns true when the user agent matches at least one
// compiled pattern.
func userAgentMatches(userAgent string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(userAgent) {
			return true
		}
	}
	return false
}

// isExcluded returns true when username is present in the exclusion list.
func isExcluded(username string, exclusions []string) bool {
	for _, ex := range exclusions {
		if ex == username {
			return true
		}
	}
	return false
}

// subjectMatchesEvent returns true when the monitor subject matches the
// resource referenced in the audit event.
func subjectMatchesEvent(subj ewv1alpha1.MonitorSubject, event *AuditEvent) bool {
	// Match API group.
	if subj.APIGroup != event.ObjectRef.APIGroup {
		return false
	}

	// Match resource (case-insensitive).
	if !strings.EqualFold(subj.Resource, event.ObjectRef.Resource) {
		return false
	}

	// NamespaceSelector is evaluated by listing namespace labels at
	// detection time; for simplicity a nil selector matches all namespaces.
	if subj.NamespaceSelector != nil {
		if !namespaceMatchesSelector(event.ObjectRef.Namespace, subj.NamespaceSelector) {
			return false
		}
	}

	return true
}

// namespaceMatchesSelector is a best-effort check: when MatchLabels is set it
// verifies the namespace name is in the allowed set if names are provided via
// a special convention.  In production this should query the cluster for the
// namespace's labels and evaluate the full LabelSelector.
//
// For now, a nil or empty NamespaceSelector always matches, and a non-empty
// MatchLabels causes the check to pass only when the namespace is explicitly
// listed in MatchLabels as a key (a simple allow-list convention).
func namespaceMatchesSelector(namespace string, sel *metav1.LabelSelector) bool {
	if sel == nil {
		return true
	}
	if len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) == 0 {
		return true
	}
	// Simplified: if the namespace name is a key in MatchLabels, it matches.
	_, ok := sel.MatchLabels[namespace]
	return ok
}
